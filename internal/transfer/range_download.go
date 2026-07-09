package transfer

import (
	"context"
	"crypto/md5" //nolint:gosec // see etag.go's package-level rationale: MD5 is used only for S3 ETag-format comparison (FR-TR-004), not for any security purpose.
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"golang.org/x/sync/errgroup"

	"threev/internal/s3client"
)

// downloadSegmentReadBufferSize is the size of the buffer downloadSegment
// reads a Range GET's response body through before each os.File.WriteAt
// call. It is unrelated to segment size (PartSize) itself - it only bounds
// how much of one segment's body is held in memory at once while streaming
// it to disk, exactly analogous to why upload.go/multipart_upload.go never
// buffer a whole part in memory either.
const downloadSegmentReadBufferSize = 32 * 1024

// progressSidecarSuffix names the resume-progress sidecar file downloadRange
// maintains alongside DownloadParams.LocalPath (at LocalPath +
// progressSidecarSuffix): a plain text file, one line per successfully
// completed segment, each line the base-10 byte offset that segment starts
// at (the same offset value downloadSegmentPlan.offset/planDownloadSegments
// use). This - not p.LocalPath's own size via os.Stat - is downloadRange's
// only source of truth for "what has actually been durably downloaded so
// far":
//
//   - file.Truncate(totalBytes) (see downloadRange) pre-allocates the full,
//     final file size up front as a sparse file, purely so concurrent
//     WriteAt calls always have a valid range to write into regardless of
//     which segment finishes first. This means os.Stat(LocalPath).Size()
//     reports totalBytes from the very first moment downloadRange runs,
//     whether or not a single byte has actually been transferred yet - a
//     resume check based on local file SIZE would therefore always
//     conclude "already fully downloaded" and skip everything, silently
//     leaving the untransferred segments as unread sparse-hole zero bytes.
//     This was a real, reproduced-on-MinIO bug in an earlier version of
//     this file; the sidecar file above is what replaces the localSize-based
//     resume check entirely.
//   - a plain one-offset-per-line text file (rather than, say, a bitmap or
//     a binary format) is deliberately the simplest thing that works: it is
//     human-inspectable, trivially append-only, and tolerant of a
//     malformed/truncated trailing line (see readCompletedSegmentOffsets) -
//     there is no requirement here for compactness or fast random access,
//     only for "durably remember which segments are done" across a process
//     restart.
//   - segment (not byte) granularity is intentional: a segment is only ever
//     recorded once downloadSegment has returned nil for it (its full
//     [offset, offset+size) range written), so an interrupted, PARTIALLY
//     written segment is never treated as done - on resume it is
//     re-requested and re-written from scratch in full (WriteAt at the same
//     offsets is idempotent, so this is always safe, merely not maximally
//     bandwidth-efficient for a segment that was 99% done when interrupted).
const progressSidecarSuffix = ".transfer-progress"

// progressSidecarPath returns the resume-progress sidecar file path for
// localPath - see progressSidecarSuffix's doc comment.
func progressSidecarPath(localPath string) string {
	return localPath + progressSidecarSuffix
}

// readCompletedSegmentOffsets reads the resume-progress sidecar file for
// localPath (progressSidecarPath) and returns the set of segment offsets it
// records as durably completed by a previous, interrupted downloadRange
// run.
//
// If the sidecar file does not exist, an empty (non-nil) set is returned,
// never an error - this is the common case, and deliberately the SAME
// result whether localPath itself does not exist yet (a genuine
// from-scratch download) or localPath already exists on disk with some or
// all of the object's bytes already present (e.g. a leftover file from
// before this sidecar mechanism existed, or unrelated local content at that
// path): without a sidecar recording which segments were actually verified
// complete by THIS package, downloadRange has no safe way to know which (if
// any) of localPath's existing bytes are trustworthy, so it conservatively
// re-downloads every segment. This never risks data corruption (WriteAt is
// idempotent), only, in that specific edge case, foregoes an available
// bandwidth optimization - an accepted, documented tradeoff.
//
// A malformed line (not a valid base-10 integer - e.g. a torn write left
// over from a process that crashed mid-append) is skipped rather than
// treated as a fatal error: worst case, the segment starting at that offset
// is simply re-downloaded, exactly as if it had never been recorded at all.
func readCompletedSegmentOffsets(localPath string) (map[int64]struct{}, error) {
	data, err := os.ReadFile(progressSidecarPath(localPath)) //nolint:gosec // local download destination path, not attacker-controlled beyond what the caller already resolved
	if err != nil {
		if os.IsNotExist(err) {
			return map[int64]struct{}{}, nil
		}

		return nil, fmt.Errorf("read progress sidecar for %s: %w", localPath, err)
	}

	lines := strings.Split(string(data), "\n")
	offsets := make(map[int64]struct{}, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		offset, parseErr := strconv.ParseInt(line, 10, 64)
		if parseErr != nil {
			continue // malformed/torn line - see doc comment above
		}

		offsets[offset] = struct{}{}
	}

	return offsets, nil
}

