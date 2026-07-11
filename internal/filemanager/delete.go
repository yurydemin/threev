package filemanager

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"threev/internal/connection"
	"threev/internal/domain"
	"threev/internal/s3client"
)

// deleteObjectsBatchSize is the largest number of keys sent in a single S3
// DeleteObjects call - the hard limit the API itself imposes
// (docs/02-tech-spec.md section 4.4 constraint 2: "нативный batch
// DeleteObjects (до 1000 ключей/вызов, партии последовательно)").
const deleteObjectsBatchSize = 1000

// DeleteObjects starts an async bulk delete of req.Keys from req.Bucket
// (FR-BULK-002), batched by deleteObjectsBatchSize, and returns immediately
// with an operation id - it does not block until the delete actually
// finishes. Progress (including the terminal outcome) is published on the
// "bulk:progress" Wails event (see runDeleteObjects); the operation can be
// interrupted early via CancelBulkOperation(operationID).
//
// Returns an error, without starting anything, if req.Keys is empty.
//
// Guarded (Этап 4 суб-этап 4.4): the guard here runs SYNCHRONOUSLY, before
// registerBulkOp/the "go f.runDeleteObjects(...)" call below, not inside
// the spawned goroutine itself (contrast runDeleteObjects'
// resolveBulkClients call, which merely receives an already-guarded key as
// a parameter - see resolveBulkClients' own doc comment). This ordering
// matters: if the guard instead lived inside the goroutine, a locked
// application would still hand the caller a live operationID and let the
// frontend's progress overlay start tracking it, only to have the
// operation silently fail moments later - misleadingly implying an
// operation actually began. Failing fast, synchronously, before any
// operationID is ever returned, avoids that.
func (f *FileManagerService) DeleteObjects(req domain.DeleteObjectsRequest) (int64, error) {
	if len(req.Keys) == 0 {
		return 0, fmt.Errorf("delete objects: no keys given")
	}

	key, ok := f.keyBox.Get()
	if !ok {
		return 0, domain.ErrLocked
	}

	opID := f.nextOperationID()
	ctx, rt := f.registerBulkOp(opID)

	go f.runDeleteObjects(ctx, opID, req, rt, key)

	return opID, nil
}

// runDeleteObjects is the body of one DeleteObjects operation's goroutine:
// it resolves req.ProfileID's clients/host exactly like
// transfer.TransferService.runTask does, then walks req.Keys in
// deleteObjectsBatchSize-sized batches, issuing one S3 DeleteObjects call
// per batch under s3client.WithRetry/f.breaker.
//
// Two different kinds of failure are handled here, deliberately not the
// same way (see this Block's task description):
//
//   - a TRANSPORT failure (network/auth/timeout - anything WithRetry itself
//     gives up on) fails the entire batch: every key in that batch is
//     recorded as a domain.BulkOperationError carrying the transport error's
//     message, and the loop moves on to the next batch regardless (a
//     transient failure on one batch must not abort keys in a later,
//     possibly-healthy batch).
//   - a PER-KEY failure (DeleteObjectsOutput.Errors - S3 returns HTTP 200
//     for a DeleteObjects call even when some individual keys within an
//     otherwise-successful batch could not be deleted, e.g. no permission on
//     that one object) is authoritative and final: these are NOT retried
//     (retrying would just get the exact same per-key error again - the
//     batch call itself already succeeded), and are recorded directly from
//     the response.
//
// Between batches, ctx.Err() is checked: if the operation was canceled
// (CancelBulkOperation), the loop stops early and the terminal event's
// Status is "cancelled" instead of "completed" - any batch already
// in-flight when cancellation happens is still allowed to finish (ctx is
// also threaded into that batch's own WithRetry call, so an in-flight
// attempt is itself interrupted promptly too).
//
// key is the encryption key DeleteObjects already read from f.keyBox
// (synchronously, before spawning this goroutine - see DeleteObjects' own
// doc comment for why the guard cannot live here instead).
func (f *FileManagerService) runDeleteObjects(ctx context.Context, opID int64, req domain.DeleteObjectsRequest, rt *runningBulkOp, key [32]byte) {
	defer f.finishBulkOp(opID, rt)

	total := len(req.Keys)

	pooled, fresh, host, err := f.resolveBulkClients(req.ProfileID, key)
	if err != nil {
		// Nothing could even be attempted - every key counts as processed
		// and failed, and the operation still reaches a normal terminal
		// "completed" event (not a new, undocumented status value) so the
		// frontend's progress overlay always sees a terminal state.
		f.emitBulkProgressEvent(domain.BulkOperationProgressEvent{
			OperationID: opID, Type: "delete", Total: total,
			Completed: total, FailedCount: total, Status: "completed",
		})

		return
	}

	var processed, failedCount int

	cancelled := false

	for start := 0; start < total; start += deleteObjectsBatchSize {
		if ctx.Err() != nil {
			cancelled = true
			break
		}

		end := start + deleteObjectsBatchSize
		if end > total {
			end = total
		}

		batch := req.Keys[start:end]

		batchErrors, batchErr := f.deleteObjectBatch(ctx, pooled, fresh, host, req.Bucket, batch)

		processed += len(batch)

		if batchErr != nil {
			failedCount += len(batch)
		} else {
			failedCount += len(batchErrors)
		}

		f.emitBulkProgressEvent(domain.BulkOperationProgressEvent{
			OperationID: opID, Type: "delete", Total: total,
			Completed: processed, FailedCount: failedCount, Status: "running",
		})
		f.emitObjectChangeEvent(req.Bucket, objectPrefixOf(batch[0]), "delete")
	}

	status := "completed"
	if cancelled {
		status = "cancelled"
	}

	f.emitBulkProgressEvent(domain.BulkOperationProgressEvent{
		OperationID: opID, Type: "delete", Total: total,
		Completed: processed, FailedCount: failedCount, Status: status,
	})
}

