package transfer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"threev/internal/domain"
)

// TestSetMaxConcurrentTasksClampsBelowOneToOne is a direct, white-box unit
// test of SetMaxConcurrentTasks' defensive floor (Этап 4 суб-этап 4.3): 0 or
// a negative value must never actually reach maxConcurrentTasks, since that
// would stop dispatch from ever starting anything at all (see dispatch's
// own `len(s.running) >= s.maxConcurrentTasks` check).
func TestSetMaxConcurrentTasksClampsBelowOneToOne(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	for _, n := range []int{0, -1, -100} {
		deps.svc.SetMaxConcurrentTasks(n)

		deps.svc.mu.Lock()
		got := deps.svc.maxConcurrentTasks
		deps.svc.mu.Unlock()

		if got != 1 {
			t.Errorf("SetMaxConcurrentTasks(%d): maxConcurrentTasks = %d, want 1", n, got)
		}
	}

	deps.svc.SetMaxConcurrentTasks(5)

	deps.svc.mu.Lock()
	got := deps.svc.maxConcurrentTasks
	deps.svc.mu.Unlock()

	if got != 5 {
		t.Errorf("SetMaxConcurrentTasks(5): maxConcurrentTasks = %d, want 5", got)
	}
}

// TestSetMaxConcurrentTasksRaisingDispatchesPendingTask verifies
// SetMaxConcurrentTasks' documented "raising the limit immediately tries to
// fill the newly available slot" behavior: with both of
// DefaultMaxConcurrentTasks' slots held by blocking occupier tasks, a third
// task queued behind them sits "pending" until SetMaxConcurrentTasks(3) is
// called - at which point it starts and completes without any further
// action from the test (no manual dispatch()/ResumeTask call).
func TestSetMaxConcurrentTasksRaisingDispatchesPendingTask(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	if DefaultMaxConcurrentTasks != 2 {
		t.Fatalf("this test assumes DefaultMaxConcurrentTasks == 2, got %d - update the number of occupier tasks below", DefaultMaxConcurrentTasks)
	}

	occupier1ID, occupier1Mock := queueBlockingDownload(t, deps, "occupier1")
	occupier2ID, occupier2Mock := queueBlockingDownload(t, deps, "occupier2")

	mock := &putObjectMock{etag: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	profileID := createTestProfile(t, deps.profileRepo, deps.key, server.URL)
	localPath := createSparseFile(t, 1024)

	id, err := deps.svc.QueueUpload(domain.UploadRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		Key:       "pending-task",
		LocalPath: localPath,
	})
	if err != nil {
		t.Fatalf("QueueUpload() returned error: %v", err)
	}

	// Both slots are held (queueBlockingDownload only returns once its
	// GetObject request actually arrived), so dispatch() (called internally
	// by QueueUpload) must have left this task pending.
	waitForTaskStatus(t, deps.svc, id, "pending", 2*time.Second)

	deps.svc.SetMaxConcurrentTasks(3)

	entry := waitForHistoryEntry(t, deps.svc, id, 5*time.Second)
	if entry.Status != "completed" {
		t.Errorf("history entry status = %q, want %q", entry.Status, "completed")
	}

	// Clean up the two occupier tasks so their goroutines/mock HTTP
	// requests don't outlive this test.
	occupier1Mock.blockEnabled = false
	occupier2Mock.blockEnabled = false
	if err := deps.svc.CancelTask(occupier1ID); err != nil {
		t.Errorf("CancelTask(occupier1) returned error: %v", err)
	}
	if err := deps.svc.CancelTask(occupier2ID); err != nil {
		t.Errorf("CancelTask(occupier2) returned error: %v", err)
	}
}