// progressSidecar is the write side of the resume-progress sidecar file
// (progressSidecarPath): downloadRange opens exactly one of these per run
// (only once it knows at least one segment actually needs fetching) and
// every worker-pool goroutine calls recordCompleted on it as its segment
// finishes.
//
// The file is opened O_APPEND so every write is placed at the file's
// current end, but O_APPEND alone does not make a multi-byte Write from
// several concurrent goroutines atomic with respect to each other on every
// platform/filesystem - so recordCompleted also serializes every write
// through mu, rather than relying on any OS-specific O_APPEND atomicity
// guarantee. This keeps the sidecar file simple to read back
// (readCompletedSegmentOffsets never has to worry about two goroutines'
// writes interleaving mid-line) at the cost of a single mutex Lock/Unlock
// per segment completion - negligible next to the network time that just
// elapsed transferring that segment's several megabytes.
type progressSidecar struct {
	mu   sync.Mutex
	file *os.File
}

// newProgressSidecar opens (creating if necessary, and appending to any
// existing content rather than truncating it - so a sidecar left over from
// an even-earlier interrupted run is preserved, not discarded) the
// resume-progress sidecar file for localPath.
func newProgressSidecar(localPath string) (*progressSidecar, error) {
	file, err := os.OpenFile(progressSidecarPath(localPath), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644) //nolint:gosec // local download destination path, not attacker-controlled beyond what the caller already resolved
	if err != nil {
		return nil, fmt.Errorf("open progress sidecar for %s: %w", localPath, err)
	}

	return &progressSidecar{file: file}, nil
}

// recordCompleted durably appends offset (a completed segment's starting
// byte offset) as its own line to the sidecar file, synchronized against
// every other worker-pool goroutine calling this concurrently (see the
// progressSidecar doc comment for why a mutex, not O_APPEND alone).
func (s *progressSidecar) recordCompleted(offset int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := fmt.Fprintf(s.file, "%d\n", offset); err != nil {
		return err
	}

	// Durability across a crash (not just a graceful cancellation) is the
	// entire point of this sidecar file existing at all - an fsync per
	// completed segment is negligible overhead next to the many megabytes
	// of network transfer that just preceded it.
	return s.file.Sync()
}

// close closes the sidecar file's underlying handle. Safe to call at most
// once (mirrors os.File.Close itself).
func (s *progressSidecar) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.file.Close()
}

// removeProgressSidecar deletes the resume-progress sidecar file for
// localPath, ignoring a "does not exist" error (and any other removal
// error - this is always a best-effort cleanup step run after the sidecar
// has already served its purpose: a leftover sidecar file on some other
// removal failure merely costs the next Download() call for the same
// LocalPath a redundant readCompletedSegmentOffsets, never any risk of data
// loss or corruption).
func removeProgressSidecar(localPath string) {
	_ = os.Remove(progressSidecarPath(localPath))
}

// downloadSegmentPlan is one Range GET this download's worker pool will
// issue: read [offset, offset+size) of the remote object and write it to
// the local file starting at byte offset. Built by planDownloadSegments
// from the full-file segment layout (PartSize(totalBytes)) and the set of
// segment offsets already recorded as completed in the resume-progress
// sidecar file (readCompletedSegmentOffsets) - a segment whose offset is in
// that set is dropped from the plan entirely; every other segment is kept
// UNCHANGED (never shrunk to a partial tail - see progressSidecarSuffix's
// doc comment on why resume tracking is whole-segment granularity, not
// byte granularity).
type downloadSegmentPlan struct {
	offset, size int64
}

