package transfer

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"threev/internal/domain"
	"threev/internal/mimetype"
	"threev/internal/s3client"
)

// crossConnectionTempPath returns the local staging file path
// runCrossConnectionCopyTask downloads task taskID's source object into,
// before it is uploaded to the destination profile - see
// runCrossConnectionCopyTask's own doc comment for why a "copy_cross" task
// needs a local staging file at all (a single, server-side CopyObject
// cannot span two different S3 endpoints/credential sets, unlike
// filemanager/copymove.go's same-profile fast path, which this Block
// deliberately leaves untouched).
//
// Deterministic per taskID (never a randomly generated name): a
// Retry-triggered, or Pause-then-Resume-triggered, re-run of the SAME task
// therefore lands on the exact same path every time - letting Download's
// own resume-progress sidecar mechanism (range_download.go's
// progressSidecarPath, keyed off LocalPath) transparently pick up wherever
// a previous, now-interrupted attempt left off, exactly as it already does
// for a plain "download" task's LocalPath. path.Base(sourceKey) (rather
// than the full, possibly "/"-nested key) is used only for the file's own
// name, purely for readability were a developer ever inspecting
// os.TempDir() by hand mid-transfer; taskID's own path segment is what
// actually guarantees no collision between two different copy_cross tasks,
// even two that happen to share the exact same source key.
func crossConnectionTempPath(taskID int64, sourceKey string) string {
	return filepath.Join(os.TempDir(), "threev-cross-copy", strconv.FormatInt(taskID, 10), path.Base(sourceKey))
}

// removeCrossConnectionTempFileBestEffort removes path (the local staging
// file left behind by a just-succeeded runCrossConnectionCopyTask - see its
// own doc comment for why it is only ever removed on full, both-phases
// success, never on a failure/pause/cancel), logging - never propagating -
// any error other than the file already not existing, mirroring
// zip_download.go's identical removeArchiveBestEffort: cleanup of a file
// that will otherwise simply sit on disk, unusable, must never itself block
// a task's own success from being recorded.
func removeCrossConnectionTempFileBestEffort(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("transfer: remove cross-connection staging file %s: %v", path, err)
	}
}