// TestSetMaxConcurrentTasksLoweringDoesNotInterruptRunningTasks verifies
// SetMaxConcurrentTasks' documented behavior that lowering the limit never
// forcibly stops or pauses an already-running task: with both of
// DefaultMaxConcurrentTasks' slots genuinely running (queueBlockingDownload
// waits for each occupant's request to actually arrive), calling
// SetMaxConcurrentTasks(1) must leave both still running (never touched by
// dispatch, which only ever refuses to START new work past the limit).
func TestSetMaxConcurrentTasksLoweringDoesNotInterruptRunningTasks(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	occupier1ID, occupier1Mock := queueBlockingDownload(t, deps, "occupier1")
	occupier2ID, occupier2Mock := queueBlockingDownload(t, deps, "occupier2")

	deps.svc.SetMaxConcurrentTasks(1)

	// Give a (wrongly) overzealous dispatch a moment to have paused/evicted
	// either occupier before asserting both are still genuinely running.
	time.Sleep(50 * time.Millisecond)

	deps.svc.mu.Lock()
	_, occupier1Running := deps.svc.running[occupier1ID]
	_, occupier2Running := deps.svc.running[occupier2ID]
	deps.svc.mu.Unlock()

	if !occupier1Running {
		t.Error("occupier1 no longer present in TransferService.running after SetMaxConcurrentTasks(1), want it left running untouched")
	}
	if !occupier2Running {
		t.Error("occupier2 no longer present in TransferService.running after SetMaxConcurrentTasks(1), want it left running untouched")
	}

	requireNotInHistory(t, deps.svc, occupier1ID)
	requireNotInHistory(t, deps.svc, occupier2ID)

	occupier1Mock.blockEnabled = false
	occupier2Mock.blockEnabled = false
	if err := deps.svc.CancelTask(occupier1ID); err != nil {
		t.Errorf("CancelTask(occupier1) returned error: %v", err)
	}
	if err := deps.svc.CancelTask(occupier2ID); err != nil {
		t.Errorf("CancelTask(occupier2) returned error: %v", err)
	}
}

// TestSetBandwidthLimitsInstallsAndClearsLimiter is a white-box unit test
// (same package as ratelimit.go, so BandwidthLimiter's unexported
// upload/download fields are directly inspectable) confirming
// SetBandwidthLimits actually replaces s.limiter's content: a positive
// value installs a *rate.Limiter configured for that rate, and <= 0 clears
// it back to nil/unlimited (NewBandwidthLimiter's own documented
// convention).
func TestSetBandwidthLimitsInstallsAndClearsLimiter(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	// Starts out nil/unlimited (docs/02-tech-spec.md section 10.6).
	if got := deps.svc.limiter.Load(); got != nil {
		t.Fatalf("limiter.Load() before any SetBandwidthLimits call = %+v, want nil", got)
	}

	deps.svc.SetBandwidthLimits(1000, 2000)

	limiter := deps.svc.limiter.Load()
	if limiter == nil {
		t.Fatal("limiter.Load() after SetBandwidthLimits(1000, 2000) = nil, want a non-nil *BandwidthLimiter")
	}
	if limiter.upload == nil {
		t.Error("limiter.upload = nil, want a configured *rate.Limiter")
	} else if got := limiter.upload.Limit(); got != rate.Limit(1000) {
		t.Errorf("limiter.upload.Limit() = %v, want %v", got, rate.Limit(1000))
	}
	if limiter.download == nil {
		t.Error("limiter.download = nil, want a configured *rate.Limiter")
	} else if got := limiter.download.Limit(); got != rate.Limit(2000) {
		t.Errorf("limiter.download.Limit() = %v, want %v", got, rate.Limit(2000))
	}

	// 0 (or negative) clears a direction back to unlimited, and clearing
	// both directions is observable as upload/download both nil.
	deps.svc.SetBandwidthLimits(0, 0)

	limiter = deps.svc.limiter.Load()
	if limiter == nil {
		t.Fatal("limiter.Load() after SetBandwidthLimits(0, 0) = nil, want a non-nil (but unlimited) *BandwidthLimiter")
	}
	if limiter.upload != nil {
		t.Error("limiter.upload != nil after SetBandwidthLimits(0, 0), want nil (unlimited)")
	}
	if limiter.download != nil {
		t.Error("limiter.download != nil after SetBandwidthLimits(0, 0), want nil (unlimited)")
	}
}