// downloadRange runs the range-download algorithm (docs/02-tech-spec.md
// section 10.3) for p.LocalPath, once DownloadParams.LocalPath's parent
// directory is known to exist and totalBytes (from HeadObject) is known -
// both handled by the caller, Download, before downloadRange is ever
// called:
//
//  1. read the resume-progress sidecar file (readCompletedSegmentOffsets)
//     to learn which segments, if any, a previous interrupted run of this
//     exact LocalPath already durably completed - NOT by inspecting
//     p.LocalPath's own size, see progressSidecarSuffix's doc comment for
//     why that would be actively wrong here.
//  2. open (or create) the local file and Truncate it to totalBytes: on
//     most filesystems this creates/extends a sparse file, and guarantees
//     every downloadSegment's WriteAt call has a valid range to write into
//     regardless of concurrent segment completion order.
//  3. slice totalBytes into segments using the same PartSize(totalBytes)
//     adaptive table multipart_upload.go uses for uploads
//     (docs/02-tech-spec.md section 10.3: "default 16 МБ", which PartSize's
//     table already produces for files in the relevant size range - see
//     partsize.go's doc comment for the full table), dropping any segment
//     already recorded as completed (planDownloadSegments). If every
//     segment was already completed on a prior run, the plan is empty:
//     downloadRange removes the now-redundant sidecar file and returns
//     immediately without making any network request at all - the
//     resume-complete equivalent of the old (and, for the reasons above,
//     removed) "localSize >= totalBytes" fast path.
//  4. run every remaining segment through downloadSegment via an errgroup
//     worker pool, bounded by effectiveConcurrency (shared with
//     multipart_upload.go - see its doc comment). As each segment's
//     downloadSegment call returns successfully, its offset is durably
//     appended to the sidecar file (progressSidecar.recordCompleted) BEFORE
//     that worker-pool goroutine returns - so a crash/cancellation at any
//     point afterward can never lose track of a segment that genuinely
//     finished.
//  5. once every segment has succeeded, the sidecar file has served its
//     purpose (a future Download() of this LocalPath will simply see the
//     final, complete file with no sidecar and, per
//     readCompletedSegmentOffsets's documented safe-default, re-verify
//     rather than trust it - which is fine, since at that point the object
//     is either identical, in which case FR-TR-004's own integrity check
//     still runs, or intentionally replaced) - so it is closed and removed.
func downloadRange(ctx context.Context, p DownloadParams, totalBytes int64) error {
	completed, err := readCompletedSegmentOffsets(p.LocalPath)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(p.LocalPath, os.O_RDWR|os.O_CREATE, 0o644) //nolint:gosec // local download destination path, not attacker-controlled beyond what the caller already resolved
	if err != nil {
		return fmt.Errorf("open %s: %w", p.LocalPath, err)
	}
	defer func() { _ = file.Close() }()

	if err := file.Truncate(totalBytes); err != nil {
		return fmt.Errorf("truncate %s to %d bytes: %w", p.LocalPath, totalBytes, err)
	}

	segments := planDownloadSegments(totalBytes, completed)

	if len(segments) == 0 {
		// Every segment the current plan calls for is already recorded as
		// completed - nothing left to fetch. Mirrors the old
		// "localSize >= totalBytes" fast path, but driven by the sidecar,
		// not by local file size (see progressSidecarSuffix's doc comment).
		removeProgressSidecar(p.LocalPath)

		return nil
	}

	progress, err := newProgressSidecar(p.LocalPath)
	if err != nil {
		return err
	}
	defer func() { _ = progress.close() }()

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(effectiveConcurrency(p.Concurrency, int64(len(segments))))

	for _, seg := range segments {
		group.Go(func() error {
			if err := downloadSegment(groupCtx, p, seg.offset, seg.size, file); err != nil {
				return fmt.Errorf("segment [%d, %d): %w", seg.offset, seg.offset+seg.size, err)
			}

			if err := progress.recordCompleted(seg.offset); err != nil {
				return fmt.Errorf("record completed segment at offset %d: %w", seg.offset, err)
			}

			return nil
		})
	}

	// Mirrors uploadMultipart's use of errgroup.WithContext: as soon as one
	// segment's closure returns a non-nil error, groupCtx is canceled, so
	// remaining in-flight/queued segments stop (their s3client.WithRetry
	// attempts observe ctx.Done() and return context.Canceled) instead of
	// continuing to burn bandwidth on a download that is already a lost
	// cause. The sidecar file (and whatever offsets it accumulated for
	// segments that DID finish before the failure) is deliberately left in
	// place in this case, exactly as uploadMultipart leaves a failed MPU's
	// server-side parts in place: a later resume needs it.
	if err := group.Wait(); err != nil {
		return err
	}

	if err := progress.close(); err != nil {
		return fmt.Errorf("close progress sidecar for %s: %w", p.LocalPath, err)
	}

	removeProgressSidecar(p.LocalPath)

	return nil
}

