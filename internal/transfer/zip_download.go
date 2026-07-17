package transfer

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"threev/internal/connection"
	"threev/internal/domain"
	"threev/internal/s3client"
)

// downloadableKey is one real (non-placeholder) S3 object found under a
// prefix by listDownloadableKeysUnderPrefix, carrying its size alongside its
// key so callers that need a byte total upfront (QueueDownloadPrefixZip)
// don't need a second listing pass just to sum sizes.
type downloadableKey struct {
	Key  string
	Size int64
}

// listDownloadableKeysUnderPrefix runs the same paginated, delimiter-less
// ListObjectsV2 walk under bucket/prefix that QueueDownloadPrefix has always
// done (via the existing listObjectsPageForDownloadPrefix, unchanged),
// applying the same zero-byte "folder placeholder" skip
// (strings.HasSuffix(key, "/") && size == 0 - see QueueDownloadPrefix's own
// doc comment for why), and returns every remaining key/size pair found.
//
// This is the ONE shared listing helper both QueueDownloadPrefix (which
// resolves each key to a local disk path and calls QueueDownload once per
// key - unchanged behavior, just now sourced from here instead of its own
// inline loop) and QueueDownloadPrefixZip/runZipDownloadTask (which need the
// list, and its summed size, for a single archive task - see
// QueueDownloadPrefixZip's own doc comment for why the list itself is never
// persisted) call, so the pagination/skip logic is written exactly once.
//
// Unlike QueueDownloadPrefix's own best-effort semantics (a page fetch
// failure is logged and simply ends pagination early, never surfaced as an
// error to its own caller), this helper DOES return a non-nil error when a
// page fetch fails - paired with whatever keys were already collected from
// earlier, successful pages. QueueDownloadPrefix chooses to log that error
// and proceed with the partial list exactly as it always has (preserving
// its existing, tested behavior); QueueDownloadPrefixZip/runZipDownloadTask
// instead treat it as fatal to the whole archive - an incomplete listing
// silently producing an incomplete zip is worse than failing loudly, and
// unlike QueueDownloadPrefix's many-independent-tasks model, a zip archive
// is a single, all-or-nothing deliverable.
func (s *TransferService) listDownloadableKeysUnderPrefix(pooled, fresh *s3.Client, host, bucket, prefix string) ([]downloadableKey, error) {
	var keys []downloadableKey

	continuationToken := ""

	for {
		page, listErr := s.listObjectsPageForDownloadPrefix(pooled, fresh, host, bucket, prefix, continuationToken)
		if listErr != nil {
			return keys, fmt.Errorf("list %s/%s: %w", bucket, prefix, listErr)
		}

		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)

			if strings.HasSuffix(key, "/") && aws.ToInt64(obj.Size) == 0 {
				continue
			}

			keys = append(keys, downloadableKey{Key: key, Size: aws.ToInt64(obj.Size)})
		}

		if !aws.ToBool(page.IsTruncated) {
			break
		}

		continuationToken = aws.ToString(page.NextContinuationToken)
		if continuationToken == "" {
			break
		}
	}

	return keys, nil
}

