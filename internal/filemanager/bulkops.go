package filemanager

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"threev/internal/domain"
)

// wailsBulkProgressEvent is the Wails event name FileManagerService
// publishes bulk delete/copy/move progress on (docs/02-tech-spec.md section
// 9.5's Stage 4 addition, "Зафиксированные решения" item 5 of the Этап 4
// plan), carrying a domain.BulkOperationProgressEvent payload - by direct
// analogy with transfer.wailsProgressEvent/"transfer:progress".
const wailsBulkProgressEvent = "bulk:progress"

// wailsObjectChangeEvent is the Wails event name FileManagerService
// publishes on whenever a bulk operation (or CreateFolder/UpdateMetadata)
// creates or removes an object under a bucket/prefix, carrying a
// domain.ObjectChangeEvent payload. This is the exact same event name and
// payload shape transfer.TransferService already publishes on - see this
// file's emitObjectChangeEvent doc comment for why that matters -
// duplicated here (not imported) since filemanager has no dependency on the
// transfer package.
const wailsObjectChangeEvent = "object:change"

// runningBulkOp is the in-memory state of one actively executing bulk
// operation (delete/copy/move), held only in FileManagerService.running for
// exactly as long as its goroutine is alive - mirrors transfer.runningTask
// (Stage 3) exactly, duplicated here rather than imported since filemanager
// has no dependency on the transfer package (see this package's other
// duplicated-pattern precedent, s3client.ConnectionManager.resolveProfile
// vs connection.ResolveProfile, for the same avoid-a-cycle/prefer-a-small-
// duplication-over-a-new-cross-package-dependency rationale - though no
// import cycle is actually at stake here, copying the pattern is still
// preferred over a direct filemanager->transfer dependency that nothing
// else in the codebase has).
type runningBulkOp struct {
	// cancel stops this operation's context, the sole mechanism
	// CancelBulkOperation uses to interrupt an in-flight delete batch/copy-
	// move worker pool (see runDeleteObjects/runCopyOrMove's ctx.Err()
	// checks).
	cancel context.CancelFunc

	// done is closed once the operation's goroutine has fully finished
	// (including its terminal "completed"/"cancelled" bulk:progress emit and
	// removing itself from FileManagerService.running), so
	// CancelBulkOperation can block until the operation has reached a fully
	// consistent state before returning to its own caller - the same
	// contract transfer.runningTask.done documents.
	done chan struct{}
}

// ctxHolder wraps a context.Context so it can be stored in an atomic.Value:
// atomic.Value.Store panics if called with values of two different concrete
// types across calls, which a bare context.Context interface value cannot
// safely guarantee (different context implementations satisfy it) -
// wrapping it in a single, fixed struct type sidesteps that entirely, at the
// cost of one extra field access on load. Identical to (but a distinct type
// from, deliberately not imported) transfer.ctxHolder - see runningBulkOp's
// doc comment for why filemanager duplicates this small pattern rather than
// depending on package transfer.
type ctxHolder struct {
	ctx context.Context //nolint:containedctx // held only so emitBulkProgressEvent/emitObjectChangeEvent can call runtime.EventsEmit with the real Wails runtime context; see FileManagerService.wailsCtx's doc comment.
}

// SetContext installs the real Wails runtime context (from App.startup),
// enabling emitBulkProgressEvent/emitObjectChangeEvent to actually publish
// events from this point on. Safe to call at most once in production
// (App.startup runs once), but idempotent/safe to call repeatedly
// regardless (e.g. from a test) - identical contract to
// transfer.TransferService.SetContext.
func (f *FileManagerService) SetContext(ctx context.Context) {
	f.wailsCtx.Store(ctxHolder{ctx: ctx})
}

// emitBulkProgressEvent publishes event on wailsBulkProgressEvent via
// runtime.EventsEmit, or does nothing at all if SetContext has not been
// called yet (see wailsCtx's doc comment) - this makes every call site free
// to call it unconditionally, the same no-op-until-SetContext contract
// transfer.TransferService.emitProgressEvent documents.
func (f *FileManagerService) emitBulkProgressEvent(event domain.BulkOperationProgressEvent) {
	holder, ok := f.wailsCtx.Load().(ctxHolder)
	if !ok || holder.ctx == nil {
		return
	}

	runtime.EventsEmit(holder.ctx, wailsBulkProgressEvent, event)
}