// planDownloadSegments lays totalBytes out into PartSize(totalBytes)-sized
// segments (the same full-file layout a from-scratch download would use),
// then drops any segment whose starting offset is present in completed (a
// set of segment offsets already recorded as durably finished in the
// resume-progress sidecar file, from readCompletedSegmentOffsets) - so it
// is never re-requested, which is what makes resume not re-download
// already-completed segments. Every other segment is returned UNCHANGED
// (start/size are never adjusted to a partial tail): resume tracking here
// is whole-segment granularity, not byte granularity - see
// progressSidecarSuffix's doc comment for why.
//
// The returned segments are always ordered by ascending offset, though
// downloadRange's worker pool does not depend on that ordering itself
// (segments run concurrently, each independently WriteAt-ing its own
// range).
func planDownloadSegments(totalBytes int64, completed map[int64]struct{}) []downloadSegmentPlan {
	segmentSize := PartSize(totalBytes)
	segmentCount := PartCount(totalBytes, segmentSize)

	segments := make([]downloadSegmentPlan, 0, segmentCount)

	for n := int64(0); n < segmentCount; n++ {
		start := n * segmentSize
		end := start + segmentSize

		if end > totalBytes {
			end = totalBytes
		}

		if _, done := completed[start]; done {
			continue // already durably completed on a previous run
		}

		segments = append(segments, downloadSegmentPlan{offset: start, size: end - start})
	}

	return segments
}

// downloadSegment fetches bytes [offset, offset+size) of p.Bucket/p.Key via
// a Range GetObject under s3client.PartRetryPolicy (the same policy a
// multipart-upload part uses - see uploadPart's doc comment for why a
// segment transfer, not just a metadata call, gets the full 5-attempt
// schedule), writing them into file at the same offset as they are read off
// the response body.
//
// Each retry attempt gets its own GetObject call and therefore its own,
// brand-new http.Response.Body - unlike uploadPart's io.NewSectionReader (a
// LOCAL reader that genuinely needs to be reconstructed per attempt to
// avoid reusing a partially-consumed one, see its doc comment), there is no
// equivalent risk to guard against here: a fresh remote response is
// inherently produced by every new GetObject call. What still matters is
// that the local read/write bookkeeping (the buffer and the written byte
// counter) lives INSIDE the s3client.WithRetry attempt closure rather than
// being shared across attempts - a failed attempt's partial writes to file
// are harmless (WriteAt at the same offsets is idempotently overwritten by
// the next attempt), but reusing its written counter across attempts would
// wrongly resume mid-segment on retry instead of re-fetching the whole
// range from offset 0.
func downloadSegment(ctx context.Context, p DownloadParams, offset, size int64, file *os.File) error {
	return s3client.WithRetry(ctx, p.Breaker, s3client.PartRetryPolicy, p.Host, func(attemptCtx context.Context, isRetry bool) error {
		client := p.Pooled
		if isRetry {
			client = p.Fresh
		}

		timeoutCtx, cancel := context.WithTimeout(attemptCtx, s3client.AdaptiveTimeout(size, 0))
		defer cancel()

		resp, err := client.GetObject(timeoutCtx, &s3.GetObjectInput{
			Bucket: aws.String(p.Bucket),
			Key:    aws.String(p.Key),
			Range:  aws.String(fmt.Sprintf("bytes=%d-%d", offset, offset+size-1)),
		})
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		var written int64

		buf := make([]byte, downloadSegmentReadBufferSize)

		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				if _, writeErr := file.WriteAt(buf[:n], offset+written); writeErr != nil {
					return fmt.Errorf("write at offset %d: %w", offset+written, writeErr)
				}

				written += int64(n)

				if p.Hooks.OnBytesTransferred != nil {
					p.Hooks.OnBytesTransferred(int64(n))
				}
			}

			if readErr != nil {
				if readErr == io.EOF { //nolint:errorlint // io.EOF is a sentinel value by contract (io.Reader), never wrapped
					break
				}

				return readErr
			}
		}

		if written != size {
			return fmt.Errorf("range GET returned %d bytes, want %d (bytes=%d-%d)", written, size, offset, offset+size-1)
		}

		return nil
	})
}