// QueueDownloadPrefixZip creates a new "pending" "download_zip" task that
// downloads every object found under bucket/prefix into a single ZIP
// archive at localZipPath, in one task (unlike QueueDownloadPrefix, which
// queues one independent task per object).
//
// Folder-only, never a bucket's root: prefix must be non-empty. This is not
// merely a UX convention - it is required by this task's own S3-side
// encoding: SourcePath is set to encodeBucketKey(bucket, prefix) below, and
// decoding that back out (splitBucketKey, called later at execution time by
// taskBucketKey) rejects an empty key component outright ("invalid
// bucket/key encoding"). No special-case handling for an empty prefix is
// added here - see this function's own tests for confirmation that an empty
// prefix fails gracefully (not a panic): QueueDownloadPrefixZip itself
// succeeds (encodeBucketKey never fails, it is plain string concatenation),
// and the resulting task simply fails, loudly and clearly, the moment
// dispatch() runs it and taskBucketKey's splitBucketKey call rejects it -
// the exact same mechanism every other malformed bucket/key encoding in
// this package already goes through.
//
// The member key list itself is NOT persisted anywhere - not in the
// transfer_queue row, not in any new table. runZipDownloadTask re-derives it
// by calling listDownloadableKeysUnderPrefix again once the task actually
// starts running. This is intentional statelessness, not an oversight:
// persisting a potentially large key list in SQLite for a single-use value
// is unwarranted schema churn, and a second, cheap ListObjectsV2 round-trip
// at execution time is negligible next to the actual GetObject transfers
// that follow it - the same "S3 is the source of truth at execution time"
// principle internal/filemanager/copymove.go's own re-resolving pattern
// already follows elsewhere in this codebase.
//
// Guarded (Этап 4 суб-этап 4.4) exactly like QueueDownloadPrefix, for the
// same reason: this resolves the profile synchronously to get a live S3
// client for the listing call below, so it needs its own guard rather than
// relying on runTask's.
func (s *TransferService) QueueDownloadPrefixZip(profileID int64, bucket, prefix, localZipPath string) (int64, error) {
	key, ok := s.keyBox.Get()
	if !ok {
		return 0, domain.ErrLocked
	}

	profile, err := connection.ResolveProfile(context.Background(), s.profileRepo, key, profileID)
	if err != nil {
		return 0, fmt.Errorf("resolve profile %d: %w", profileID, err)
	}

	pooled, fresh, err := s.connMgr.Get(profileID)
	if err != nil {
		return 0, fmt.Errorf("get S3 clients for profile %d: %w", profileID, err)
	}

	host := extractHostname(profile.EndpointURL)

	keys, listErr := s.listDownloadableKeysUnderPrefix(pooled, fresh, host, bucket, prefix)
	if listErr != nil {
		return 0, fmt.Errorf("list objects under %s/%s: %w", bucket, prefix, listErr)
	}

	if len(keys) == 0 {
		return 0, fmt.Errorf("no objects were queued for download under %s/%s", bucket, prefix)
	}

	var totalSize int64
	for _, k := range keys {
		totalSize += k.Size
	}

	task := domain.TransferTask{
		ProfileID:       profileID,
		Type:            "download_zip",
		SourcePath:      encodeBucketKey(bucket, prefix),
		DestinationPath: localZipPath,
		Status:          "pending",
		TotalBytes:      totalSize,
	}

	created, err := s.queueRepo.Create(context.Background(), task)
	if err != nil {
		return 0, err
	}

	s.dispatch()

	return created.ID, nil
}

// archiveEntryName computes the "/"-separated zip entry name for key inside
// a runZipDownloadTask archive: key with prefix stripped, exactly the same
// relative-path derivation resolveDownloadPrefixLocalPath uses for a local
// destination directory - reusing its traversal-safety check
// (relativeKeyPathIsSafe) since a zip entry name is just as capable of a
// "zip-slip" path-traversal attack as a local-disk path is, arguably worse
// since some naive zip-extraction tools don't defend against it themselves.
// Returns ("", false) if key's path relative to prefix escapes via "..".
//
// Unlike resolveDownloadPrefixLocalPath (which needs filepath.FromSlash to
// build a real OS path), the S3 key itself is already "/"-separated, and
// archive/zip's own documented convention is that entry names always use
// "/" regardless of host OS - so relPath is returned as-is, with no
// separator conversion needed in either direction.
func archiveEntryName(prefix, key string) (string, bool) {
	relPath := strings.TrimPrefix(key, prefix)
	if !relativeKeyPathIsSafe(key, relPath) {
		return "", false
	}

	return relPath, true
}

// trackingWriter wraps an io.Writer, calling onWrite(n) after every
// successful Write of n>0 bytes - used by fetchObjectBytes below to feed
// live progress (tracker.AddBytes) as an object's body is copied into a
// scratch buffer, mirroring downloadSegment's own buffered-read-loop-
// calling-AddBytes-per-chunk pattern (range_download.go) via a smaller,
// io.Writer-shaped equivalent suited to io.Copy's destination parameter.
type trackingWriter struct {
	w       io.Writer
	onWrite func(delta int64)
}

func (t *trackingWriter) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	if n > 0 && t.onWrite != nil {
		t.onWrite(int64(n))
	}

	return n, err
}