// TestSetBandwidthLimitsConcurrentWithRunningTaskIsRaceFree exercises
// SetBandwidthLimits being called repeatedly from another goroutine while a
// task is genuinely in flight and reading s.limiter.Load() itself
// (runDownloadTask, via DownloadParams.Limiter) - a regression test for
// exactly the data race an earlier plain *BandwidthLimiter field (replaced
// by atomic.Pointer[BandwidthLimiter] in this Block) would have under
// `go test -race`.
func TestSetBandwidthLimitsConcurrentWithRunningTaskIsRaceFree(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	id, mock := queueBlockingDownload(t, deps, "key1")

	stop := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)

		n := int64(1)
		for {
			select {
			case <-stop:
				return
			default:
				deps.svc.SetBandwidthLimits(n*1000, n*2000)
				n++
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)
	close(stop)
	<-done

	mock.blockEnabled = false
	if err := deps.svc.CancelTask(id); err != nil {
		t.Fatalf("CancelTask() returned error: %v", err)
	}
}

// TestSetPartSizeOverrideMBClamps is a direct, white-box unit test of
// SetPartSizeOverrideMB's clamp/clear behavior (Этап 4 суб-этап 4.3):
// <= 0 clears the override to 0 (adaptive default); any other value is
// clamped to [5,128] MB before being converted to bytes.
func TestSetPartSizeOverrideMBClamps(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	// Zero value: no override set yet.
	if got := deps.svc.partSizeOverride.Load(); got != 0 {
		t.Fatalf("partSizeOverride.Load() before any SetPartSizeOverrideMB call = %d, want 0", got)
	}

	tests := []struct {
		mb   int
		want int64
	}{
		{mb: 0, want: 0},
		{mb: -10, want: 0},
		{mb: 1, want: 5 * 1024 * 1024},     // clamped up to the 5MB floor
		{mb: 5, want: 5 * 1024 * 1024},     // exactly the floor
		{mb: 20, want: 20 * 1024 * 1024},   // within bounds, used verbatim
		{mb: 128, want: 128 * 1024 * 1024}, // exactly the ceiling
		{mb: 999, want: 128 * 1024 * 1024}, // clamped down to the 128MB ceiling
	}

	for _, tt := range tests {
		deps.svc.SetPartSizeOverrideMB(tt.mb)

		if got := deps.svc.partSizeOverride.Load(); got != tt.want {
			t.Errorf("SetPartSizeOverrideMB(%d): partSizeOverride.Load() = %d, want %d", tt.mb, got, tt.want)
		}
	}
}

// TestSetPartSizeOverrideMBAppliesToQueuedUpload is an end-to-end
// integration test confirming SetPartSizeOverrideMB's value actually
// reaches a real, queued-and-dispatched multipart upload
// (task.go's runUploadTask populates UploadParams.PartSizeOverride from
// s.partSizeOverride.Load()): a 40MB file normally splits into 8 parts
// under the unmodified adaptive table (< 100MB -> 5MB parts), but with a
// 20MB override in effect it must split into exactly 2 parts instead.
func TestSetPartSizeOverrideMBAppliesToQueuedUpload(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)
	deps.svc.SetPartSizeOverrideMB(20)

	mock := newMPUMock("upload-id-settings-override")
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	profileID := createTestProfile(t, deps.profileRepo, deps.key, server.URL)
	const totalBytes = 40 * 1024 * 1024
	localPath := createSparseFile(t, totalBytes)

	id, err := deps.svc.QueueUpload(domain.UploadRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		Key:       "key1",
		LocalPath: localPath,
	})
	if err != nil {
		t.Fatalf("QueueUpload() returned error: %v", err)
	}

	entry := waitForHistoryEntry(t, deps.svc, id, 10*time.Second)
	if entry.Status != "completed" {
		t.Fatalf("history entry status = %q, want %q", entry.Status, "completed")
	}

	if got := mock.totalUploadPartRequests(); got != 2 {
		t.Errorf("total UploadPart requests = %d, want 2 (20MB override on a 40MB file)", got)
	}
}