// verifyDownloadIntegrity performs a best-effort integrity check
// (FR-TR-004) of a just-downloaded p.LocalPath against expectedETag (the
// HEAD/GetObject-reported ETag, already stripped of surrounding quotes by
// the caller, Download), mirroring upload.go/etag.go's split between a
// plain, single-part ETag and a multipart-source composite ETag:
//
//   - plainETagPattern ("^[0-9a-f]{32}$", case-insensitive): expectedETag is
//     a bare MD5 digest, exactly what a locally computed MD5 of the whole
//     downloaded file should equal - the file is hashed and compared,
//     case-insensitively, and the result is returned as verified.
//   - compositeETagPattern ("^[0-9a-fA-F]{32}-\\d+$"): expectedETag is a
//     multipart-upload composite ETag. Unlike verifyMultipartETag's upload
//     side (which can recompute the exact same composite from the ETags of
//     the parts IT just uploaded), a download has no way to know how the
//     ORIGINAL uploader split the object into parts - the part boundaries
//     are not recoverable from the object alone - so a byte-for-byte MD5
//     check against this ETag format is not possible at all, not just
//     inconvenient. verified=false is returned for this case (never
//     verified), but - unlike the upload side, where an unverifiable ETag
//     is simply ignored - a download's local file size is always checked
//     against totalBytes here regardless: an incomplete file on disk after
//     downloadRange reports success is a genuine bug (a segment silently
//     wrote fewer bytes than planned, or file.Truncate/WriteAt did
//     something unexpected), not merely "verification not applicable", so
//     a size mismatch is returned as a non-nil error rather than folded
//     into the same verified=false/err=nil "not applicable" result an
//     unrecognized ETag format gets.
//   - any other format (SSE-KMS/non-standard providers, exactly as
//     upload.go's verifySingleETag/verifyMultipartETag already treat this):
//     verified=false, err=nil - verification is skipped, not failed.
func verifyDownloadIntegrity(localPath string, expectedETag string, totalBytes int64) (verified bool, err error) {
	clean := strings.Trim(expectedETag, `"`)

	switch {
	case plainETagPattern.MatchString(clean):
		return verifyDownloadedFileMD5(localPath, clean)

	case compositeETagPattern.MatchString(clean):
		info, statErr := os.Stat(localPath)
		if statErr != nil {
			return false, fmt.Errorf("stat %s: %w", localPath, statErr)
		}

		if info.Size() != totalBytes {
			return false, fmt.Errorf("downloaded file %s is %d bytes, want %d (incomplete download)", localPath, info.Size(), totalBytes)
		}

		// Byte-for-byte verification is not possible against a
		// multipart-source composite ETag (see doc comment above); the
		// size check above is the only integrity signal available, and
		// it passed.
		return false, nil

	default:
		return false, nil
	}
}

// verifyDownloadedFileMD5 hashes the whole file at localPath and reports
// whether it matches expectedMD5 (a bare, already-lowercased-or-not hex
// MD5 digest - compared case-insensitively, same as verifySingleETag).
func verifyDownloadedFileMD5(localPath string, expectedMD5 string) (bool, error) {
	file, err := os.Open(localPath) //nolint:gosec // local download destination path, not attacker-controlled beyond what the caller already resolved
	if err != nil {
		return false, fmt.Errorf("open %s: %w", localPath, err)
	}
	defer func() { _ = file.Close() }()

	hasher := md5.New() //nolint:gosec // see package-level rationale above
	if _, err := io.Copy(hasher, file); err != nil {
		return false, fmt.Errorf("hash %s: %w", localPath, err)
	}

	return strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), expectedMD5), nil
}
