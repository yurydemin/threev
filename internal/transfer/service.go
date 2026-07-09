package transfer

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"threev/internal/domain"
	"threev/internal/s3client"
	"threev/internal/storage"
)

// wailsProgressEvent is the Wails event name TransferService publishes task
// progress/status changes on (docs/02-tech-spec.md section 9.5), carrying a
// domain.TransferProgressEvent payload.
const wailsProgressEvent = "transfer:progress"

// TransferService implements the Wails-bound API described in
// docs/02-tech-spec.md section 9.3: queueing/pausing/resuming/cancelling/
// retrying upload and download tasks, backed by the transfer_queue/
// transfer_history tables and the upload.go/download.go transfer engine
// (Этап 3 Blocks C/D), scheduled by dispatch (scheduler.go) and executed by
// runTask (task.go).
//
// Like FileManagerService, it deliberately does not depend on
// connection.ConnectionService: it takes the same *storage.ProfileRepository
// and encryption key already constructed once in app.go, resolving a
// decrypted domain.Profile itself via connection.ResolveProfile (task.go's
// runTask) whenever a task actually starts.
type TransferService struct {
	profileRepo   *storage.ProfileRepository
	encryptionKey [32]byte

	queueRepo   *storage.TransferQueueRepository
	historyRepo *storage.TransferHistoryRepository

	connMgr *s3client.ConnectionManager
	breaker *s3client.CircuitBreaker
	// limiter is nil by default (docs/02-tech-spec.md section 10.6: "По
	// умолчанию лимит выключен") - NewTransferService never sets it; a
	// future Settings screen (Этап 4) is expected to call a not-yet-written
	// setter to install one, without needing any other change here (every
	// UploadParams/DownloadParams.Limiter already tolerates nil, see
	// ratelimit.go).
	limiter *BandwidthLimiter

	maxConcurrentTasks int

	// mu guards running - the set of tasks currently executing. It is NOT
	// held for the duration of a task's own network I/O (see runTask/
	// dispatch's own doc comments for exactly which critical sections it
	// covers).
	mu      sync.Mutex
	running map[int64]*runningTask

	// wailsCtx holds ctxHolder (never a bare context.Context - see its own
	// doc comment for why), set once via SetContext from App.startup once
	// the real Wails runtime context is available. Until then (including
	// for every test in this package, which never calls SetContext),
	// emitProgressEvent is a no-op - publishing progress events is
	// best-effort UI plumbing, never required for a transfer to run
	// correctly.
	wailsCtx atomic.Value
}

// ctxHolder wraps a context.Context so it can be stored in an atomic.Value:
// atomic.Value.Store panics if called with values of two different
// concrete types across calls, which a bare context.Context interface
// value cannot safely guarantee (different context implementations satisfy
// it) - wrapping it in a single, fixed struct type sidesteps that
// entirely, at the cost of one extra field access on load.
type ctxHolder struct {
	ctx context.Context //nolint:containedctx // held only so emitProgressEvent can call runtime.EventsEmit with the real Wails runtime context; see TransferService.wailsCtx's doc comment.
}

// NewTransferService returns a TransferService backed by profileRepo/
// encryptionKey (for resolving profiles), queueRepo/historyRepo (for
// transfer_queue/transfer_history persistence), connMgr (for pooled/fresh
// S3 clients per profile), and breaker (the shared, per-process circuit
// breaker every task's retries coordinate through). maxConcurrentTasks is
// fixed to DefaultMaxConcurrentTasks (FR-QUEUE-004) and the bandwidth
// limiter starts out nil/unlimited (see the limiter field's doc comment) -
// neither has a constructor parameter yet.
func NewTransferService(
	profileRepo *storage.ProfileRepository,
	encryptionKey [32]byte,
	queueRepo *storage.TransferQueueRepository,
	historyRepo *storage.TransferHistoryRepository,
	connMgr *s3client.ConnectionManager,
	breaker *s3client.CircuitBreaker,
) *TransferService {
	return &TransferService{
		profileRepo:        profileRepo,
		encryptionKey:      encryptionKey,
		queueRepo:          queueRepo,
		historyRepo:        historyRepo,
		connMgr:            connMgr,
		breaker:            breaker,
		maxConcurrentTasks: DefaultMaxConcurrentTasks,
		running:            make(map[int64]*runningTask),
	}
}

// SetContext installs the real Wails runtime context (from App.startup),
// enabling emitProgressEvent to actually publish events from this point
// on. Safe to call at most once in production (App.startup runs once), but
// idempotent/safe to call repeatedly regardless (e.g. from a test).
func (s *TransferService) SetContext(ctx context.Context) {
	s.wailsCtx.Store(ctxHolder{ctx: ctx})
}