// runCrossConnectionCopyTask runs the "copy_cross" side of runTask: it
// stages sourceBucket/sourceKey (resolved against the SOURCE profile's
// sourcePooled/sourceFresh/sourceHost) into a local temp file
// (crossConnectionTempPath) via the ordinary Download, then uploads that
// same temp file to task.DestinationPath's bucket/key (resolved against the
// DESTINATION profile's destPooled/destFresh/destHost, decoded here since
// taskBucketKey's own one-bucket/key-pair contract only ever returns the
// SOURCE side - see its doc comment) via the ordinary Upload - reusing both
// unmodified, exactly as every other task type in this package does, rather
// than this package growing any S3-to-S3 streaming logic of its own.
//
// A single, shared Tracker is used across BOTH phases (constructed once,
// from task.TotalBytes - already known from the HeadObject
// QueueCopyBetweenProfiles performed at queue time, unlike a plain
// "download" task's own HeadObject-then-construct dance in
// runDownloadTask): every byte is therefore counted TWICE over the full
// task - once as it is written to the local staging file (Download's
// OnBytesTransferred) and again as it is read back off it to build the
// upload's request body (Upload's OnBytesTransferred). This is an accepted,
// deliberate MVP simplification (per this Block's own task description),
// not a bug to be worked around: a copy_cross task's progress bar
// legitimately reflects two full passes of I/O work (a real GetObject
// followed by a real PutObject/multipart upload, not a single, cheaper
// server-side CopyObject), so a transferred count that can momentarily
// exceed a naive reading of "100% of the object's size" is simply an
// honest reflection of the actual, doubled amount of work this task
// performs - not a defect in Tracker itself (see its own Snapshot doc
// comment: an ETA/percentage display built from these numbers is left
// entirely to the frontend).
//
// Move semantics (task.IsMove): the source object is deleted ONLY after
// the destination upload has already fully succeeded - never before, and
// never at all if either phase fails - mirroring
// filemanager/copymove.go's own copy-then-delete-only-after-success
// ordering (see its copyOneObject doc comment) for exactly the same
// reason: a source object must never be lost while its copy on the
// destination side is still unconfirmed. A failure to delete the
// now-redundant source object surfaces as THIS task's own failure too
// (mirroring copyOneObject's identical choice, not silently swallowed) -
// the already-uploaded destination object is deliberately never rolled
// back in that case, left as a safe DUPLICATE (both source and destination
// now exist) rather than risking the reverse: a source deleted before its
// copy was ever durably confirmed. A duplicate is a recoverable annoyance
// (RetryTask re-runs the whole task, or the user deletes the leftover
// source by hand); a prematurely deleted, unconfirmed source would be
// silent data loss - see copyOneObject's own doc comment for the identical
// asymmetry argument.
//
// Pause/Cancel: ctx (shared with every other task type's own S3 calls, per
// runTask/startTask) is passed unchanged into both the Download and Upload
// calls below - a Pause/Cancel during either phase surfaces as
// context.Canceled from whichever call was in flight, handled by
// handleTaskResult exactly like any other task type's cancellation. No
// extra bookkeeping is needed for this to be safely resumable: Download's
// own resume-progress sidecar (keyed off the deterministic tempPath above)
// resumes an interrupted DOWNLOAD phase transparently, and an interrupted
// UPLOAD phase resumes via task.MultipartUploadID exactly like a plain
// "upload" task's own ExistingUploadID path (runUploadTask) - the staging
// file itself is left untouched on disk in either case (never removed
// except on full success, see removeCrossConnectionTempFileBestEffort's
// doc comment), so a resumed Upload phase reads back the exact same,
// already-complete local bytes Download left behind on the interrupted
// attempt, with nothing to re-download.
func (s *TransferService) runCrossConnectionCopyTask(
	ctx context.Context,
	task domain.TransferTask,
	rt *runningTask,
	sourcePooled, sourceFresh *s3.Client,
	sourceHost string,
	destPooled, destFresh *s3.Client,
	destHost string,
	sourceBucket, sourceKey string,
) {
	destBucket, destKey, destErr := splitBucketKey(task.DestinationPath)
	if destErr != nil {
		s.handleTaskResult(task, rt, fmt.Errorf("decode destination bucket/key: %w", destErr), nil, "", "", "")
		return
	}

	tempPath := crossConnectionTempPath(task.ID, sourceKey)

	tracker := NewTracker(task.TotalBytes)
	rt.tracker = tracker

	stopTracker := s.startTracker(ctx, tracker, task.ID)

	// Seeded from task.MultipartUploadID for the same reason
	// runUploadTask's identical assignedUploadID is: a task whose
	// destination-side CreateMultipartUpload already succeeded on a
	// previous, now-Failed (or Paused-during-upload-phase) attempt still
	// has its UploadId on hand here if THIS attempt is itself interrupted
	// before the upload phase even runs (e.g. a Cancel arriving during the
	// download phase) - see handleTaskResult's own abort step, which needs
	// this exact id.
	assignedUploadID := task.MultipartUploadID

	downloadParams := DownloadParams{
		Pooled:        sourcePooled,
		Fresh:         sourceFresh,
		Breaker:       s.breaker,
		RetryPolicies: s.retryPolicies,
		Host:          sourceHost,
		Limiter:       s.limiter.Load(),

		Bucket:    sourceBucket,
		Key:       sourceKey,
		LocalPath: tempPath,

		PartSizeOverride: s.partSizeOverride.Load(),
		Concurrency:      DefaultPartConcurrency,

		Hooks: DownloadHooks{OnBytesTransferred: tracker.AddBytes},
	}

	if _, downloadErr := Download(ctx, downloadParams); downloadErr != nil {
		stopTracker()
		s.handleTaskResult(task, rt, fmt.Errorf("download source object %s/%s: %w", sourceBucket, sourceKey, downloadErr), destPooled, destBucket, destKey, assignedUploadID)
		return
	}

	uploadParams := UploadParams{
		Pooled:        destPooled,
		Fresh:         destFresh,
		Breaker:       s.breaker,
		RetryPolicies: s.retryPolicies,
		Host:          destHost,
		Limiter:       s.limiter.Load(),

		Bucket:      destBucket,
		Key:         destKey,
		LocalPath:   tempPath,
		ContentType: mimetype.ContentTypeForKey(destKey),
		TotalBytes:  task.TotalBytes,

		PartSizeOverride: s.partSizeOverride.Load(),

		ExistingUploadID: task.MultipartUploadID,
		Concurrency:      DefaultPartConcurrency,

		Hooks: UploadHooks{
			OnMultipartUploadIDAssigned: func(uploadID string) error {
				assignedUploadID = uploadID
				return s.queueRepo.UpdateMultipartUploadID(context.Background(), task.ID, uploadID)
			},
			OnBytesTransferred: tracker.AddBytes,
		},
	}

	_, uploadErr := Upload(ctx, uploadParams)

	// See runUploadTask/runDownloadTask/runZipDownloadTask's identical
	// stopTracker() call for why this must happen before handleTaskResult
	// snapshots/emits the final result - here, once, right after the
	// SECOND (upload) phase, since a Tracker is shared across both phases
	// for the whole task (see this function's own doc comment).
	stopTracker()

	if uploadErr != nil {
		s.handleTaskResult(task, rt, fmt.Errorf("upload to destination %s/%s: %w", destBucket, destKey, uploadErr), destPooled, destBucket, destKey, assignedUploadID)
		return
	}

	removeCrossConnectionTempFileBestEffort(tempPath)

	if task.IsMove {
		if delErr := s.deleteCrossConnectionSourceObject(context.Background(), sourcePooled, sourceFresh, sourceHost, sourceBucket, sourceKey); delErr != nil {
			s.handleTaskResult(task, rt, fmt.Errorf("copied %s/%s -> %s/%s but failed to delete source: %w", sourceBucket, sourceKey, destBucket, destKey, delErr), destPooled, destBucket, destKey, assignedUploadID)
			return
		}
	}

	s.handleTaskResult(task, rt, nil, destPooled, destBucket, destKey, assignedUploadID)
}