// emitObjectChangeEvent publishes a domain.ObjectChangeEvent on
// wailsObjectChangeEvent via runtime.EventsEmit, or does nothing at all if
// SetContext has not been called yet - the exact same no-op-until-SetContext
// contract emitBulkProgressEvent documents, for the same reason: this is
// best-effort UI plumbing (letting an open FileManagerScreen refresh its
// listing via useTransferEvents.ts's existing "object:change" subscription -
// see this file's wailsObjectChangeEvent doc comment), never required for a
// bulk operation to complete correctly.
func (f *FileManagerService) emitObjectChangeEvent(bucket, prefix, changeType string) {
	holder, ok := f.wailsCtx.Load().(ctxHolder)
	if !ok || holder.ctx == nil {
		return
	}

	runtime.EventsEmit(holder.ctx, wailsObjectChangeEvent, domain.ObjectChangeEvent{
		Bucket: bucket,
		Prefix: prefix,
		Type:   changeType,
	})
}

// nextOperationID hands out the next unique
// domain.BulkOperationProgressEvent.OperationID (f.nextOpID starts at the
// atomic.Int64 zero value, so the first call returns 1).
func (f *FileManagerService) nextOperationID() int64 {
	return f.nextOpID.Add(1)
}

// registerBulkOp creates and registers a fresh runningBulkOp under f.mu,
// returning both it and the context its goroutine should run under -
// shared by DeleteObjects/CopyObjects/MoveObjects (mirrors
// transfer.TransferService.startTask's context.WithCancel(context.
// Background())+running-map-insert pattern, but returns rather than itself
// starting the goroutine, since each of the three callers' goroutine bodies
// differ).
func (f *FileManagerService) registerBulkOp(opID int64) (ctx context.Context, rt *runningBulkOp) {
	ctx, cancel := context.WithCancel(context.Background())

	rt = &runningBulkOp{cancel: cancel, done: make(chan struct{})}

	f.mu.Lock()
	f.running[opID] = rt
	f.mu.Unlock()

	return ctx, rt
}

// finishBulkOp removes opID from f.running and closes rt.done, signaling
// both "no longer cancelable" and "safe to observe a fully consistent final
// state" (see runningBulkOp.done's doc comment) - deferred by every bulk
// operation's goroutine (runDeleteObjects/runCopyOrMove), mirroring
// transfer.runTask's identical deferred cleanup.
func (f *FileManagerService) finishBulkOp(opID int64, rt *runningBulkOp) {
	f.mu.Lock()
	delete(f.running, opID)
	f.mu.Unlock()

	close(rt.done)
}

// CancelBulkOperation cancels the in-flight bulk operation identified by
// operationID (FR-BULK constraint 4 of the Этап 4 plan: "тот же паттерн,
// что TransferService.running"), blocking until its goroutine has fully
// finished (including its terminal "cancelled" bulk:progress emit) before
// returning - the same PauseTask/CancelTask-style
// signal-then-block-on-done contract transfer.TransferService's task
// lifecycle methods document. Returns a descriptive error, without blocking,
// if operationID does not identify a currently-running bulk operation
// (already finished, or never existed).
func (f *FileManagerService) CancelBulkOperation(operationID int64) error {
	f.mu.Lock()
	rt, ok := f.running[operationID]
	f.mu.Unlock()

	if !ok {
		return fmt.Errorf("bulk operation %d not found or already finished", operationID)
	}

	rt.cancel()
	<-rt.done

	return nil
}

// extractHostname returns the bare hostname component of endpointURL (e.g.
// for CircuitBreaker/s3client.WithRetry's host parameter), falling back to
// endpointURL itself, unparsed, if it cannot be parsed as a URL or has no
// recognizable host - never panicking. Identical to (but deliberately not
// imported from) transfer.extractHostname - see runningBulkOp's doc comment
// for why filemanager duplicates this small pattern rather than depending
// on package transfer.
func extractHostname(endpointURL string) string {
	u, err := url.Parse(endpointURL)
	if err != nil || u.Hostname() == "" {
		return endpointURL
	}

	return u.Hostname()
}

// objectPrefixOf returns the "folder" prefix key belongs to - everything up
// to and including the last "/", or "" if key has no "/" (an object at
// bucket root) - matching the bucket/prefix a FileManagerScreen browsing
// that location would be showing (see FileManagerService.ListObjects's
// Prefix parameter). Identical to (but deliberately not imported from)
// transfer.objectPrefixOf - see extractHostname's doc comment for why.
func objectPrefixOf(key string) string {
	idx := strings.LastIndex(key, "/")
	if idx == -1 {
		return ""
	}

	return key[:idx+1]
}