// emitProgressEvent publishes a domain.TransferProgressEvent on
// wailsProgressEvent via runtime.EventsEmit, or does nothing at all if
// SetContext has not been called yet (see wailsCtx's doc comment) - this
// makes every call site free to call it unconditionally, exactly as
// BandwidthLimiter's nil-safe Wrap*Reader methods let upload.go/download.go
// call them unconditionally.
func (s *TransferService) emitProgressEvent(taskID int64, transferred, total int64, speed float64, eta int64, status, errMsg string) {
	holder, ok := s.wailsCtx.Load().(ctxHolder)
	if !ok || holder.ctx == nil {
		return
	}

	runtime.EventsEmit(holder.ctx, wailsProgressEvent, domain.TransferProgressEvent{
		TaskID:           taskID,
		TransferredBytes: transferred,
		TotalBytes:       total,
		SpeedBytesPerSec: speed,
		ETASeconds:       eta,
		Status:           status,
		Error:            errMsg,
	})
}

// onUIProgress returns a Tracker.Run onUI callback (task.go's runUploadTask/
// runDownloadTask pass it to Tracker.Run for taskID) that publishes a
// "running"-status progress event on every UI tick.
func (s *TransferService) onUIProgress(taskID int64) func(transferred, total int64, speed float64, eta int64) {
	return func(transferred, total int64, speed float64, eta int64) {
		s.emitProgressEvent(taskID, transferred, total, speed, eta, "running", "")
	}
}

// onPersistProgress returns a Tracker.Run onPersist callback that writes
// throttled progress to transfer_queue via TransferQueueRepository.
// UpdateProgress. A persistence failure is logged, not propagated - see
// UpdateProgress's caller in progress.go's Run doc comment: persisted
// progress is best-effort, never fatal to the transfer itself.
func (s *TransferService) onPersistProgress(taskID int64) func(transferred, total int64) {
	return func(transferred, total int64) {
		if err := s.queueRepo.UpdateProgress(context.Background(), taskID, transferred, total); err != nil {
			log.Printf("transfer: task %d: persist progress: %v", taskID, err)
		}
	}
}

// archiveTask moves task out of transfer_queue into transfer_history with
// the given terminal status/errMsg, in the single MoveToHistory transaction
// (storage/transfer_queue_repository.go) - see handleTaskResult's doc
// comment (task.go) for exactly which statuses ever call this.
func (s *TransferService) archiveTask(task domain.TransferTask, status, errMsg string) error {
	entry := domain.TransferHistoryEntry{
		QueueID:         task.ID,
		ProfileID:       task.ProfileID,
		Type:            task.Type,
		SourcePath:      task.SourcePath,
		DestinationPath: task.DestinationPath,
		TotalBytes:      task.TotalBytes,
		Status:          status,
		CompletedAt:     time.Now(),
		ErrorMessage:    errMsg,
	}

	return s.queueRepo.MoveToHistory(context.Background(), task.ID, entry)
}

// encodeBucketKey encodes an S3 bucket/key pair into the single-string
// representation stored in a domain.TransferTask's SourcePath
// (downloads)/DestinationPath (uploads) column - the "ORIGIN"/"TARGET"
// convention documented on those two fields: bucket and key joined by a
// single "/", with no escaping. Since S3 object keys may themselves contain
// "/", decoding (splitBucketKey) always splits on only the FIRST "/" -
// everything after it, however many further "/" it contains, is the key.
func encodeBucketKey(bucket, key string) string {
	return bucket + "/" + key
}

// splitBucketKey decodes a string produced by encodeBucketKey back into its
// bucket and key, splitting on the first "/" only (see encodeBucketKey's
// doc comment for why). It returns an error if s has no "/" at all, or an
// empty bucket or key - a task's SourcePath/DestinationPath is never
// expected to look like that on the S3 side of a transfer, so this
// indicates a bug (or, defensively, a corrupted/hand-edited database row)
// rather than a normal runtime condition.
func splitBucketKey(s string) (bucket, key string, err error) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid bucket/key encoding %q", s)
	}

	return parts[0], parts[1], nil
}