// TestRecoverOrphanedTasksReturnsOnlyJustRecoveredIDs directly exercises the
// contract AutoResumeIfEnabled depends on: RecoverOrphanedTasks returns
// only the ids of tasks IT just moved from "running" to "paused", never
// every "paused" row already present in transfer_queue - in particular, a
// task the user had already paused themselves (before the simulated
// crash/restart) must never appear in the returned slice.
func TestRecoverOrphanedTasksReturnsOnlyJustRecoveredIDs(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	profileID := createTestProfile(t, deps.profileRepo, deps.key, "http://127.0.0.1:1") // never contacted

	ctx := context.Background()

	orphaned, err := deps.svc.queueRepo.Create(ctx, domain.TransferTask{
		ProfileID: profileID, Type: "upload", SourcePath: "/tmp/orphaned", DestinationPath: "bucket1/orphaned",
		Status: "running",
	})
	if err != nil {
		t.Fatalf("queueRepo.Create(orphaned) returned error: %v", err)
	}

	alreadyPaused, err := deps.svc.queueRepo.Create(ctx, domain.TransferTask{
		ProfileID: profileID, Type: "upload", SourcePath: "/tmp/already-paused", DestinationPath: "bucket1/already-paused",
		Status: "paused",
	})
	if err != nil {
		t.Fatalf("queueRepo.Create(alreadyPaused) returned error: %v", err)
	}

	recovered, err := deps.svc.RecoverOrphanedTasks()
	if err != nil {
		t.Fatalf("RecoverOrphanedTasks() returned error: %v", err)
	}

	if len(recovered) != 1 || recovered[0] != orphaned.ID {
		t.Errorf("RecoverOrphanedTasks() = %v, want [%d]", recovered, orphaned.ID)
	}

	task, err := deps.svc.queueRepo.GetByID(ctx, orphaned.ID)
	if err != nil {
		t.Fatalf("queueRepo.GetByID(orphaned) returned error: %v", err)
	}
	if task.Status != "paused" {
		t.Errorf("orphaned task status = %q, want %q", task.Status, "paused")
	}

	task, err = deps.svc.queueRepo.GetByID(ctx, alreadyPaused.ID)
	if err != nil {
		t.Fatalf("queueRepo.GetByID(alreadyPaused) returned error: %v", err)
	}
	if task.Status != "paused" {
		t.Errorf("already-paused task status = %q, want %q (untouched)", task.Status, "paused")
	}
}

// TestAutoResumeIfEnabledDisabledIsNoOp verifies AutoResumeIfEnabled(ids,
// false) never resumes anything - the Этап 3 plan's constraint 4 default:
// a crash-recovered task stays Paused unless the user has explicitly opted
// into auto-resume.
func TestAutoResumeIfEnabledDisabledIsNoOp(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	profileID := createTestProfile(t, deps.profileRepo, deps.key, "http://127.0.0.1:1") // never contacted
	ctx := context.Background()

	created, err := deps.svc.queueRepo.Create(ctx, domain.TransferTask{
		ProfileID: profileID, Type: "upload", SourcePath: "/tmp/orphaned", DestinationPath: "bucket1/orphaned",
		Status: "running",
	})
	if err != nil {
		t.Fatalf("queueRepo.Create() returned error: %v", err)
	}

	recovered, err := deps.svc.RecoverOrphanedTasks()
	if err != nil {
		t.Fatalf("RecoverOrphanedTasks() returned error: %v", err)
	}

	deps.svc.AutoResumeIfEnabled(recovered, false)

	// Give a (wrongly) overzealous auto-resume a moment before asserting
	// the task is still sitting untouched as "paused".
	time.Sleep(50 * time.Millisecond)

	task, err := deps.svc.queueRepo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("queueRepo.GetByID() returned error: %v", err)
	}
	if task.Status != "paused" {
		t.Errorf("task status = %q, want %q (AutoResumeIfEnabled(_, false) must be a no-op)", task.Status, "paused")
	}
}

