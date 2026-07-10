package filemanager

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"threev/internal/domain"
	"threev/internal/s3client"
)

// copyMoveWorkerCount is the fixed worker pool size CopyObjects/MoveObjects
// use (docs/02-tech-spec.md section 4.4 constraint 3: "worker pool, 8
// воркеров (некороткие поштучные CopyObject, выше чем
// DefaultPartConcurrency=4)") - not configurable in this MVP.
const copyMoveWorkerCount = 8

// bulkProgressEmitInterval is how often runCopyOrMove's ticker goroutine
// publishes a "running" bulk:progress event while a worker pool is active -
// throttled per the Этап 4 plan's "Зафиксированные решения" item 5
// ("throttled ~250мс"), so a bulk operation over thousands of small objects
// never floods the Wails event bridge with one event per key.
const bulkProgressEmitInterval = 250 * time.Millisecond

// bulkCopyMoveParams is the common, direction-agnostic shape CopyObjects and
// MoveObjects both reduce their request to before handing off to
// runCopyOrMove - Move differs from Copy only in that copyOneObject also
// deletes the source key after a successful CopyObject.
type bulkCopyMoveParams struct {
	ProfileID    int64
	SourceBucket string
	Keys         []string
	DestBucket   string
	DestPrefix   string
	Move         bool
}

// CopyObjects starts an async bulk copy of req.Keys from req.SourceBucket to
// req.DestBucket/req.DestPrefix (FR-BULK-003) and returns immediately with
// an operation id - see runCopyOrMove for the worker pool/progress/cancel
// mechanics shared with MoveObjects.
func (f *FileManagerService) CopyObjects(req domain.BulkCopyRequest) (int64, error) {
	if len(req.Keys) == 0 {
		return 0, fmt.Errorf("copy objects: no keys given")
	}

	opID := f.nextOperationID()
	ctx, rt := f.registerBulkOp(opID)

	go f.runCopyOrMove(ctx, opID, "copy", bulkCopyMoveParams{
		ProfileID:    req.ProfileID,
		SourceBucket: req.SourceBucket,
		Keys:         req.Keys,
		DestBucket:   req.DestBucket,
		DestPrefix:   req.DestPrefix,
		Move:         false,
	}, rt)

	return opID, nil
}

// MoveObjects starts an async bulk move of req.Keys from req.SourceBucket to
// req.DestBucket/req.DestPrefix (FR-BULK-003: CopyObject + DeleteObject) and
// returns immediately with an operation id - see runCopyOrMove/
// copyOneObject for the copy-then-delete-that-same-key ordering this
// implements and why.
func (f *FileManagerService) MoveObjects(req domain.BulkMoveRequest) (int64, error) {
	if len(req.Keys) == 0 {
		return 0, fmt.Errorf("move objects: no keys given")
	}

	opID := f.nextOperationID()
	ctx, rt := f.registerBulkOp(opID)

	go f.runCopyOrMove(ctx, opID, "move", bulkCopyMoveParams{
		ProfileID:    req.ProfileID,
		SourceBucket: req.SourceBucket,
		Keys:         req.Keys,
		DestBucket:   req.DestBucket,
		DestPrefix:   req.DestPrefix,
		Move:         true,
	}, rt)

	return opID, nil
}

// runCopyOrMove is the shared body of one CopyObjects/MoveObjects
// operation's goroutine: opType is "copy" or "move" (the
// BulkOperationProgressEvent.Type value), params.Move selects whether
// copyOneObject also deletes each successfully-copied source key.
//
// Concurrency: copyMoveWorkerCount (8) workers pull keys off a single jobs
// channel fed by params.Keys in order; each worker checks ctx.Err() BEFORE
// taking its next job (never mid-key) - once canceled, a worker stops
// taking new work but any key it already started is allowed to finish (or
// fail/be interrupted on its own, via WithRetry observing the same canceled
// ctx). completed/failed are tracked with atomic.Int64 counters read by a
// separate ticker goroutine (bulkProgressEmitInterval) for "running"
// progress events - never incremented from more than one place per key, so
// no additional locking is needed for them; per-key failures are appended
// to a mutex-guarded slice (not used by any caller today - domain.
// BulkOperationResult exists for callers that grow to want it - but
// collected regardless since Stage 4's task description calls for it).
//
// The final bulk:progress event is emitted synchronously, strictly after
// the worker pool's sync.WaitGroup.Wait() returns (guaranteeing it reflects
// the exact final count, never a stale tick from the throttled ticker), with
// Status "completed" or "cancelled" depending on whether ctx was canceled.
func (f *FileManagerService) runCopyOrMove(ctx context.Context, opID int64, opType string, params bulkCopyMoveParams, rt *runningBulkOp) {
	defer f.finishBulkOp(opID, rt)

	total := len(params.Keys)

	pooled, fresh, host, err := f.resolveBulkClients(params.ProfileID)
	if err != nil {
		// See runDeleteObjects' identical up-front-failure handling: every
		// key counts as processed and failed, terminal status is still the
		// ordinary "completed" (never a new, undocumented status value).
		f.emitBulkProgressEvent(domain.BulkOperationProgressEvent{
			OperationID: opID, Type: opType, Total: total,
			Completed: total, FailedCount: total, Status: "completed",
		})

		return
	}

	var completed, failed atomic.Int64

	var failuresMu sync.Mutex

	var failures []domain.BulkOperationError //nolint:prealloc // size unknown up front (number of failures, not total keys)

	jobs := make(chan string)

	go func() {
		defer close(jobs)

		for _, key := range params.Keys {
			select {
			case jobs <- key:
			case <-ctx.Done():
				return
			}
		}
	}()

	progressStop := make(chan struct{})

	var progressWG sync.WaitGroup

	progressWG.Add(1)

	go func() {
		defer progressWG.Done()

		ticker := time.NewTicker(bulkProgressEmitInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				f.emitBulkProgressEvent(domain.BulkOperationProgressEvent{
					OperationID: opID, Type: opType, Total: total,
					Completed: int(completed.Load()), FailedCount: int(failed.Load()), Status: "running",
				})
			case <-progressStop:
				return
			}
		}
	}()

	var workersWG sync.WaitGroup

	for i := 0; i < copyMoveWorkerCount; i++ {
		workersWG.Add(1)

		go func() {
			defer workersWG.Done()

			for {
				if ctx.Err() != nil {
					return
				}

				key, ok := <-jobs
				if !ok {
					return
				}

				if copyErr := f.copyOneObject(ctx, pooled, fresh, host, params, key); copyErr != nil {
					failuresMu.Lock()
					failures = append(failures, domain.BulkOperationError{Key: key, Message: copyErr.Error()})
					failuresMu.Unlock()

					failed.Add(1)
				}

				completed.Add(1)
			}
		}()
	}

	workersWG.Wait()
	close(progressStop)
	progressWG.Wait()

	cancelled := ctx.Err() != nil

	status := "completed"
	if cancelled {
		status = "cancelled"
	}

	finalCompleted := int(completed.Load())
	finalFailed := int(failed.Load())

	f.emitBulkProgressEvent(domain.BulkOperationProgressEvent{
		OperationID: opID, Type: opType, Total: total,
		Completed: finalCompleted, FailedCount: finalFailed, Status: status,
	})

	// Only announce a listing change if at least one key actually
	// succeeded - see this function's own doc comment.
	if finalCompleted-finalFailed <= 0 || len(params.Keys) == 0 {
		return
	}

	f.emitObjectChangeEvent(params.DestBucket, params.DestPrefix, "create")

	if params.Move {
		f.emitObjectChangeEvent(params.SourceBucket, objectPrefixOf(params.Keys[0]), "delete")
	}
}