// fetchObjectBytes fetches the full body of bucket/key.Key under
// s3client.WithRetry/s.retryPolicies.Metadata() - a coarser-grained,
// whole-object retry rather than PartRetryPolicy's part-level one (per this
// Block's own design decision: "retry на объект, не на весь архив" - retry
// the object, not the whole archive), mirroring downloadSegment's retry
// structure (range_download.go) but for a full GetObject rather than a
// Range GetObject.
//
// Deliberate deviation from a direct resp.Body -> zip-entry-Writer copy:
// unlike downloadSegment's destination (an *os.File, where a failed
// attempt's partial WriteAt calls are harmless - the next attempt's writes
// at the same offsets simply overwrite them), a zip.Writer's per-entry
// Writer (returned by zip.Writer.Create) is write-once and append-only,
// with no equivalent way to "rewind" it. Retrying a GetObject that already
// partially streamed into a zip entry - via WithRetry, exactly as this
// package's other retry call sites do - would therefore silently append a
// second, overlapping copy of the object's bytes into that entry, corrupting
// the archive. To avoid this, every attempt streams into a fresh in-memory
// buffer (reset via buf.Reset() at the start of each attempt, discarding
// any partial data from a previous failed one) instead of the zip entry
// directly; only once WithRetry reports overall success does the caller
// (streamOneZipEntry) commit the now-complete, verified buffer to the
// archive in a single Write call. Since this loop streams one object at a
// time (never concurrently, unlike a multipart upload/range download's
// worker pool), the extra memory this costs is bounded by the single
// largest object being processed at any moment, not the archive's total
// size.
//
// tracker.AddBytes is still called incrementally as bytes are copied into
// the scratch buffer (via trackingWriter), not only once at the end, so
// live progress during a single large object's transfer is not lost to
// this buffering - at the accepted cost (identical, precedented trade-off
// to downloadSegment's own OnBytesTransferred calls inside its retried
// attempt closure) that a retried object can cause the tracker's cumulative
// counter to overcount by whatever partial amount the earlier, failed
// attempt(s) already reported: harmless to the final archive (which is
// always built from the one successful attempt's complete buffer) and, at
// worst, cosmetically imprecise progress/ETA display for that single
// object's window.
func (s *TransferService) fetchObjectBytes(ctx context.Context, pooled, fresh *s3.Client, host, bucket string, key downloadableKey, limiter *BandwidthLimiter, tracker *Tracker) ([]byte, error) {
	var buf bytes.Buffer

	err := s3client.WithRetry(ctx, s.breaker, s.retryPolicies.Metadata(), host, func(attemptCtx context.Context, isRetry bool) error {
		client := pooled
		if isRetry {
			client = fresh
		}

		timeoutCtx, cancel := context.WithTimeout(attemptCtx, s3client.AdaptiveTimeout(key.Size, 0, s.retryPolicies.TimeoutFloor()))
		defer cancel()

		resp, getErr := client.GetObject(timeoutCtx, &s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key.Key),
		})
		if getErr != nil {
			return getErr
		}
		defer func() { _ = resp.Body.Close() }()

		buf.Reset() // discard any partial data a previous, failed attempt already wrote

		body := limiter.WrapDownloadReader(timeoutCtx, resp.Body)
		dst := &trackingWriter{w: &buf, onWrite: tracker.AddBytes}

		_, copyErr := io.Copy(dst, body)

		return copyErr
	})
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// streamOneZipEntry fetches bucket/key.Key's full body (fetchObjectBytes)
// and commits it, in a single Write call, as a new entry (named via
// archiveEntryName(prefix, key.Key)) in zipWriter - see fetchObjectBytes'
// doc comment for why the fetch is fully buffered before ever touching
// zipWriter.
func (s *TransferService) streamOneZipEntry(ctx context.Context, pooled, fresh *s3.Client, host, bucket, prefix string, key downloadableKey, zipWriter *zip.Writer, limiter *BandwidthLimiter, tracker *Tracker) error {
	relPath, ok := archiveEntryName(prefix, key.Key)
	if !ok {
		return fmt.Errorf("key %s escapes the archive: rejected relative path", key.Key)
	}

	content, err := s.fetchObjectBytes(ctx, pooled, fresh, host, bucket, key, limiter, tracker)
	if err != nil {
		return fmt.Errorf("fetch object %s: %w", key.Key, err)
	}

	entryWriter, err := zipWriter.Create(relPath)
	if err != nil {
		return fmt.Errorf("create archive entry %s: %w", relPath, err)
	}

	if _, err := entryWriter.Write(content); err != nil {
		return fmt.Errorf("write archive entry %s: %w", relPath, err)
	}

	return nil
}

// removeArchiveBestEffort removes path (a partial/abandoned ZIP archive left
// behind by a failed or cancelled runZipDownloadTask), logging - never
// propagating - any error other than the file already not existing. Cleanup
// of a file that will otherwise simply sit on disk, unusable, must never
// itself block a task's own failure/cancellation from being recorded.
func removeArchiveBestEffort(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("transfer: remove partial archive %s: %v", path, err)
	}
}