// QueueUpload creates a new "pending" upload task for req.LocalPath ->
// req.Bucket/req.Key (docs/02-tech-spec.md section 9.3), stats LocalPath to
// populate the task's TotalBytes up front (required so runUploadTask can
// construct a Tracker immediately, see its doc comment), and calls
// dispatch() so an idle scheduler slot picks it up right away. Returns an
// error - without creating any task - if LocalPath cannot be stat'd (does
// not exist, unreadable, ...) or is a directory (QueueUpload only ever
// queues a single file; recursive folder upload is QueueUploadPaths, a
// later Stage 3 Block G addition).
func (s *TransferService) QueueUpload(req domain.UploadRequest) (int64, error) {
	info, err := os.Stat(req.LocalPath)
	if err != nil {
		return 0, fmt.Errorf("stat %s: %w", req.LocalPath, err)
	}

	if info.IsDir() {
		return 0, fmt.Errorf("%s is a directory, not a file", req.LocalPath)
	}

	task := domain.TransferTask{
		ProfileID:       req.ProfileID,
		Type:            "upload",
		SourcePath:      req.LocalPath,
		DestinationPath: encodeBucketKey(req.Bucket, req.Key),
		Status:          "pending",
		TotalBytes:      info.Size(),
		Priority:        req.Priority,
	}

	created, err := s.queueRepo.Create(context.Background(), task)
	if err != nil {
		return 0, err
	}

	s.dispatch()

	return created.ID, nil
}

// QueueDownload creates a new "pending" download task for req.Bucket/
// req.Key -> req.LocalPath (docs/02-tech-spec.md section 9.3) and calls
// dispatch(). Unlike QueueUpload, TotalBytes is left at 0 here: the real
// object size is not known until runDownloadTask's own HeadObject call once
// the task actually starts (see its doc comment for the accepted
// double-HEAD compromise this implies).
func (s *TransferService) QueueDownload(req domain.DownloadRequest) (int64, error) {
	task := domain.TransferTask{
		ProfileID:       req.ProfileID,
		Type:            "download",
		SourcePath:      encodeBucketKey(req.Bucket, req.Key),
		DestinationPath: req.LocalPath,
		Status:          "pending",
		Priority:        req.Priority,
	}

	created, err := s.queueRepo.Create(context.Background(), task)
	if err != nil {
		return 0, err
	}

	s.dispatch()

	return created.ID, nil
}

// PauseTask pauses task id, working uniformly whether or not it is
// currently running (FR-QUEUE-002):
//
//   - if it IS running: signals PauseTask's intent (rt.intent, read by
//     handleTaskResult), cancels its context, and blocks until its
//     goroutine has fully finished (including persisting "paused") before
//     returning - so callers always observe a consistent post-Pause state.
//   - if it is NOT running: it must already be "pending" (a task that is
//     merely queued, never started); its status is updated to "paused"
//     directly, with no goroutine involved. Any other status (already
//     paused, or a terminal one) is rejected with an error.
func (s *TransferService) PauseTask(id int64) error {
	s.mu.Lock()
	rt, running := s.running[id]
	s.mu.Unlock()

	if running {
		rt.intent.Store("pause")
		rt.cancel()
		<-rt.done

		return nil
	}

	task, err := s.queueRepo.GetByID(context.Background(), id)
	if err != nil {
		return err
	}

	if task.Status != "pending" {
		return fmt.Errorf("cannot pause transfer task %d in status %q", id, task.Status)
	}

	return s.queueRepo.UpdateStatus(context.Background(), id, "paused", "")
}

// ResumeTask moves a "paused" task id back to "pending" and calls
// dispatch() so it can be picked up immediately if a scheduler slot is
// free. Any other status is rejected with an error - a running task cannot
// be "resumed" (it is already running), and a terminal one has already
// left the queue.
func (s *TransferService) ResumeTask(id int64) error {
	task, err := s.queueRepo.GetByID(context.Background(), id)
	if err != nil {
		return err
	}

	if task.Status != "paused" {
		return fmt.Errorf("cannot resume transfer task %d in status %q, want %q", id, task.Status, "paused")
	}

	if err := s.queueRepo.UpdateStatus(context.Background(), id, "pending", ""); err != nil {
		return err
	}

	s.dispatch()

	return nil
}