// copyOneObject performs the copy (and, for a move, the copy-then-delete)
// of a single sourceKey under params, using host/pooled/fresh exactly like
// deleteObjectBatch: pooled on the first attempt of each WithRetry call,
// fresh on any retry.
//
// destKey is params.DestPrefix + path.Base(sourceKey) (path.Base, not
// filepath.Base - S3 keys always use "/" regardless of the host OS) - see
// domain.BulkCopyRequest's doc comment for why this is a flat, sibling-only
// destination layout (no relative sub-path preserved).
//
// Move ordering (copy-then-delete-THAT-SAME-key, never "copy everything
// then delete everything"): copyOneObject only ever calls DeleteObject for
// sourceKey after CopyObject for that exact key has already succeeded. If
// the process is interrupted partway through a bulk move, every key that
// was already copied-and-deleted is a genuine move, every key not yet
// reached is untouched, and - critically - a key whose CopyObject succeeded
// but whose DeleteObject has not yet run (or failed) is left as a safe
// DUPLICATE (both source and destination exist) rather than a source that
// was deleted before its copy was ever confirmed. A duplicate is a
// recoverable annoyance (the user can delete the leftover source manually);
// a prematurely deleted source with no confirmed copy would be silent data
// loss - the asymmetry is why this order, and never the reverse, is used.
func (f *FileManagerService) copyOneObject(ctx context.Context, pooled, fresh *s3.Client, host string, params bulkCopyMoveParams, sourceKey string) error {
	destKey := params.DestPrefix + path.Base(sourceKey)

	err := s3client.WithRetry(ctx, f.breaker, s3client.MetadataRetryPolicy, host, func(attemptCtx context.Context, isRetry bool) error {
		client := pooled
		if isRetry {
			client = fresh
		}

		_, copyErr := client.CopyObject(attemptCtx, &s3.CopyObjectInput{
			Bucket:     aws.String(params.DestBucket),
			Key:        aws.String(destKey),
			CopySource: aws.String(copySourceFor(params.SourceBucket, sourceKey)),
		})

		return copyErr
	})
	if err != nil {
		return fmt.Errorf("copy %s/%s -> %s/%s: %w", params.SourceBucket, sourceKey, params.DestBucket, destKey, err)
	}

	if !params.Move {
		return nil
	}

	delErr := s3client.WithRetry(ctx, f.breaker, s3client.MetadataRetryPolicy, host, func(attemptCtx context.Context, isRetry bool) error {
		client := pooled
		if isRetry {
			client = fresh
		}

		_, deleteErr := client.DeleteObject(attemptCtx, &s3.DeleteObjectInput{
			Bucket: aws.String(params.SourceBucket),
			Key:    aws.String(sourceKey),
		})

		return deleteErr
	})
	if delErr != nil {
		return fmt.Errorf("copied %s/%s -> %s/%s but failed to delete source: %w", params.SourceBucket, sourceKey, params.DestBucket, destKey, delErr)
	}

	return nil
}

// copySourceFor builds the value of CopyObjectInput.CopySource for
// bucket/key: every path SEGMENT of key is percent-encoded individually via
// url.PathEscape, then rejoined with literal "/" separators - never
// url.QueryEscape(bucket+"/"+key) as a single string, which would encode
// key's own internal "/" characters as "%2F" too, and S3 does NOT decode
// "%2F" back into a path separator when parsing CopySource: it would be
// interpreted as one single (non-existent) key segment containing a literal
// "%2F", breaking every copy of a key that itself contains "/" (i.e. nearly
// every key in this application, since "folders" are modeled as key
// prefixes).
func copySourceFor(bucket, key string) string {
	segments := strings.Split(key, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}

	return url.PathEscape(bucket) + "/" + strings.Join(segments, "/")
}
