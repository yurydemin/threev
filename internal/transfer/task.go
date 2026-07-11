package transfer

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"threev/internal/connection"
	"threev/internal/domain"
	"threev/internal/mimetype"
)

// transferUIInterval/transferPersistInterval are the two Tracker.Run tick
// intervals a running task uses in production: 500ms for Wails
// "transfer:progress" events (FR-TR-005), 3s for throttled SQLite progress
// persistence (see progress.go's Run doc comment for why these are two
// separate tickers).
const (
	transferUIInterval      = 500 * time.Millisecond
	transferPersistInterval = 3 * time.Second
)

// startTracker launches tracker.Run in its own goroutine, on a context
// derived from ctx, and returns a stop function the caller is expected to
// defer IMMEDIATELY (idiomatically: "defer s.startTracker(...)()") - not
// just some time before returning.
//
// Calling stop cancels the tracker's own (derived, child) context and then
// blocks until tracker.Run has actually returned. This matters for a
// reason specific to this call site: ctx itself (the task's own, shared
// context) is only ever canceled by an explicit Pause/Cancel - a task that
// completes or fails NORMALLY never cancels it at all, so without this
// derived child context and explicit stop(), tracker.Run would simply
// block forever past that point, both leaking its goroutine for the
// remaining lifetime of the process and - concretely, reproduced while
// writing this package's tests - risking a "database is closed" write
// error from a still-running onPersist callback firing after whatever
// owns TransferQueueRepository's *sql.DB has already closed it.
func (s *TransferService) startTracker(ctx context.Context, tracker *Tracker, taskID int64) (stop func()) {
	trackerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)
		tracker.Run(trackerCtx, transferUIInterval, transferPersistInterval, s.onUIProgress(taskID), s.onPersistProgress(taskID))
	}()

	return func() {
		cancel()
		<-done
	}
}

// runningTask is the in-memory state of one actively executing transfer
// task, held only in TransferService.running (keyed by TransferTask.ID) for
// exactly as long as its goroutine (runTask) is alive. It is never itself
// persisted - domain.TransferTask/SQLite's transfer_queue table is always
// the source of truth for a task's existence and status; runningTask only
// tracks what a live goroutine needs to be pausable/cancelable and to hand
// its progress off to Tracker.Run.
type runningTask struct {
	// cancel stops this task's context (ctx passed to runTask), the sole
	// mechanism Pause/Cancel use to interrupt an in-flight Upload/Download
	// call (see the Этап 3 plan's "Pause = cancel контекста" architecture
	// decision) - both boil down to the exact same signal
	// (context.Canceled) at every call site inside upload.go/download.go;
	// intent (below) is what lets runTask tell them apart afterward.
	cancel context.CancelFunc

	// tracker is created inside runTask itself (not startTask), once the
	// task's real TotalBytes is known - immediately for an upload (already
	// known from QueueUpload's os.Stat), only after an extra lightweight
	// HeadObject for a download (see runDownloadTask's doc comment for why).
	// It is written exactly once, by runTask's own goroutine, before it is
	// read by anything else - PauseTask/CancelTask never touch it, only
	// intent/cancel/done - so no additional synchronization is needed for
	// this field itself.
	tracker *Tracker

	// intent records WHY cancel is about to be (or was just) called -
	// "pause" or "cancel" - so runTask, upon observing its Upload/Download
	// call return context.Canceled (which by itself is ambiguous: both a
	// Pause and a Cancel produce exactly that same error), knows which
	// outcome to persist. Written by PauseTask/CancelTask BEFORE they call
	// cancel(); read by runTask AFTER Upload/Download returns
	// context.Canceled. An unset value (the atomic.Value zero state, never
	// explicitly stored) is treated as "pause" - the conservative default,
	// since silently archiving a task out of the queue on an ambiguous
	// signal is worse than merely leaving it resumable as Paused.
	intent atomic.Value // string: "" | "pause" | "cancel"

	// done is closed once runTask has fully finished - including its
	// terminal-status persistence/archive step - so PauseTask/CancelTask
	// (when called against a task that is actually running) can block until
	// the task has reached a fully consistent, persisted state before
	// returning to their own caller, rather than returning immediately
	// after merely requesting cancellation.
	done chan struct{}
}

// startTask registers task as active (inserting a fresh *runningTask into
// s.running) and starts its execution in a new goroutine (runTask) - it
// never blocks waiting for that goroutine. It is called only from
// dispatch(), which already holds s.mu for the entirety of the loop it
// runs in - see scheduler.go's doc comment for why that single critical
// section is what makes concurrent dispatch() calls race-free.
func (s *TransferService) startTask(task domain.TransferTask) {
	ctx, cancel := context.WithCancel(context.Background())

	rt := &runningTask{
		cancel: cancel,
		done:   make(chan struct{}),
	}

	s.running[task.ID] = rt

	go s.runTask(ctx, task, rt)
}