// deleteObjectBatch issues one S3 DeleteObjects call for keys (at most
// deleteObjectsBatchSize of them) against bucket, under
// s3client.WithRetry/f.breaker/s3client.MetadataRetryPolicy - the
// pooled-then-fresh-client-on-retry pattern every bulk operation in this
// package follows.
//
// batchErrors holds S3's own per-key failures (DeleteObjectsOutput.Errors) -
// see runDeleteObjects' doc comment for why these are never retried and
// batchErr is separate: batchErr is non-nil only for a transport-level
// failure of the DeleteObjects call itself (after WithRetry has exhausted
// its attempts), in which case batchErrors is always empty (the call never
// got a response body to read errors from).
func (f *FileManagerService) deleteObjectBatch(ctx context.Context, pooled, fresh *s3.Client, host, bucket string, keys []string) (batchErrors []domain.BulkOperationError, batchErr error) {
	objects := make([]types.ObjectIdentifier, len(keys))
	for i, k := range keys {
		objects[i] = types.ObjectIdentifier{Key: aws.String(k)}
	}

	err := s3client.WithRetry(ctx, f.breaker, s3client.MetadataRetryPolicy, host, func(attemptCtx context.Context, isRetry bool) error {
		client := pooled
		if isRetry {
			client = fresh
		}

		out, deleteErr := client.DeleteObjects(attemptCtx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucket),
			Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(false)},
		})
		if deleteErr != nil {
			return deleteErr
		}

		batchErrors = make([]domain.BulkOperationError, 0, len(out.Errors))
		for _, e := range out.Errors {
			batchErrors = append(batchErrors, domain.BulkOperationError{
				Key:     aws.ToString(e.Key),
				Message: aws.ToString(e.Message),
			})
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return batchErrors, nil
}

// resolveBulkClients resolves profileID's decrypted profile (using key) and
// pooled/fresh S3 clients (via connection.ResolveProfile/f.connMgr.Get -
// the same two calls transfer.TransferService.runTask makes) plus the bare
// hostname s3client.WithRetry's host parameter expects, in one call -
// shared by every bulk operation's goroutine (runDeleteObjects,
// runCopyOrMove).
//
// key is received as a parameter (already guarded by each of
// DeleteObjects/CopyObjects/MoveObjects, synchronously, before their
// goroutine was ever spawned) rather than read from f.keyBox here: by the
// time this runs, on a goroutine with no caller left to hand a
// domain.ErrLocked back to, re-checking the guard here would be too late to
// matter (see DeleteObjects' own doc comment). f.connMgr.Get performs its
// own, entirely separate guard against f.connMgr's own keyBox (the same
// shared *crypto.KeyBox instance) - this call therefore effectively goes
// through two independent decrypt attempts against the same key, which is
// harmless (both trivially succeed or fail together, since they share one
// KeyBox) rather than a real double-guard.
func (f *FileManagerService) resolveBulkClients(profileID int64, key [32]byte) (pooled, fresh *s3.Client, host string, err error) {
	profile, err := connection.ResolveProfile(context.Background(), f.repo, key, profileID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("resolve profile %d: %w", profileID, err)
	}

	pooled, fresh, err = f.connMgr.Get(profileID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("get S3 clients for profile %d: %w", profileID, err)
	}

	return pooled, fresh, extractHostname(profile.EndpointURL), nil
}