// deleteCrossConnectionSourceObject deletes sourceBucket/sourceKey from the
// SOURCE profile (sourcePooled/sourceFresh/sourceHost), under
// s3client.WithRetry/s.retryPolicies.Metadata() - the same policy every
// other metadata-only S3 call in this package uses (e.g.
// abortOrphanedMultipartUpload's AbortMultipartUpload, listObjectsPageForDownloadPrefix's
// ListObjectsV2) - so a move's source-side delete is never left bypassing
// the shared retry/circuit-breaker machinery, mirroring
// filemanager/copymove.go's own DeleteObject-under-WithRetry call exactly.
// Called only from runCrossConnectionCopyTask, only after a confirmed
// upload success (see its own doc comment for why that ordering is never
// reversed).
func (s *TransferService) deleteCrossConnectionSourceObject(ctx context.Context, sourcePooled, sourceFresh *s3.Client, sourceHost, sourceBucket, sourceKey string) error {
	return s3client.WithRetry(ctx, s.breaker, s.retryPolicies.Metadata(), sourceHost, func(attemptCtx context.Context, isRetry bool) error {
		client := sourcePooled
		if isRetry {
			client = sourceFresh
		}

		_, err := client.DeleteObject(attemptCtx, &s3.DeleteObjectInput{
			Bucket: aws.String(sourceBucket),
			Key:    aws.String(sourceKey),
		})

		return err
	})
}