// runTask is the body of one active transfer task's goroutine: it resolves
// the task's profile/S3 clients, decodes its Bucket/Key from
// SourcePath/DestinationPath (see encodeBucketKey/splitBucketKey), delegates
// to runUploadTask/runDownloadTask for the type-specific Upload/Download
// call, and - via handleTaskResult, called from every exit path below -
// persists the final outcome (completed/failed/paused/cancelled), archiving
// to transfer_history for completed/cancelled only (see handleTaskResult's
// doc comment for the full Queue->History rule, including the plan
// correction this implements).
//
// Every database write here uses context.Background(), deliberately not
// ctx (which may already be canceled by the time this code runs, exactly
// when a Pause/Cancel is in progress) - a canceled ctx must never prevent
// runTask from durably recording the very reason it stopped.
func (s *TransferService) runTask(ctx context.Context, task domain.TransferTask, rt *runningTask) {
	defer func() {
		s.mu.Lock()
		delete(s.running, task.ID)
		s.mu.Unlock()

		close(rt.done)

		s.dispatch()
	}()

	if err := s.queueRepo.UpdateStatus(context.Background(), task.ID, "running", ""); err != nil {
		log.Printf("transfer: task %d: mark running: %v", task.ID, err)
	}

	bucket, key, pathErr := taskBucketKey(task)
	if pathErr != nil {
		s.handleTaskResult(task, rt, fmt.Errorf("decode task bucket/key: %w", pathErr), nil, "", "", "")
		return
	}

	// Guarded (Этап 4 суб-этап 4.4): runTask always runs asynchronously, on
	// its own goroutine spawned by startTask/dispatch, with no caller left
	// to hand a domain.ErrLocked back to directly - unlike every guarded
	// method elsewhere in this package/codebase, there is no "return
	// domain.ErrLocked" here. Instead, a locked application's keyBox.Get()
	// miss is folded into the exact same failure path a real resolve-profile
	// error would take: handleTaskResult marks the task "failed" (with
	// domain.ErrLocked's message persisted as its ErrorMessage), leaving it
	// sitting in transfer_queue for the user to RetryTask once they unlock,
	// exactly like any other transient resolve failure.
	//
	// Naming note: this package already has a local variable named key (the
	// S3 object key, from taskBucketKey above) in this exact function scope
	// - the encryption key is deliberately named encKey, not key, to avoid
	// shadowing/confusing the two (a prior Block in this codebase already
	// tripped over exactly this kind of name collision once).
	encKey, ok := s.keyBox.Get()
	if !ok {
		s.handleTaskResult(task, rt, fmt.Errorf("resolve profile: %w", domain.ErrLocked), nil, bucket, key, task.MultipartUploadID)
		return
	}

	profile, err := connection.ResolveProfile(context.Background(), s.profileRepo, encKey, task.ProfileID)
	if err != nil {
		s.handleTaskResult(task, rt, fmt.Errorf("resolve profile: %w", err), nil, bucket, key, task.MultipartUploadID)
		return
	}

	pooled, fresh, err := s.connMgr.Get(task.ProfileID)
	if err != nil {
		s.handleTaskResult(task, rt, fmt.Errorf("get S3 clients: %w", err), nil, bucket, key, task.MultipartUploadID)
		return
	}

	host := extractHostname(profile.EndpointURL)

	switch task.Type {
	case "upload":
		s.runUploadTask(ctx, task, rt, pooled, fresh, host, bucket, key)
	case "download":
		s.runDownloadTask(ctx, task, rt, pooled, fresh, host, bucket, key)
	default:
		s.handleTaskResult(task, rt, fmt.Errorf("unknown transfer task type %q", task.Type), pooled, bucket, key, task.MultipartUploadID)
	}
}

