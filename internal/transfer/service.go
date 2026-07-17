package transfer

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"threev/internal/connection"
	"threev/internal/crypto"
	"threev/internal/domain"
	"threev/internal/s3client"
	"threev/internal/storage"
)

// wailsProgressEvent is the Wails event name TransferService publishes task
// progress/status changes on (docs/02-tech-spec.md section 9.5), carrying a
// domain.TransferProgressEvent payload.
const wailsProgressEvent = "transfer:progress"

// wailsObjectChangeEvent is the Wails event name TransferService publishes
// on (docs/02-tech-spec.md section 9.5) whenever a transfer creates or
// removes an object under a bucket/prefix, carrying a
// domain.ObjectChangeEvent payload - see emitObjectChangeEvent.
const wailsObjectChangeEvent = "object:change"

// TransferService implements the Wails-bound API described in
// docs/02-tech-spec.md section 9.3: queueing/pausing/resuming/cancelling/
// retrying upload and download tasks, backed by the transfer_queue/
// transfer_history tables and the upload.go/download.go transfer engine
// (Этап 3 Blocks C/D), scheduled by dispatch (scheduler.go) and executed by
// runTask (task.go).
//
// Like FileManagerService, it deliberately does not depend on
// connection.ConnectionService: it takes the same *storage.ProfileRepository
// and (Этап 4 суб-этап 4.4) *crypto.KeyBox already constructed once in
// app.go, resolving a decrypted domain.Profile itself via
// connection.ResolveProfile (task.go's runTask) whenever a task actually
// starts.
type TransferService struct {
	profileRepo *storage.ProfileRepository
	keyBox      *crypto.KeyBox

	queueRepo   *storage.TransferQueueRepository
	historyRepo *storage.TransferHistoryRepository

	connMgr *s3client.ConnectionManager
	breaker *s3client.CircuitBreaker
	// retryPolicies is the shared, per-process source of retry/timeout
	// configuration every s3client.WithRetry/s3client.AdaptiveTimeout call
	// site in this package (and free functions it hands UploadParams/
	// DownloadParams to, in upload.go/download.go/multipart_upload.go/
	// range_download.go) reads from, instead of the s3client.PartRetryPolicy/
	// MetadataRetryPolicy package vars directly - see
	// s3client.RetryPolicyStore's own doc comment. Exactly the same shared
	// instance app.go's newApp() passes to filemanager.NewFileManagerService
	// (not a new one), mirroring breaker above.
	retryPolicies *s3client.RetryPolicyStore
	// limiter is nil (the atomic.Pointer[BandwidthLimiter] zero value) by
	// default (docs/02-tech-spec.md section 10.6: "По умолчанию лимит
	// выключен") - NewTransferService never sets it. SetBandwidthLimits
	// (Этап 4 суб-этап 4.3, internal/appsettings.SettingsService.
	// ApplySettings) installs one at runtime. It is an atomic.Pointer,
	// exactly like wailsCtx above, rather than a plain *BandwidthLimiter
	// field, because it can now be replaced (Store) concurrently with
	// task.go's runUploadTask/runDownloadTask reading it (Load) for an
	// already in-flight task - a plain pointer read/write pair would be a
	// data race under -race. Load() on a never-Store'd atomic.Pointer
	// returns nil, which is already nil-safe for every
	// WrapUploadReader/WrapDownloadReader call site (see ratelimit.go), so
	// no additional initialization is needed in NewTransferService.
	limiter atomic.Pointer[BandwidthLimiter]

	// partSizeOverride is the fixed part/segment size (in bytes) SetPartSizeOverrideMB
	// installs, or 0 for PartSize's adaptive table (the default - see
	// multipart_upload.go/range_download.go's override-or-adaptive checks).
	// An atomic.Int64 for the same reason limiter is an atomic.Pointer: it
	// can be changed (SettingsService.ApplySettings) concurrently with an
	// in-flight task reading it.
	partSizeOverride atomic.Int64

	// maxConcurrentTasks is guarded by mu (below) - see SetMaxConcurrentTasks
	// and dispatch's own doc comment for why a plain int, not an atomic,
	// suffices: every read/write already happens inside a mu critical
	// section.
	maxConcurrentTasks int

	// mu guards running and maxConcurrentTasks. It is NOT held for the
	// duration of a task's own network I/O (see runTask/dispatch's own doc
	// comments for exactly which critical sections it covers).
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
// keyBox (for resolving profiles - keyBox is read fresh on every call that
// needs it rather than a fixed [32]byte being taken at construction time,
// see crypto.KeyBox's own doc comment and NewConnectionService's identical
// rationale, Этап 4 суб-этап 4.4), queueRepo/historyRepo (for
// transfer_queue/transfer_history persistence), connMgr (for pooled/fresh
// S3 clients per profile), breaker (the shared, per-process circuit
// breaker every task's retries coordinate through), and retryPolicies (the
// shared, per-process retry/timeout configuration store - see its own doc
// comment). maxConcurrentTasks starts at DefaultMaxConcurrentTasks
// (FR-QUEUE-004), the bandwidth limiter starts out nil/unlimited, and
// partSizeOverride starts out 0/adaptive (see the limiter/partSizeOverride
// fields' own doc comments) - none of the three has a constructor
// parameter; each has its own setter (SetMaxConcurrentTasks/
// SetBandwidthLimits/SetPartSizeOverrideMB) instead, called by
// internal/appsettings.SettingsService.ApplySettings.
func NewTransferService(
	profileRepo *storage.ProfileRepository,
	keyBox *crypto.KeyBox,
	queueRepo *storage.TransferQueueRepository,
	historyRepo *storage.TransferHistoryRepository,
	connMgr *s3client.ConnectionManager,
	breaker *s3client.CircuitBreaker,
	retryPolicies *s3client.RetryPolicyStore,
) *TransferService {
	return &TransferService{
		profileRepo:        profileRepo,
		keyBox:             keyBox,
		queueRepo:          queueRepo,
		historyRepo:        historyRepo,
		connMgr:            connMgr,
		breaker:            breaker,
		retryPolicies:      retryPolicies,
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

// SetBandwidthLimits installs a new BandwidthLimiter configured for
// uploadBytesPerSec/downloadBytesPerSec (<= 0 means unlimited for that
// direction - see NewBandwidthLimiter's doc comment), replacing whatever
// limiter was previously in effect. Safe to call at any time, including
// concurrently with in-flight tasks: task.go's runUploadTask/
// runDownloadTask each Load() a fresh *BandwidthLimiter from s.limiter once
// per attempt, so an already-running task keeps using whatever limiter was
// in effect when it started until its next retry attempt, rather than being
// interrupted - this is the same "no forced disruption of running work"
// philosophy SetMaxConcurrentTasks documents for reducing the concurrency
// limit.
//
// Called by internal/appsettings.SettingsService.ApplySettings (Этап 4
// суб-этап 4.3) - there is no other production caller yet.
func (s *TransferService) SetBandwidthLimits(uploadBytesPerSec, downloadBytesPerSec int64) {
	s.limiter.Store(NewBandwidthLimiter(uploadBytesPerSec, downloadBytesPerSec))
}

// SetMaxConcurrentTasks changes the scheduler's concurrency limit
// (FR-QUEUE-004) to n, clamping n up to 1 if it is less (a limit of 0 or
// negative would stop dispatch from ever starting anything at all, which is
// never an intended outcome of a Settings change). It then calls dispatch()
// so that RAISING the limit immediately tries to fill the newly available
// slot(s) with pending tasks, without waiting for some other event (a task
// finishing, a new QueueUpload/QueueDownload call, ...) to trigger it.
//
// LOWERING the limit never forcibly stops or pauses any task already
// running past the new, smaller limit - dispatch() only ever refuses to
// START new tasks once len(s.running) reaches maxConcurrentTasks; it does
// not evict existing entries from s.running. This is a deliberate,
// unobtrusive choice: a user narrowing the concurrency slider mid-transfer
// should not have an already-in-flight upload/download abruptly interrupted
// as a side effect.
//
// Called by internal/appsettings.SettingsService.ApplySettings (Этап 4
// суб-этап 4.3) - there is no other production caller yet.
func (s *TransferService) SetMaxConcurrentTasks(n int) {
	if n < 1 {
		n = 1
	}

	s.mu.Lock()
	s.maxConcurrentTasks = n
	s.mu.Unlock()

	s.dispatch()
}

// SetPartSizeOverrideMB installs (or clears) a fixed part/segment size that
// bypasses PartSize's adaptive table (multipart_upload.go/
// range_download.go). mb <= 0 clears any override, reverting to the
// adaptive default. Any other value is clamped to [5,128] (the same bounds
// internal/appsettings.SettingsService.SaveSettings already enforces before
// ever calling this) - TransferService deliberately does not trust its
// caller to have applied that clamp itself, since a part size outside
// S3's [5MB, ...] protocol floor (or an unreasonably large one) could
// otherwise reach multipart_upload.go/range_download.go directly were this
// method ever called from anywhere else in the future.
//
// Called by internal/appsettings.SettingsService.ApplySettings (Этап 4
// суб-этап 4.3) - there is no other production caller yet.
func (s *TransferService) SetPartSizeOverrideMB(mb int) {
	if mb <= 0 {
		s.partSizeOverride.Store(0)
		return
	}

	if mb < partSizeOverrideMinMB {
		mb = partSizeOverrideMinMB
	}
	if mb > partSizeOverrideMaxMB {
		mb = partSizeOverrideMaxMB
	}

	s.partSizeOverride.Store(int64(mb) * 1024 * 1024)
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

// emitObjectChangeEvent publishes a domain.ObjectChangeEvent on
// wailsObjectChangeEvent via runtime.EventsEmit, or does nothing at all if
// SetContext has not been called yet - the exact same no-op-until-SetContext
// contract as emitProgressEvent (see its doc comment), for the same reason:
// this is best-effort UI plumbing (letting an open FileManagerScreen refresh
// its listing), never required for a transfer to run correctly.
func (s *TransferService) emitObjectChangeEvent(bucket, prefix, changeType string) {
	holder, ok := s.wailsCtx.Load().(ctxHolder)
	if !ok || holder.ctx == nil {
		return
	}

	runtime.EventsEmit(holder.ctx, wailsObjectChangeEvent, domain.ObjectChangeEvent{
		Bucket: bucket,
		Prefix: prefix,
		Type:   changeType,
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

// downloadPrefixPageSize is the ListObjectsV2 page size QueueDownloadPrefix
// requests - the same value as filemanager's own maxKeysPerPage
// (FR-FM-005), duplicated here rather than exported/shared since the two
// packages' list calls otherwise differ in every other respect (this one is
// delimiter-less and fully paginated internally, filemanager's is
// delimiter-based and paginated one page at a time by the caller).
const downloadPrefixPageSize = 1000

// normalizeS3Prefix returns prefix with any leading "/" stripped and,
// unless prefix is empty, exactly one trailing "/" - the form every S3 key
// QueueUploadPaths builds is prefixed with.
func normalizeS3Prefix(prefix string) string {
	prefix = strings.TrimPrefix(prefix, "/")
	if prefix == "" {
		return ""
	}

	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	return prefix
}

// QueueUploadPaths is the single entry point both the system file/folder
// picker (dialogs.go's PickUploadFiles/PickUploadDirectory) and a future
// drag-and-drop handler (Этап 3 Block J, not yet implemented) call to queue
// zero or more uploads at once: localPaths may freely mix plain files and
// directories (absolute local paths), in any combination.
//
// S3 key construction for destinationPrefix (normalized via
// normalizeS3Prefix) + each resulting file:
//
//   - a localPaths entry that is itself a plain file: destinationPrefix +
//     filepath.Base(localPath).
//   - a localPaths entry that is a directory: recursively walked
//     (filepath.WalkDir); every regular file found (d.Type().IsRegular() -
//     symlinks and anything else are silently skipped, and never followed)
//     gets destinationPrefix + filepath.ToSlash(relPath), where relPath is
//     the file's path relative to filepath.Dir(localPath) - i.e. INCLUDING
//     the walked directory's own name as the first path segment, so that
//     uploading several sibling files/directories in one call can never
//     collide with each other, while still preserving a recognizable
//     directory structure under destinationPrefix.
//
// Best-effort semantics: any single localPaths entry that cannot be
// stat'd/walked, and any single resulting file QueueUpload itself rejects,
// is logged and skipped - never aborting the rest of localPaths. The
// returned []int64 holds the id of every task that was actually queued,
// preserving no particular guaranteed order beyond localPaths' own
// traversal order; a non-nil error is returned only when that slice would
// otherwise be empty (nothing at all could be queued - e.g. localPaths
// itself is empty, or every entry was unreadable), since an empty result
// with no error would otherwise be indistinguishable from "nothing was
// asked for" to a caller.
func (s *TransferService) QueueUploadPaths(profileID int64, bucket, destinationPrefix string, localPaths []string) ([]int64, error) {
	prefix := normalizeS3Prefix(destinationPrefix)

	var ids []int64

	for _, localPath := range localPaths {
		info, err := os.Stat(localPath)
		if err != nil {
			log.Printf("transfer: QueueUploadPaths: stat %s: %v", localPath, err)
			continue
		}

		if !info.IsDir() {
			key := prefix + filepath.Base(localPath)
			if id, ok := s.queueUploadPathBestEffort(profileID, bucket, key, localPath); ok {
				ids = append(ids, id)
			}

			continue
		}

		baseDir := filepath.Dir(localPath)

		walkErr := filepath.WalkDir(localPath, func(path string, d fs.DirEntry, walkEntryErr error) error {
			if walkEntryErr != nil {
				log.Printf("transfer: QueueUploadPaths: walk %s: %v", path, walkEntryErr)
				return nil // best-effort: skip this entry, keep walking the rest of the tree
			}

			if !d.Type().IsRegular() {
				return nil // directories are recursed into automatically; symlinks/other types are never followed
			}

			relPath, relErr := filepath.Rel(baseDir, path)
			if relErr != nil {
				log.Printf("transfer: QueueUploadPaths: relative path for %s under %s: %v", path, baseDir, relErr)
				return nil
			}

			key := prefix + filepath.ToSlash(relPath)
			if id, ok := s.queueUploadPathBestEffort(profileID, bucket, key, path); ok {
				ids = append(ids, id)
			}

			return nil
		})
		if walkErr != nil {
			log.Printf("transfer: QueueUploadPaths: walk %s: %v", localPath, walkErr)
		}
	}

	if len(ids) == 0 {
		return nil, fmt.Errorf("no files were queued for upload out of %d given path(s)", len(localPaths))
	}

	return ids, nil
}

// queueUploadPathBestEffort calls QueueUpload for localPath -> bucket/key,
// logging (never propagating) any error - the shared best-effort tail of
// both QueueUploadPaths branches (a bare file, and each regular file found
// while walking a directory).
func (s *TransferService) queueUploadPathBestEffort(profileID int64, bucket, key, localPath string) (id int64, ok bool) {
	id, err := s.QueueUpload(domain.UploadRequest{
		ProfileID: profileID,
		Bucket:    bucket,
		Key:       key,
		LocalPath: localPath,
	})
	if err != nil {
		log.Printf("transfer: QueueUploadPaths: queue upload %s -> %s/%s: %v", localPath, bucket, key, err)
		return 0, false
	}

	return id, true
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

// QueueDownloadPrefix recursively lists every object under bucket/prefix -
// a full, DELIMITER-LESS ListObjectsV2 pagination, unlike
// filemanager.ListObjects's delimiter-based single-level folder view: the
// goal here is "download everything that exists under this prefix,
// mirroring its structure onto disk", which needs every key at every
// depth, not just the immediate children - and calls QueueDownload once per
// resulting object.
//
// Local path for each object: filepath.Join(localDestDir,
// filepath.FromSlash(relPath)), where relPath is the object's key with
// prefix stripped (strings.TrimPrefix(key, prefix)). Two kinds of key are
// never queued:
//
//   - a zero-byte "folder placeholder" object (a key ending in "/" with
//     size 0 - the same object filemanager/list.go's entriesFromPage
//     special-cases for the same underlying reason: many S3-compatible
//     servers/GUI clients create one when a "folder" is made explicitly).
//   - a key whose relPath, once filepath.Clean'd and split into segments,
//     contains ".." anywhere - a basic defense against directory traversal
//     when mirroring a remote, otherwise-untrusted S3 key onto the local
//     filesystem (see the Этап 3 plan's "known risks" section).
//
// Best-effort semantics identical to QueueUploadPaths: a single object that
// fails to queue (or an entire ListObjectsV2 page that fails to fetch,
// which simply ends pagination early rather than aborting everything
// already queued) is logged and skipped, never aborting the rest; a
// non-nil error is returned only if nothing at all was queued.
//
// Guarded (Этап 4 суб-этап 4.4): unlike QueueUpload/QueueUploadPaths/
// QueueDownload (which only ever create transfer_queue rows, resolving the
// profile later, asynchronously, in runTask - see runTask's own guard),
// QueueDownloadPrefix resolves the profile SYNCHRONOUSLY right here, since
// it needs a live S3 client immediately to list bucket/prefix - so it needs
// its own guard rather than relying on runTask's.
func (s *TransferService) QueueDownloadPrefix(profileID int64, bucket, prefix, localDestDir string) ([]int64, error) {
	key, ok := s.keyBox.Get()
	if !ok {
		return nil, domain.ErrLocked
	}

	profile, err := connection.ResolveProfile(context.Background(), s.profileRepo, key, profileID)
	if err != nil {
		return nil, fmt.Errorf("resolve profile %d: %w", profileID, err)
	}

	pooled, fresh, err := s.connMgr.Get(profileID)
	if err != nil {
		return nil, fmt.Errorf("get S3 clients for profile %d: %w", profileID, err)
	}

	host := extractHostname(profile.EndpointURL)

	var ids []int64

	continuationToken := ""

	for {
		page, listErr := s.listObjectsPageForDownloadPrefix(pooled, fresh, host, bucket, prefix, continuationToken)
		if listErr != nil {
			log.Printf("transfer: QueueDownloadPrefix: list %s/%s: %v", bucket, prefix, listErr)
			break
		}

		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)

			if strings.HasSuffix(key, "/") && aws.ToInt64(obj.Size) == 0 {
				continue
			}

			localPath, ok := resolveDownloadPrefixLocalPath(localDestDir, prefix, key)
			if !ok {
				continue
			}

			id, err := s.QueueDownload(domain.DownloadRequest{
				ProfileID: profileID,
				Bucket:    bucket,
				Key:       key,
				LocalPath: localPath,
			})
			if err != nil {
				log.Printf("transfer: QueueDownloadPrefix: queue download %s/%s -> %s: %v", bucket, key, localPath, err)
				continue
			}

			ids = append(ids, id)
		}

		if !aws.ToBool(page.IsTruncated) {
			break
		}

		continuationToken = aws.ToString(page.NextContinuationToken)
		if continuationToken == "" {
			break
		}
	}

	if len(ids) == 0 {
		return nil, fmt.Errorf("no objects were queued for download under %s/%s", bucket, prefix)
	}

	return ids, nil
}

// listObjectsPageForDownloadPrefix runs one delimiter-less ListObjectsV2
// call (bucket/prefix, continuing from continuationToken if non-empty)
// under s3client.WithRetry/s.breaker (s3client.MetadataRetryPolicy - the
// same policy every other metadata-only call in this package uses), the
// same pooled-then-fresh-client-on-retry pattern task.go's headObject call
// follows, so that no direct S3 API call in this package ever bypasses the
// shared retry/circuit-breaker machinery.
func (s *TransferService) listObjectsPageForDownloadPrefix(pooled, fresh *s3.Client, host, bucket, prefix, continuationToken string) (*s3.ListObjectsV2Output, error) {
	var page *s3.ListObjectsV2Output

	err := s3client.WithRetry(context.Background(), s.breaker, s.retryPolicies.Metadata(), host, func(attemptCtx context.Context, isRetry bool) error {
		client := pooled
		if isRetry {
			client = fresh
		}

		input := &s3.ListObjectsV2Input{
			Bucket:  aws.String(bucket),
			Prefix:  aws.String(prefix),
			MaxKeys: aws.Int32(downloadPrefixPageSize),
		}
		if continuationToken != "" {
			input.ContinuationToken = aws.String(continuationToken)
		}

		out, listErr := client.ListObjectsV2(attemptCtx, input)
		if listErr != nil {
			return listErr
		}

		page = out

		return nil
	})
	if err != nil {
		return nil, err
	}

	return page, nil
}

// resolveDownloadPrefixLocalPath computes the local filesystem path for key
// (found under prefix by QueueDownloadPrefix) inside localDestDir, or
// returns ("", false) if key's path relative to prefix escapes
// localDestDir via ".." once cleaned - see QueueDownloadPrefix's own doc
// comment for why this check exists.
func resolveDownloadPrefixLocalPath(localDestDir, prefix, key string) (string, bool) {
	relPath := strings.TrimPrefix(key, prefix)

	cleaned := filepath.ToSlash(filepath.Clean(relPath))
	for _, segment := range strings.Split(cleaned, "/") {
		if segment == ".." {
			log.Printf("transfer: QueueDownloadPrefix: rejecting key %q: relative path %q escapes the destination directory", key, relPath)
			return "", false
		}
	}

	return filepath.Join(localDestDir, filepath.FromSlash(relPath)), true
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

// CancelTasksForProfile cancels every transfer_queue task belonging to
// profileID (in whatever status - pending/paused/failed/running - CancelTask
// itself already knows how to handle), returning how many were successfully
// cancelled.
//
// Why this exists: the transfer_queue table's profile_id column is a
// FOREIGN KEY REFERENCES profiles(id) with no ON DELETE CASCADE
// (internal/storage/migrations/0001_init.sql), and internal/storage/
// sqlite.go turns on "PRAGMA foreign_keys = ON". Deleting a profile that
// still has any transfer_queue row therefore fails at the SQLite layer with
// a raw foreign-key-constraint error, not a clean, actionable one -
// connection.ConnectionService.DeleteProfile has no way to avoid this
// itself, since it only ever calls its own repo.Delete.
//
// Why it lives here rather than in ConnectionService: this codebase
// deliberately never lets one Wails-bound service hold a reference to
// another (see this type's own doc comment, and FileManagerService's
// identical stance) - cross-cutting concerns are wired at the FRONTEND
// layer instead. The intended caller is exactly that: the frontend's
// profile-deletion flow (mirroring ConnectionsScreen.tsx's existing
// handleDelete, which already separately reaches into
// useFileManagerStore) is expected to call this method first, inspect the
// returned count for its confirmation-dialog wording, and only then call
// DeleteProfile - never the other way around, and never through a Go-level
// dependency between the two services.
//
// A single task's CancelTask call failing (e.g. a race where it left its
// terminal state between GetQueue and this call) is logged and skipped,
// never aborting the rest - a caller here cares about "cancel as many as
// possible so the profile can be deleted", not an all-or-nothing
// transaction. Returns (0, nil), not an error, when profileID has no queued
// tasks at all - the common case for most profile deletions.
func (s *TransferService) CancelTasksForProfile(profileID int64) (int, error) {
	tasks, err := s.GetQueue()
	if err != nil {
		return 0, fmt.Errorf("list transfer queue: %w", err)
	}

	cancelled := 0

	for _, task := range tasks {
		if task.ProfileID != profileID {
			continue
		}

		if err := s.CancelTask(task.ID); err != nil {
			log.Printf("transfer: cancel tasks for profile %d: cancel task %d: %v", profileID, task.ID, err)
			continue
		}

		cancelled++
	}

	return cancelled, nil
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

// RecoverOrphanedTasks resets every "running" transfer_queue row back to
// "paused" - called once, synchronously, right after NewTransferService,
// before any dispatch() call (see app.go's newApp), to reconcile state left
// behind by a process that was killed mid-transfer (crash, force-quit,
// power loss): no goroutine survives a process restart, so a "running" row
// found at startup can only mean exactly that - its task was never actually
// finished, yet dispatch() (which only ever looks at "pending" rows, see
// scheduler.go) would otherwise leave it sitting there, indistinguishable
// from a genuinely in-progress transfer, forever.
//
// It deliberately does not call dispatch() itself: per the Этап 3 plan's
// constraint 4 ("автовозобновление не автостартует при следующем запуске"),
// a reconciled task must be left Paused, requiring an explicit ResumeTask
// from the user - never silently resumed on the user's behalf.
//
// It is deliberately NOT called from NewTransferService itself: the
// constructor has no side effect today beyond assembling the struct, and
// the two failure modes warrant different handling by their respective
// callers - opening the database is fatal to the whole application
// (app.go's newApp), while a failure reconciling the queue here is not (see
// its call site's own doc comment for why it only logs and continues).
//
// It returns the ids of every task this call itself just moved from
// "running" to "paused" - never every "paused" row already sitting in
// transfer_queue (which may include tasks the user paused deliberately, on
// a previous run, and which must never be silently resumed on their
// behalf). This is what lets AutoResumeIfEnabled (Этап 4 суб-этап 4.3)
// resume ONLY the crash-orphaned tasks this exact call reconciled, when the
// user has opted into FR-SET-001's "auto-resume queue" setting, without
// ever touching a task the user paused themselves.
func (s *TransferService) RecoverOrphanedTasks() ([]int64, error) {
	tasks, err := s.queueRepo.GetAll(context.Background())
	if err != nil {
		return nil, fmt.Errorf("list transfer queue: %w", err)
	}

	var recovered []int64

	for _, task := range tasks {
		if task.Status != "running" {
			continue
		}

		if err := s.queueRepo.UpdateStatus(context.Background(), task.ID, "paused", ""); err != nil {
			return recovered, fmt.Errorf("reset orphaned task %d to paused: %w", task.ID, err)
		}

		recovered = append(recovered, task.ID)
	}

	return recovered, nil
}

// AutoResumeIfEnabled resumes every task in recoveredIDs (the result of a
// just-completed RecoverOrphanedTasks call) via ResumeTask, but only if
// enabled is true (domain.AppSettings.AutoResumeQueue, per FR-SET-001 and
// the Этап 3 plan's constraint 4: a reconciled task is Paused by default,
// requiring an explicit user Resume, UNLESS the user has separately opted
// into auto-resume via the Settings screen). If enabled is false, this is a
// no-op.
//
// Each ResumeTask failure is logged and skipped, never propagated or
// aborting the rest of recoveredIDs - resuming N-1 out of N recovered tasks
// is strictly better than resuming none of them because task N-1's own
// ResumeTask call happened to fail (e.g. a task already left "paused" via
// some other concurrent path by the time this runs).
func (s *TransferService) AutoResumeIfEnabled(recoveredIDs []int64, enabled bool) {
	if !enabled {
		return
	}

	for _, id := range recoveredIDs {
		if err := s.ResumeTask(id); err != nil {
			log.Printf("transfer: auto-resume recovered task %d: %v", id, err)
		}
	}
}