// TestAutoResumeIfEnabledResumesOnlyGivenIDs verifies AutoResumeIfEnabled
// (recoveredIDs, true) resumes exactly the tasks named in recoveredIDs -
// here, a genuinely crash-recovered task (which completes, since its mock
// server is configured to succeed) - while a separate task the user had
// already paused themselves (never included in recoveredIDs) is left
// untouched, even though both are "paused" in transfer_queue at the moment
// AutoResumeIfEnabled runs.
func TestAutoResumeIfEnabledResumesOnlyGivenIDs(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	mock := &putObjectMock{etag: "ffffffffffffffffffffffffffffffff"}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	profileID := createTestProfile(t, deps.profileRepo, deps.key, server.URL)
	localPath := createSparseFile(t, 1024)

	ctx := context.Background()

	// Simulates a task left "running" by a killed process - the scenario
	// RecoverOrphanedTasks exists for.
	orphaned, err := deps.svc.queueRepo.Create(ctx, domain.TransferTask{
		ProfileID: profileID, Type: "upload", SourcePath: localPath, DestinationPath: encodeBucketKey("bucket1", "key1"),
		Status: "running", TotalBytes: 1024,
	})
	if err != nil {
		t.Fatalf("queueRepo.Create(orphaned) returned error: %v", err)
	}

	// Simulates a task the user paused deliberately on a previous run -
	// must never be resumed by AutoResumeIfEnabled, which only ever acts on
	// the ids it is explicitly given.
	userPaused, err := deps.svc.queueRepo.Create(ctx, domain.TransferTask{
		ProfileID: profileID, Type: "upload", SourcePath: "/tmp/user-paused", DestinationPath: "bucket1/user-paused",
		Status: "paused",
	})
	if err != nil {
		t.Fatalf("queueRepo.Create(userPaused) returned error: %v", err)
	}

	recovered, err := deps.svc.RecoverOrphanedTasks()
	if err != nil {
		t.Fatalf("RecoverOrphanedTasks() returned error: %v", err)
	}
	if len(recovered) != 1 || recovered[0] != orphaned.ID {
		t.Fatalf("RecoverOrphanedTasks() = %v, want [%d]", recovered, orphaned.ID)
	}

	deps.svc.AutoResumeIfEnabled(recovered, true)

	entry := waitForHistoryEntry(t, deps.svc, orphaned.ID, 5*time.Second)
	if entry.Status != "completed" {
		t.Errorf("recovered task history entry status = %q, want %q", entry.Status, "completed")
	}

	task, err := deps.svc.queueRepo.GetByID(ctx, userPaused.ID)
	if err != nil {
		t.Fatalf("queueRepo.GetByID(userPaused) returned error: %v", err)
	}
	if task.Status != "paused" {
		t.Errorf("user-paused task status = %q, want %q (must never be auto-resumed)", task.Status, "paused")
	}
}

// TestAutoResumeIfEnabledSkipsFailingIDsBestEffort verifies a ResumeTask
// failure for one id in recoveredIDs (here, an id that does not exist in
// transfer_queue at all) does not prevent AutoResumeIfEnabled from still
// resuming every other, valid id in the same call - best-effort semantics,
// per its own doc comment.
func TestAutoResumeIfEnabledSkipsFailingIDsBestEffort(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	mock := &putObjectMock{etag: "11111111111111111111111111111111"}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	profileID := createTestProfile(t, deps.profileRepo, deps.key, server.URL)
	localPath := createSparseFile(t, 1024)

	ctx := context.Background()

	orphaned, err := deps.svc.queueRepo.Create(ctx, domain.TransferTask{
		ProfileID: profileID, Type: "upload", SourcePath: localPath, DestinationPath: encodeBucketKey("bucket1", "key1"),
		Status: "running", TotalBytes: 1024,
	})
	if err != nil {
		t.Fatalf("queueRepo.Create(orphaned) returned error: %v", err)
	}

	recovered, err := deps.svc.RecoverOrphanedTasks()
	if err != nil {
		t.Fatalf("RecoverOrphanedTasks() returned error: %v", err)
	}
	if len(recovered) != 1 || recovered[0] != orphaned.ID {
		t.Fatalf("RecoverOrphanedTasks() = %v, want [%d]", recovered, orphaned.ID)
	}

	const nonExistentID = int64(9_999_999)
	mixedIDs := []int64{nonExistentID, recovered[0]}

	deps.svc.AutoResumeIfEnabled(mixedIDs, true)

	entry := waitForHistoryEntry(t, deps.svc, orphaned.ID, 5*time.Second)
	if entry.Status != "completed" {
		t.Errorf("history entry status = %q, want %q (the valid id must still be resumed despite the invalid one)", entry.Status, "completed")
	}
}