// runUploadTask runs the Upload side of runTask: task.TotalBytes is already
// known (QueueUpload populates it via os.Stat before the task is ever
// created), so the Tracker can be constructed immediately, unlike
// runDownloadTask.
func (s *TransferService) runUploadTask(ctx context.Context, task domain.TransferTask, rt *runningTask, pooled, fresh *s3.Client, host, bucket, key string) {
	tracker := NewTracker(task.TotalBytes)
	rt.tracker = tracker

	stopTracker := s.startTracker(ctx, tracker, task.ID)

	// Seeded from task.MultipartUploadID so a task whose CreateMultipartUpload
	// already succeeded on a previous, now-Failed attempt (ExistingUploadID
	// below resumes it, without a fresh OnMultipartUploadIDAssigned call)
	// still has its UploadId on hand here if this attempt is itself
	// interrupted by an explicit Cancel - see handleTaskResult's abort step.
	assignedUploadID := task.MultipartUploadID

	params := UploadParams{
		Pooled:  pooled,
		Fresh:   fresh,
		Breaker: s.breaker,
		Host:    host,
		Limiter: s.limiter.Load(),

		Bucket:      bucket,
		Key:         key,
		LocalPath:   task.SourcePath,
		ContentType: mimetype.ContentTypeForKey(key),
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

	_, err := Upload(ctx, params)

	// Stop the tracker (and wait for its last tick to flush) BEFORE
	// snapshotting/emitting the final result below - otherwise a stray
	// "running" tick from the 500ms UI ticker could still fire and be
	// observed by the frontend after the terminal "completed"/"failed"/
	// "paused"/"cancelled" event handleTaskResult is about to emit.
	stopTracker()

	s.handleTaskResult(task, rt, err, pooled, bucket, key, assignedUploadID)
}

// runDownloadTask runs the Download side of runTask.
//
// Accepted compromise (see this Block's task description): unlike an
// upload, a download's TotalBytes is not known until a HeadObject call -
// but the Tracker needs an accurate TotalBytes at construction time (it is
// immutable after NewTracker, see progress.go), and constructing it is a
// prerequisite for wiring OnBytesTransferred into DownloadParams.Hooks
// before Download itself runs. Rather than threading a pre-known
// TotalBytes through DownloadParams (which would complicate download.go's
// already-settled, self-contained HeadObject step for every OTHER caller
// too), runDownloadTask makes its own lightweight HeadObject call first -
// reusing headObject from download.go directly, since both live in this
// same package - to learn totalBytes, persists it, builds the Tracker, and
// only then calls the real Download (which performs its own, second
// HeadObject internally). This is a deliberate, small, redundant extra
// network round-trip per download task, accepted for the simplicity of
// leaving download.go's own public contract untouched.
func (s *TransferService) runDownloadTask(ctx context.Context, task domain.TransferTask, rt *runningTask, pooled, fresh *s3.Client, host, bucket, key string) {
	headParams := DownloadParams{
		Pooled:  pooled,
		Fresh:   fresh,
		Breaker: s.breaker,
		Host:    host,
		Bucket:  bucket,
		Key:     key,
	}

	totalBytes, _, headErr := headObject(ctx, headParams)
	if headErr != nil {
		s.handleTaskResult(task, rt, fmt.Errorf("head object: %w", headErr), pooled, bucket, key, "")
		return
	}

	if updErr := s.queueRepo.UpdateProgress(context.Background(), task.ID, task.TransferredBytes, totalBytes); updErr != nil {
		log.Printf("transfer: task %d: persist total bytes: %v", task.ID, updErr)
	}

	task.TotalBytes = totalBytes // so a later archiveTask records the real, HEAD-reported size

	tracker := NewTracker(totalBytes)
	rt.tracker = tracker

	stopTracker := s.startTracker(ctx, tracker, task.ID)

	params := DownloadParams{
		Pooled:  pooled,
		Fresh:   fresh,
		Breaker: s.breaker,
		Host:    host,
		Limiter: s.limiter.Load(),

		Bucket:    bucket,
		Key:       key,
		LocalPath: task.DestinationPath,

		PartSizeOverride: s.partSizeOverride.Load(),

		Concurrency: DefaultPartConcurrency,

		Hooks: DownloadHooks{OnBytesTransferred: tracker.AddBytes},
	}

	_, err := Download(ctx, params)

	// See runUploadTask's identical stopTracker() call for why this must
	// happen before handleTaskResult snapshots/emits the final result.
	stopTracker()

	s.handleTaskResult(task, rt, err, pooled, bucket, key, "")
}

// handleTaskResult persists the terminal outcome of one runTask attempt
// (err, as returned by Upload/Download, or a pre-flight error from runTask
// itself) and emits a final "transfer:progress" event reflecting it.
//
// Queue->History rule (the correction to the Этап 3 plan's original draft,
// see this Block's task description): ONLY two outcomes ever archive the
// task out of transfer_queue into transfer_history - a genuine success
// (err == nil, "completed") and an explicit, user-initiated Cancel
// (context.Canceled with rt.intent == "cancel", "cancelled"). Every other
// outcome - a Pause (context.Canceled with rt.intent != "cancel") or any
// other error at all ("failed") - leaves the task in transfer_queue,
// unarchived, so PauseTask/ResumeTask/RetryTask can keep finding it by id
// (and, for an upload, its MultipartUploadID) later.
//
// pooled/bucket/key/uploadID are used only for the Cancel branch's
// best-effort AbortMultipartUpload: pooled may be nil and/or uploadID may
// be empty (a download, an upload that never got past CreateMultipartUpload,
// or a pre-flight failure before a client was even resolved) - abort is
// simply skipped in that case, never treated as an error in itself.
func (s *TransferService) handleTaskResult(task domain.TransferTask, rt *runningTask, err error, pooled *s3.Client, bucket, key, uploadID string) {
	transferred, total, speed, eta := int64(0), task.TotalBytes, 0.0, int64(-1)
	if rt.tracker != nil {
		transferred, total, speed, eta = rt.tracker.Snapshot()
	}

	switch {
	case err == nil:
		if archErr := s.archiveTask(task, "completed", ""); archErr != nil {
			log.Printf("transfer: task %d: archive completed task: %v", task.ID, archErr)
		}

		s.emitProgressEvent(task.ID, transferred, total, speed, eta, "completed", "")

		// Only an upload actually changes what a bucket/prefix listing would
		// show server-side; a download never does, so no FileManagerScreen
		// needs to hear about it (see this function's own doc comment for
		// the pooled/bucket/key/uploadID parameters' broader purpose).
		if task.Type == "upload" {
			s.emitObjectChangeEvent(bucket, objectPrefixOf(key), "create")
		}

	case errors.Is(err, context.Canceled):
		var intent string
		if v, ok := rt.intent.Load().(string); ok {
			intent = v
		}

		if intent == "cancel" {
			if pooled != nil && uploadID != "" {
				if abortErr := AbortMultipartUpload(context.Background(), pooled, bucket, key, uploadID); abortErr != nil {
					log.Printf("transfer: task %d: abort multipart upload %s: %v", task.ID, uploadID, abortErr)
				}
			}

			if archErr := s.archiveTask(task, "cancelled", ""); archErr != nil {
				log.Printf("transfer: task %d: archive cancelled task: %v", task.ID, archErr)
			}

			s.emitProgressEvent(task.ID, transferred, total, speed, eta, "cancelled", "")

			return
		}

		// "pause", or an unset intent (defensive default - see
		// runningTask.intent's doc comment): the task stays in the queue.
		if statusErr := s.queueRepo.UpdateStatus(context.Background(), task.ID, "paused", ""); statusErr != nil {
			log.Printf("transfer: task %d: update status to paused: %v", task.ID, statusErr)
		}

		s.emitProgressEvent(task.ID, transferred, total, speed, eta, "paused", "")

	default:
		if statusErr := s.queueRepo.UpdateStatus(context.Background(), task.ID, "failed", err.Error()); statusErr != nil {
			log.Printf("transfer: task %d: update status to failed: %v", task.ID, statusErr)
		}

		s.emitProgressEvent(task.ID, transferred, total, speed, eta, "failed", err.Error())
	}
}

// taskBucketKey decodes task's Bucket/Key from whichever of
// SourcePath/DestinationPath carries the S3 side of the transfer, per
// encodeBucketKey/splitBucketKey's documented ORIGIN/TARGET convention: an
// upload's target (DestinationPath) is bucket/key, a download's origin
// (SourcePath) is bucket/key.
func taskBucketKey(task domain.TransferTask) (bucket, key string, err error) {
	switch task.Type {
	case "upload":
		return splitBucketKey(task.DestinationPath)
	case "download":
		return splitBucketKey(task.SourcePath)
	default:
		return "", "", fmt.Errorf("unknown transfer task type %q", task.Type)
	}
}

// objectPrefixOf returns the "folder" prefix key belongs to - everything up
// to and including the last "/", or "" if key has no "/" (an object at
// bucket root) - matching the bucket/prefix a FileManagerScreen browsing
// that location would be showing (see FileManagerService.ListObjects's
// Prefix parameter).
func objectPrefixOf(key string) string {
	idx := strings.LastIndex(key, "/")
	if idx == -1 {
		return ""
	}

	return key[:idx+1]
}

// extractHostname returns the bare hostname component of endpointURL (e.g.
// for CircuitBreaker/s3client.WithRetry's host parameter), falling back to
// endpointURL itself, unparsed, if it cannot be parsed as a URL or has no
// recognizable host - never panicking, per this Block's task description.
func extractHostname(endpointURL string) string {
	u, err := url.Parse(endpointURL)
	if err != nil || u.Hostname() == "" {
		return endpointURL
	}

	return u.Hostname()
}