// CancelTask cancels task id, working uniformly whether or not it is
// currently running (mirroring PauseTask's structure):
//
//   - if it IS running: signals CancelTask's intent, cancels its context,
//     and blocks until its goroutine has finished - which itself performs
//     the AbortMultipartUpload (if applicable) + archive-to-history steps,
//     via handleTaskResult (task.go).
//   - if it is NOT running: it must be "pending", "paused", or "failed" (a
//     "completed"/"cancelled" task has already left the queue entirely, and
//     a "running" task is - barring a crash-recovery edge case outside this
//     Block's scope - always found in s.running). CancelTask itself
//     performs the AbortMultipartUpload (best-effort, logged not
//     propagated) + archive steps synchronously, since there is no
//     goroutine to do it on its behalf.
//
// Returns domain.ErrTransferTaskNotFound (wrapped) if id does not identify
// any transfer_queue row.
func (s *TransferService) CancelTask(id int64) error {
	s.mu.Lock()
	rt, running := s.running[id]
	s.mu.Unlock()

	if running {
		rt.intent.Store("cancel")
		rt.cancel()
		<-rt.done

		return nil
	}

	task, err := s.queueRepo.GetByID(context.Background(), id)
	if err != nil {
		return err
	}

	switch task.Status {
	case "pending", "paused", "failed":
		// allowed - fall through to the synchronous cancel below.
	default:
		return fmt.Errorf("cannot cancel transfer task %d in status %q", id, task.Status)
	}

	s.abortOrphanedMultipartUpload(id, task)

	return s.archiveTask(task, "cancelled", "")
}

// abortOrphanedMultipartUpload best-effort aborts task's server-side
// multipart upload (if it is an upload task that got as far as
// CreateMultipartUpload) as part of a synchronous CancelTask call against a
// not-currently-running task. Every failure here is logged, never returned:
// this is cleanup of a resource that will otherwise simply sit unused
// server-side (subject to whatever lifecycle policy, if any, the bucket
// has) - it must never block the task's own cancellation from completing.
func (s *TransferService) abortOrphanedMultipartUpload(id int64, task domain.TransferTask) {
	if task.Type != "upload" || task.MultipartUploadID == "" {
		return
	}

	pooled, _, err := s.connMgr.Get(task.ProfileID)
	if err != nil {
		log.Printf("transfer: cancel task %d: get S3 client for multipart abort: %v", id, err)
		return
	}

	bucket, key, err := splitBucketKey(task.DestinationPath)
	if err != nil {
		log.Printf("transfer: cancel task %d: decode bucket/key for multipart abort: %v", id, err)
		return
	}

	if err := AbortMultipartUpload(context.Background(), pooled, bucket, key, task.MultipartUploadID); err != nil {
		log.Printf("transfer: cancel task %d: abort multipart upload %s: %v", id, task.MultipartUploadID, err)
	}
}

// RetryTask resets a "failed" task id back to "pending" (clearing its
// ErrorMessage) and calls dispatch(). It deliberately returns the SAME id
// rather than creating a new transfer_queue row - a documented departure
// from a literal reading of docs/02-tech-spec.md section 9.3's signature,
// required so a resumable upload's MultipartUploadID (left untouched here)
// is not orphaned: runUploadTask's ExistingUploadID resume path only ever
// looks it up by this exact id. Any status other than "failed" is rejected
// with an error.
func (s *TransferService) RetryTask(id int64) (int64, error) {
	task, err := s.queueRepo.GetByID(context.Background(), id)
	if err != nil {
		return 0, err
	}

	if task.Status != "failed" {
		return 0, fmt.Errorf("cannot retry transfer task %d in status %q, want %q", id, task.Status, "failed")
	}

	if err := s.queueRepo.UpdateStatus(context.Background(), id, "pending", ""); err != nil {
		return 0, err
	}

	s.dispatch()

	return id, nil
}

// ReorderTask updates task id's priority (FR-QUEUE-003; lower values run
// first). It does not call dispatch(): reordering never creates a new free
// scheduler slot, only changes which pending task the next organic
// dispatch() call (from a QueueUpload/QueueDownload/ResumeTask/RetryTask
// call, or a task finishing) will prefer.
func (s *TransferService) ReorderTask(id int64, newPriority int) error {
	return s.queueRepo.UpdatePriority(context.Background(), id, newPriority)
}

// GetQueue returns every transfer_queue row (docs/02-tech-spec.md section
// 9.3), ordered by priority/created_at (FR-QUEUE-003).
func (s *TransferService) GetQueue() ([]domain.TransferTask, error) {
	return s.queueRepo.GetAll(context.Background())
}

// GetHistory returns up to limit transfer_history rows, most recently
// completed first (FR-QUEUE-006).
func (s *TransferService) GetHistory(limit int) ([]domain.TransferHistoryEntry, error) {
	return s.historyRepo.GetAll(context.Background(), limit)
}

// ClearHistory deletes every transfer_history row (FR-SET-002).
func (s *TransferService) ClearHistory() error {
	return s.historyRepo.Clear(context.Background())
}