// writeZipArchive creates task.DestinationPath (truncating any pre-existing
// file there - see runZipDownloadTask's doc comment for why this is
// correct both for a first run and for a Retry-triggered re-run) and
// streams every key in keys into it as one ZIP archive, in order, aborting
// the whole task on the very first key that fails (per this Block's
// "cancel-and-restart-only, no partial-archive resume" lifecycle decision -
// see runZipDownloadTask's own doc comment): no attempt is made to skip a
// failed key and continue with the rest, unlike QueueDownloadPrefix's
// best-effort semantics for its independent, per-object tasks. On any
// failure (including task's ctx being canceled - context.Canceled surfaces
// here exactly like any other per-object error, see runZipDownloadTask's
// doc comment for why that is correct), the partial file is removed
// (removeArchiveBestEffort) before the error is returned, so a
// failed/cancelled ZIP task never leaves a stray, incomplete archive on
// disk.
func (s *TransferService) writeZipArchive(ctx context.Context, task domain.TransferTask, pooled, fresh *s3.Client, host, bucket, prefix string, keys []downloadableKey, tracker *Tracker) error {
	file, err := os.Create(task.DestinationPath) //nolint:gosec // dest path is chosen via the frontend's native save-file dialog (dialogs.go's PickDownloadDestination), not attacker-controlled input
	if err != nil {
		return fmt.Errorf("create archive %s: %w", task.DestinationPath, err)
	}

	zipWriter := zip.NewWriter(file)

	limiter := s.limiter.Load()

	for _, key := range keys {
		if err := s.streamOneZipEntry(ctx, pooled, fresh, host, bucket, prefix, key, zipWriter, limiter, tracker); err != nil {
			_ = zipWriter.Close()
			_ = file.Close()
			removeArchiveBestEffort(task.DestinationPath)

			return err
		}
	}

	if err := zipWriter.Close(); err != nil {
		_ = file.Close()
		removeArchiveBestEffort(task.DestinationPath)

		return fmt.Errorf("close archive %s: %w", task.DestinationPath, err)
	}

	if err := file.Close(); err != nil {
		removeArchiveBestEffort(task.DestinationPath)

		return fmt.Errorf("close archive file %s: %w", task.DestinationPath, err)
	}

	return nil
}

// runZipDownloadTask runs the "download_zip" side of runTask: it re-lists
// bucket/prefix (listDownloadableKeysUnderPrefix - see QueueDownloadPrefixZip's
// doc comment for why the member key list is re-derived here rather than
// persisted), builds a Tracker immediately from task.TotalBytes (already
// known - set once, upfront, by QueueDownloadPrefixZip from its own listing
// pass, unlike runDownloadTask's HEAD-then-construct pattern), and streams
// every listed key into a single ZIP archive at task.DestinationPath
// (writeZipArchive).
//
// Deliberately narrower lifecycle than upload/download (per this Block's
// own scope decision, enforced by PauseTask's dedicated guard, see its own
// doc comment): a "download_zip" task can never be paused, only cancelled
// (and, after a failure, retried) - RetryTask's normal "reset to pending,
// dispatch() again" flow re-runs this function from scratch, and
// writeZipArchive's os.Create always truncates task.DestinationPath, so a
// retried archive is always written completely anew, never resumed/appended
// to a previous partial attempt. This is intentional, not a limitation to
// work around: unlike a range download's byte-addressable segments or a
// multipart upload's server-side part list, a ZIP archive being streamed
// entry-by-entry has no well-defined "resume point" once a later entry has
// already started - restarting cleanly is simpler and safer than any
// partial-archive resume scheme would be.
func (s *TransferService) runZipDownloadTask(ctx context.Context, task domain.TransferTask, rt *runningTask, pooled, fresh *s3.Client, host, bucket, prefix string) {
	keys, err := s.listDownloadableKeysUnderPrefix(pooled, fresh, host, bucket, prefix)
	if err != nil {
		s.handleTaskResult(task, rt, fmt.Errorf("re-list objects under %s/%s: %w", bucket, prefix, err), pooled, bucket, prefix, "")
		return
	}

	tracker := NewTracker(task.TotalBytes)
	rt.tracker = tracker

	stopTracker := s.startTracker(ctx, tracker, task.ID)

	zipErr := s.writeZipArchive(ctx, task, pooled, fresh, host, bucket, prefix, keys, tracker)

	// See runUploadTask/runDownloadTask's identical stopTracker() call for
	// why this must happen before handleTaskResult snapshots/emits the
	// final result.
	stopTracker()

	s.handleTaskResult(task, rt, zipErr, pooled, bucket, prefix, "")
}
