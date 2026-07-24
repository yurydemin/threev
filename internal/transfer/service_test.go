package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"threev/internal/connection"
	"threev/internal/crypto"
	"threev/internal/domain"
	"threev/internal/s3client"
	"threev/internal/storage"
)

// testTransferDeps bundles a fresh TransferService (over a real, migrated,
// temp-file SQLite database - mirroring connection/service_test.go's
// newTestConnectionService and filemanager's own service tests) with the
// pieces needed to create test profiles for it.
type testTransferDeps struct {
	svc         *TransferService
	profileRepo *storage.ProfileRepository
	key         [32]byte
	keyBox      *crypto.KeyBox
}

// newTestTransferService opens a fresh migrated SQLite database backed by a
// temporary file and returns a TransferService over it, using a fixed
// (test-only) 32-byte encryption key already Set on a fresh *crypto.KeyBox -
// identical technique to connection/service_test.go's
// newTestConnectionService (Этап 4 суб-этап 4.4: see
// TestQueueDownloadPrefixReturnsErrLockedWhenLocked/
// TestRunTaskFailsWithErrLockedWhenLocked for the dedicated locked-state
// tests, which build their own, never-Set KeyBox instead).
func newTestTransferService(t *testing.T) testTransferDeps {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "transfer_service_test.db")

	db, err := storage.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB(%q) returned error: %v", dbPath, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close() returned error: %v", err)
		}
	})

	profileRepo := storage.NewProfileRepository(db)
	queueRepo := storage.NewTransferQueueRepository(db)
	historyRepo := storage.NewTransferHistoryRepository(db)

	var key [32]byte
	for i := range key {
		key[i] = byte(i)
	}

	keyBox := crypto.NewKeyBox()
	keyBox.Set(key)

	connMgr := s3client.NewConnectionManager(profileRepo, keyBox)
	breaker := s3client.NewCircuitBreaker()
	retryPolicies := s3client.NewRetryPolicyStore()

	svc := NewTransferService(profileRepo, keyBox, queueRepo, historyRepo, connMgr, breaker, retryPolicies)

	return testTransferDeps{svc: svc, profileRepo: profileRepo, key: key, keyBox: keyBox}
}

// testProfileNameCounter guarantees every createTestProfile call gets a
// unique profile Name within a test binary run - SaveProfile rejects
// duplicate names (domain.ErrDuplicateProfileName), and t.Parallel() tests
// in this file each create their own profile(s) concurrently.
var testProfileNameCounter atomic.Int64

// createTestProfile saves (via connection.ConnectionService.SaveProfile,
// which encrypts SecretAccessKey exactly as production code does - a plain
// storage.ProfileRepository.Create would store it as unencrypted plaintext,
// which s3client.ConnectionManager's crypto.Decrypt call would then fail
// against) a profile pointed at endpointURL, returning its ID.
//
// key is wrapped in a fresh, already-Set *crypto.KeyBox here (rather than
// this function's callers each needing their own) purely because
// ConnectionService's constructor now takes a *crypto.KeyBox, not a raw
// [32]byte (Этап 4 суб-этап 4.4) - every call site of createTestProfile
// still just passes the [32]byte key it already has (typically
// testTransferDeps.key), unaffected by this internal detail.
func createTestProfile(t *testing.T, profileRepo *storage.ProfileRepository, key [32]byte, endpointURL string) int64 {
	t.Helper()

	keyBox := crypto.NewKeyBox()
	keyBox.Set(key)

	connSvc := connection.NewConnectionService(profileRepo, keyBox)

	saved, err := connSvc.SaveProfile(domain.Profile{
		Name:            fmt.Sprintf("transfer-service-test-%d", testProfileNameCounter.Add(1)),
		EndpointURL:     endpointURL,
		Region:          "us-east-1",
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "supersecret",
		PathStyle:       true,
		VerifySSL:       true,
	})
	if err != nil {
		t.Fatalf("SaveProfile() returned error: %v", err)
	}

	return saved.ID
}

// waitForTaskStatus polls GetQueue() until id is present with status want,
// failing the test if timeout elapses first. Used for statuses a task can
// durably sit in inside transfer_queue (pending/paused/failed) - a
// completed/cancelled task is archived out of the queue entirely, see
// waitForHistoryEntry for that case.
func waitForTaskStatus(t *testing.T, svc *TransferService, id int64, want string, timeout time.Duration) domain.TransferTask {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for {
		tasks, err := svc.GetQueue()
		if err != nil {
			t.Fatalf("GetQueue() returned error: %v", err)
		}

		for _, task := range tasks {
			if task.ID == id && task.Status == want {
				return task
			}
		}

		if time.Now().After(deadline) {
			t.Fatalf("task %d did not reach status %q within %s (queue: %+v)", id, want, timeout, tasks)
		}

		time.Sleep(10 * time.Millisecond)
	}
}

// waitForHistoryEntry polls GetHistory() until a row with the given
// queueID is present, failing the test if timeout elapses first.
func waitForHistoryEntry(t *testing.T, svc *TransferService, queueID int64, timeout time.Duration) domain.TransferHistoryEntry {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for {
		entries, err := svc.GetHistory(100)
		if err != nil {
			t.Fatalf("GetHistory() returned error: %v", err)
		}

		for _, entry := range entries {
			if entry.QueueID == queueID {
				return entry
			}
		}

		if time.Now().After(deadline) {
			t.Fatalf("task %d did not appear in history within %s", queueID, timeout)
		}

		time.Sleep(10 * time.Millisecond)
	}
}

// requireNotInQueue fails the test if id is present in GetQueue().
func requireNotInQueue(t *testing.T, svc *TransferService, id int64) {
	t.Helper()

	tasks, err := svc.GetQueue()
	if err != nil {
		t.Fatalf("GetQueue() returned error: %v", err)
	}

	for _, task := range tasks {
		if task.ID == id {
			t.Errorf("task %d still present in GetQueue(), want it archived to history", id)
		}
	}
}

// requireNotInHistory fails the test if queueID is present in GetHistory().
func requireNotInHistory(t *testing.T, svc *TransferService, queueID int64) {
	t.Helper()

	entries, err := svc.GetHistory(100)
	if err != nil {
		t.Fatalf("GetHistory() returned error: %v", err)
	}

	for _, entry := range entries {
		if entry.QueueID == queueID {
			t.Errorf("task %d present in history (status %q), want it to remain in transfer_queue", queueID, entry.Status)
		}
	}
}

// putObjectMock is a minimal S3-compatible mock server implementing just
// the single PutObject operation upload.go's uploadSingle path exercises -
// every file this test file uploads stays well under singlePutThreshold, so
// mpuMock's fuller multipart surface (multipart_upload_test.go) is not
// needed here. fail/etag are mutex-guarded so a test can safely flip fail
// between a failed attempt and a RetryTask call (TestRetryFailedUploadTask
// EventuallyCompletes) while the mock's own handler goroutine may still be
// reachable concurrently from a previous, already-finished request.
type putObjectMock struct {
	etag string

	mu       sync.Mutex
	putCount int
	fail     bool
}

func (m *putObjectMock) setFail(fail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.fail = fail
}

func (m *putObjectMock) requestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.putCount
}

func (m *putObjectMock) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	m.putCount++
	fail := m.fail
	m.mu.Unlock()

	_, _ = io.Copy(io.Discard, r.Body)

	if fail {
		writeXML(w, http.StatusForbidden, mpuAccessDeniedBody) // reused from multipart_upload_test.go, same package
		return
	}

	w.Header().Set("ETag", fmt.Sprintf(`"%s"`, m.etag))
	w.WriteHeader(http.StatusOK)
}

// queueBlockingDownload queues a download task whose single GetObject
// segment hangs until its context is canceled (reusing rangeMock's
// blockOffset/blockEnabled/blockStarted mechanism from
// range_download_test.go, same package - see its doc comment), and waits
// for that request to actually arrive before returning - so the caller can
// rely on the task being genuinely "running" (mid-flight, holding one
// TransferService concurrency slot) rather than merely "queued".
func queueBlockingDownload(t *testing.T, deps testTransferDeps, key string) (id int64, mock *rangeMock) {
	t.Helper()

	mock = newRangeMock([]byte("blocking download occupant content, held open until canceled/paused"))
	mock.blockEnabled = true
	mock.blockOffset = 0

	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	profileID := createTestProfile(t, deps.profileRepo, deps.key, server.URL)

	localPath := filepath.Join(t.TempDir(), "downloaded.bin")

	id, err := deps.svc.QueueDownload(domain.DownloadRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		Key:       key,
		LocalPath: localPath,
	})
	if err != nil {
		t.Fatalf("QueueDownload() returned error: %v", err)
	}

	select {
	case <-mock.blockStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the blocking download's GetObject request to arrive")
	}

	return id, mock
}

// TestQueueUploadLifecycleCompletesAndMovesToHistory drives a full,
// realistic pending->running->completed cycle for a small (single-
// PutObject) upload against an httptest mock: QueueUpload creates the row,
// dispatch() (called internally by QueueUpload) starts it without any
// further action from the test, and the task ends up archived to
// transfer_history and gone from GetQueue().
func TestQueueUploadLifecycleCompletesAndMovesToHistory(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	mock := &putObjectMock{etag: "cccccccccccccccccccccccccccccccc"}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	profileID := createTestProfile(t, deps.profileRepo, deps.key, server.URL)
	localPath := createSparseFile(t, 1024) // 1KB, well under singlePutThreshold

	id, err := deps.svc.QueueUpload(domain.UploadRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		Key:       "key1",
		LocalPath: localPath,
	})
	if err != nil {
		t.Fatalf("QueueUpload() returned error: %v", err)
	}

	if id == 0 {
		t.Fatal("QueueUpload() returned id 0")
	}

	entry := waitForHistoryEntry(t, deps.svc, id, 5*time.Second)

	if entry.Status != "completed" {
		t.Errorf("history entry status = %q, want %q", entry.Status, "completed")
	}
	if entry.ErrorMessage != "" {
		t.Errorf("history entry ErrorMessage = %q, want empty", entry.ErrorMessage)
	}

	requireNotInQueue(t, deps.svc, id)

	if got := mock.requestCount(); got != 1 {
		t.Errorf("PutObject request count = %d, want 1", got)
	}
}

// TestPauseThenResumeRunningDownloadTask verifies Pause on a task that is
// genuinely mid-flight (not merely queued - see queueBlockingDownload):
// the task moves to "paused" and stays in transfer_queue (never archived),
// and a subsequent Resume (with blocking disabled) lets it complete
// normally and archives it to transfer_history.
func TestPauseThenResumeRunningDownloadTask(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	id, mock := queueBlockingDownload(t, deps, "key1")

	if err := deps.svc.PauseTask(id); err != nil {
		t.Fatalf("PauseTask() returned error: %v", err)
	}

	task := waitForTaskStatus(t, deps.svc, id, "paused", 5*time.Second)
	if task.ID != id {
		t.Fatalf("waitForTaskStatus returned task %+v, want ID %d", task, id)
	}

	requireNotInHistory(t, deps.svc, id)

	// PauseTask already blocked until the paused task's own goroutine
	// fully exited (<-rt.done), so there is no concurrent access to mock
	// left to race with this write - see PauseTask's doc comment.
	mock.blockEnabled = false

	if err := deps.svc.ResumeTask(id); err != nil {
		t.Fatalf("ResumeTask() returned error: %v", err)
	}

	entry := waitForHistoryEntry(t, deps.svc, id, 5*time.Second)
	if entry.Status != "completed" {
		t.Errorf("history entry status = %q, want %q", entry.Status, "completed")
	}
}

// TestCancelRunningDownloadTask verifies Cancel on a genuinely mid-flight
// task: it moves straight to "cancelled" in transfer_history and is gone
// from GetQueue() - no "failed"/"paused" intermediate state.
func TestCancelRunningDownloadTask(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	id, _ := queueBlockingDownload(t, deps, "key1")

	if err := deps.svc.CancelTask(id); err != nil {
		t.Fatalf("CancelTask() returned error: %v", err)
	}

	entry := waitForHistoryEntry(t, deps.svc, id, 5*time.Second)
	if entry.Status != "cancelled" {
		t.Errorf("history entry status = %q, want %q", entry.Status, "cancelled")
	}

	requireNotInQueue(t, deps.svc, id)
}

// TestCancelPendingTaskArchivesDirectlyWithoutRunning verifies CancelTask's
// other branch: a task that has never started (both concurrency slots held
// by two other, deliberately blocked, tasks) is archived synchronously by
// CancelTask itself, with no goroutine ever having raced to do it.
func TestCancelPendingTaskArchivesDirectlyWithoutRunning(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	if DefaultMaxConcurrentTasks != 2 {
		t.Fatalf("this test assumes DefaultMaxConcurrentTasks == 2, got %d - update the number of occupier tasks below", DefaultMaxConcurrentTasks)
	}

	occupier1ID, _ := queueBlockingDownload(t, deps, "occupier1")
	occupier2ID, _ := queueBlockingDownload(t, deps, "occupier2")

	// Both concurrency slots are now held (queueBlockingDownload only
	// returns once each occupant's GetObject request has actually
	// arrived at its mock, so both are genuinely "running", not merely
	// "pending" themselves).
	profileID := createTestProfile(t, deps.profileRepo, deps.key, "http://127.0.0.1:1") // never actually contacted
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

	// dispatch() (called synchronously inside QueueUpload, before it
	// returned) already observed both slots full and left this task
	// "pending" - no need to poll/wait, this is deterministic given
	// queueBlockingDownload's own synchronization.
	tasks, err := deps.svc.GetQueue()
	if err != nil {
		t.Fatalf("GetQueue() returned error: %v", err)
	}

	found := false
	for _, task := range tasks {
		if task.ID == id {
			found = true
			if task.Status != "pending" {
				t.Errorf("task %d status = %q, want %q (both concurrency slots should still be held by the occupier tasks)", id, task.Status, "pending")
			}
		}
	}
	if !found {
		t.Fatalf("task %d not found in GetQueue()", id)
	}

	if err := deps.svc.CancelTask(id); err != nil {
		t.Fatalf("CancelTask() returned error: %v", err)
	}

	entry := waitForHistoryEntry(t, deps.svc, id, 2*time.Second)
	if entry.Status != "cancelled" {
		t.Errorf("history entry status = %q, want %q", entry.Status, "cancelled")
	}

	requireNotInQueue(t, deps.svc, id)

	// Clean up the two occupier tasks so their goroutines/mock HTTP
	// requests don't outlive this test.
	if err := deps.svc.CancelTask(occupier1ID); err != nil {
		t.Errorf("CancelTask(occupier1) returned error: %v", err)
	}
	if err := deps.svc.CancelTask(occupier2ID); err != nil {
		t.Errorf("CancelTask(occupier2) returned error: %v", err)
	}
}

// TestFailedUploadTaskStaysInQueueNotArchived is a direct regression test
// for this Block's correction to the Этап 3 plan's original draft (see
// task.go's handleTaskResult doc comment): a task that fails (here, a
// non-retryable 403 AccessDenied from PutObject - see mpuAccessDeniedBody/
// putObjectMock.fail, chosen so the test does not have to wait through
// s3client.PartRetryPolicy's real, multi-second retry schedule for a
// retryable error) must be found afterward as status "failed" in
// GetQueue(), and must NOT have been archived to transfer_history - which
// would make RetryTask unable to find it and resume its
// MultipartUploadID/state.
func TestFailedUploadTaskStaysInQueueNotArchived(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	mock := &putObjectMock{fail: true}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	profileID := createTestProfile(t, deps.profileRepo, deps.key, server.URL)
	localPath := createSparseFile(t, 1024)

	id, err := deps.svc.QueueUpload(domain.UploadRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		Key:       "key1",
		LocalPath: localPath,
	})
	if err != nil {
		t.Fatalf("QueueUpload() returned error: %v", err)
	}

	task := waitForTaskStatus(t, deps.svc, id, "failed", 5*time.Second)
	if task.ErrorMessage == "" {
		t.Error("failed task's ErrorMessage is empty, want the PutObject failure recorded")
	}

	requireNotInHistory(t, deps.svc, id)
}

// TestRunTaskFailsWithErrLockedWhenLocked is runTask's (task.go) Этап 4
// суб-этап 4.4 guard test: unlike every other guarded method in this
// codebase, runTask always executes on its own goroutine with no direct
// caller to hand a domain.ErrLocked back to - so this test observes the
// guard's effect the same indirect way
// TestFailedUploadTaskStaysInQueueNotArchived observes any other runTask
// failure: the task ends up "failed" in the queue (never archived), with
// domain.ErrLocked's message present in ErrorMessage.
//
// The profile itself is still created normally (createTestProfile builds
// its own, separate, already-Set KeyBox internally - see its own doc
// comment - so profile creation is unaffected by deps.keyBox's state)
// before deps.keyBox.Clear() simulates "the application is currently
// locked" for the QueueUpload/runTask call that follows.
func TestRunTaskFailsWithErrLockedWhenLocked(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	mock := &putObjectMock{etag: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	profileID := createTestProfile(t, deps.profileRepo, deps.key, server.URL)
	localPath := createSparseFile(t, 1024)

	deps.keyBox.Clear()

	id, err := deps.svc.QueueUpload(domain.UploadRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		Key:       "key1",
		LocalPath: localPath,
	})
	if err != nil {
		t.Fatalf("QueueUpload() returned error: %v (QueueUpload itself is unguarded - only runTask should fail)", err)
	}

	task := waitForTaskStatus(t, deps.svc, id, "failed", 5*time.Second)
	if !strings.Contains(task.ErrorMessage, domain.ErrLocked.Error()) {
		t.Errorf("failed task's ErrorMessage = %q, want it to contain %q", task.ErrorMessage, domain.ErrLocked.Error())
	}

	requireNotInHistory(t, deps.svc, id)
}

// TestQueueDownloadPrefixReturnsErrLockedWhenLocked is QueueDownloadPrefix's
// own Этап 4 суб-этап 4.4 guard test - unlike QueueUpload/QueueDownload,
// QueueDownloadPrefix resolves the profile synchronously and so has its own
// direct guard (see its own doc comment), returning domain.ErrLocked
// immediately rather than queuing anything at all.
func TestQueueDownloadPrefixReturnsErrLockedWhenLocked(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	profileID := createTestProfile(t, deps.profileRepo, deps.key, "http://127.0.0.1:1")

	deps.keyBox.Clear()

	_, err := deps.svc.QueueDownloadPrefix(profileID, "bucket1", "prefix/", t.TempDir())
	if !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("QueueDownloadPrefix() on a locked service error = %v, want errors.Is(_, domain.ErrLocked)", err)
	}
}

// TestRetryFailedUploadTaskEventuallyCompletes verifies RetryTask resets a
// failed task back to "pending" (returning the SAME id, per RetryTask's
// documented departure from a literal ТЗ 9.3 reading), which dispatch()
// then picks up again - and, once the mock is reconfigured to succeed,
// completes normally and is archived.
func TestRetryFailedUploadTaskEventuallyCompletes(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	mock := &putObjectMock{fail: true, etag: "dddddddddddddddddddddddddddddddd"}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	profileID := createTestProfile(t, deps.profileRepo, deps.key, server.URL)
	localPath := createSparseFile(t, 1024)

	id, err := deps.svc.QueueUpload(domain.UploadRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		Key:       "key1",
		LocalPath: localPath,
	})
	if err != nil {
		t.Fatalf("QueueUpload() returned error: %v", err)
	}

	waitForTaskStatus(t, deps.svc, id, "failed", 5*time.Second)

	mock.setFail(false)

	retryID, err := deps.svc.RetryTask(id)
	if err != nil {
		t.Fatalf("RetryTask() returned error: %v", err)
	}
	if retryID != id {
		t.Errorf("RetryTask() returned id %d, want the same id %d (must not create a new transfer_queue row)", retryID, id)
	}

	entry := waitForHistoryEntry(t, deps.svc, id, 5*time.Second)
	if entry.Status != "completed" {
		t.Errorf("history entry status = %q, want %q", entry.Status, "completed")
	}
}

// TestReorderTaskChangesQueueOrder verifies ReorderTask's effect on
// GetQueue()'s priority/created_at ordering (FR-QUEUE-003), using rows
// inserted directly via the service's own (unexported, same-package)
// queueRepo - deliberately bypassing QueueUpload/dispatch entirely, since
// this test only cares about ordering among "pending" rows and has no need
// to actually run any transfer against a mock S3 server.
func TestReorderTaskChangesQueueOrder(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	profileID := createTestProfile(t, deps.profileRepo, deps.key, "http://127.0.0.1:1") // never contacted

	ctx := context.Background()

	taskA, err := deps.svc.queueRepo.Create(ctx, domain.TransferTask{
		ProfileID: profileID, Type: "upload", SourcePath: "/tmp/a", DestinationPath: "bucket1/a",
		Status: "pending", Priority: 10,
	})
	if err != nil {
		t.Fatalf("queueRepo.Create() returned error: %v", err)
	}

	taskB, err := deps.svc.queueRepo.Create(ctx, domain.TransferTask{
		ProfileID: profileID, Type: "upload", SourcePath: "/tmp/b", DestinationPath: "bucket1/b",
		Status: "pending", Priority: 20,
	})
	if err != nil {
		t.Fatalf("queueRepo.Create() returned error: %v", err)
	}

	queue, err := deps.svc.GetQueue()
	if err != nil {
		t.Fatalf("GetQueue() returned error: %v", err)
	}
	if len(queue) != 2 || queue[0].ID != taskA.ID || queue[1].ID != taskB.ID {
		t.Fatalf("initial GetQueue() = %+v, want [taskA(%d), taskB(%d)] in that order", queue, taskA.ID, taskB.ID)
	}

	if err := deps.svc.ReorderTask(taskB.ID, 5); err != nil {
		t.Fatalf("ReorderTask() returned error: %v", err)
	}

	queue, err = deps.svc.GetQueue()
	if err != nil {
		t.Fatalf("GetQueue() returned error: %v", err)
	}
	if len(queue) != 2 || queue[0].ID != taskB.ID || queue[1].ID != taskA.ID {
		t.Fatalf("GetQueue() after ReorderTask(taskB, 5) = %+v, want [taskB(%d), taskA(%d)] in that order", queue, taskB.ID, taskA.ID)
	}
}

// TestEncodeSplitBucketKeyRoundTrip is a small table-driven unit test of
// the SourcePath/DestinationPath encoding helpers used by QueueUpload/
// QueueDownload/task.go's taskBucketKey.
// TestCancelTasksForProfileCancelsAllQueuedTasks verifies
// CancelTasksForProfile against a profile with two queued tasks in
// different statuses (pending and paused, neither ever running - both
// concurrency slots are held throughout by two unrelated occupier tasks
// belonging to a different profile, mirroring
// TestCancelPendingTaskArchivesDirectlyWithoutRunning's setup): both are
// cancelled and archived to transfer_history, the returned count is 2, and
// the unrelated occupier tasks (a different profile) are left untouched.
func TestCancelTasksForProfileCancelsAllQueuedTasks(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	if DefaultMaxConcurrentTasks != 2 {
		t.Fatalf("this test assumes DefaultMaxConcurrentTasks == 2, got %d - update the number of occupier tasks below", DefaultMaxConcurrentTasks)
	}

	occupier1ID, _ := queueBlockingDownload(t, deps, "occupier1")
	occupier2ID, _ := queueBlockingDownload(t, deps, "occupier2")

	// Both concurrency slots are now held, so every task queued below stays
	// "pending" (dispatch() has nowhere to run it) unless explicitly paused.
	targetProfileID := createTestProfile(t, deps.profileRepo, deps.key, "http://127.0.0.1:1") // never actually contacted

	pendingID, err := deps.svc.QueueDownload(domain.DownloadRequest{
		ProfileID: targetProfileID,
		Bucket:    "bucket1",
		Key:       "pending-task",
		LocalPath: filepath.Join(t.TempDir(), "pending.bin"),
	})
	if err != nil {
		t.Fatalf("QueueDownload() returned error: %v", err)
	}

	pausedID, err := deps.svc.QueueDownload(domain.DownloadRequest{
		ProfileID: targetProfileID,
		Bucket:    "bucket1",
		Key:       "paused-task",
		LocalPath: filepath.Join(t.TempDir(), "paused.bin"),
	})
	if err != nil {
		t.Fatalf("QueueDownload() returned error: %v", err)
	}

	// pausedID is still "pending" at this point (both slots are held) -
	// PauseTask's non-running branch moves it straight to "paused" without
	// it ever having run, giving this test its second, distinct status.
	if err := deps.svc.PauseTask(pausedID); err != nil {
		t.Fatalf("PauseTask() returned error: %v", err)
	}

	waitForTaskStatus(t, deps.svc, pausedID, "paused", 2*time.Second)

	cancelled, err := deps.svc.CancelTasksForProfile(targetProfileID)
	if err != nil {
		t.Fatalf("CancelTasksForProfile() returned error: %v", err)
	}

	if cancelled != 2 {
		t.Errorf("CancelTasksForProfile() cancelled = %d, want 2", cancelled)
	}

	for _, id := range []int64{pendingID, pausedID} {
		entry := waitForHistoryEntry(t, deps.svc, id, 2*time.Second)
		if entry.Status != "cancelled" {
			t.Errorf("history entry for task %d status = %q, want %q", id, entry.Status, "cancelled")
		}
		requireNotInQueue(t, deps.svc, id)
	}

	// The unrelated occupier tasks (a different profile) must be untouched.
	tasks, err := deps.svc.GetQueue()
	if err != nil {
		t.Fatalf("GetQueue() returned error: %v", err)
	}

	occupierIDs := map[int64]bool{occupier1ID: true, occupier2ID: true}
	found := 0
	for _, task := range tasks {
		if occupierIDs[task.ID] {
			found++
		}
	}
	if found != 2 {
		t.Errorf("found %d of the 2 occupier tasks still in GetQueue(), want 2 (untouched)", found)
	}

	// Clean up the two occupier tasks so their goroutines/mock HTTP
	// requests don't outlive this test.
	if err := deps.svc.CancelTask(occupier1ID); err != nil {
		t.Errorf("CancelTask(occupier1) returned error: %v", err)
	}
	if err := deps.svc.CancelTask(occupier2ID); err != nil {
		t.Errorf("CancelTask(occupier2) returned error: %v", err)
	}
}

// TestCancelTasksForProfileNoTasksReturnsZero verifies CancelTasksForProfile
// on a profile with no queued tasks at all - the common case for most
// profile deletions - returns (0, nil), never an error.
func TestCancelTasksForProfileNoTasksReturnsZero(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	profileID := createTestProfile(t, deps.profileRepo, deps.key, "http://127.0.0.1:1")

	cancelled, err := deps.svc.CancelTasksForProfile(profileID)
	if err != nil {
		t.Fatalf("CancelTasksForProfile() returned error: %v", err)
	}

	if cancelled != 0 {
		t.Errorf("CancelTasksForProfile() cancelled = %d, want 0", cancelled)
	}
}

// TestQueueCopyBetweenProfilesCreatesTasksWithCorrectFields verifies
// QueueCopyBetweenProfiles queues one "copy_cross" task per key, each with
// ProfileID/DestProfileID/TotalBytes/IsMove set correctly - TotalBytes
// learned from a HeadObject against the SOURCE profile (see this method's
// own doc comment). The destination profile here points nowhere real
// (never actually contacted by anything this test itself does): this test
// only cares about what QueueCopyBetweenProfiles itself persists at queue
// time, checked via an immediate GetQueue() call - mirroring
// TestReorderTaskChangesQueueOrder's own "read straight back from GetQueue(),
// no need to wait for anything" style - regardless of whether dispatch()
// has, by the time that call runs, already raced ahead and started one or
// both tasks running in the background (harmless either way: ProfileID/
// DestProfileID/TotalBytes/IsMove are all set once, at Create time, and
// never change over a task's lifecycle).
func TestQueueCopyBetweenProfilesCreatesTasksWithCorrectFields(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	content := []byte("cross-connection copy test content")
	mock := newRangeMock(content)

	sourceServer := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(sourceServer.Close)

	sourceProfileID := createTestProfile(t, deps.profileRepo, deps.key, sourceServer.URL)
	destProfileID := createTestProfile(t, deps.profileRepo, deps.key, "http://127.0.0.1:1") // never actually contacted by this test

	ids, err := deps.svc.QueueCopyBetweenProfiles(sourceProfileID, destProfileID, "bucket1", []string{"key-a.txt", "key-b.txt"}, "bucket2", "dest-prefix/", true)
	if err != nil {
		t.Fatalf("QueueCopyBetweenProfiles() returned error: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("QueueCopyBetweenProfiles() returned %d ids, want 2", len(ids))
	}

	tasks, err := deps.svc.GetQueue()
	if err != nil {
		t.Fatalf("GetQueue() returned error: %v", err)
	}

	byID := make(map[int64]domain.TransferTask, len(tasks))
	for _, task := range tasks {
		byID[task.ID] = task
	}

	for _, id := range ids {
		task, ok := byID[id]
		if !ok {
			t.Errorf("task %d not found in GetQueue()", id)
			continue
		}

		if task.ProfileID != sourceProfileID {
			t.Errorf("task %d ProfileID = %d, want %d", id, task.ProfileID, sourceProfileID)
		}
		if task.DestProfileID != destProfileID {
			t.Errorf("task %d DestProfileID = %d, want %d", id, task.DestProfileID, destProfileID)
		}
		if task.Type != "copy_cross" {
			t.Errorf("task %d Type = %q, want %q", id, task.Type, "copy_cross")
		}
		if task.TotalBytes != int64(len(content)) {
			t.Errorf("task %d TotalBytes = %d, want %d", id, task.TotalBytes, len(content))
		}
		if !task.IsMove {
			t.Errorf("task %d IsMove = false, want true", id)
		}
	}

	// Clean up: cancel both tasks (whatever status they have reached by
	// now - pending/running/failed are all accepted by CancelTask) so
	// their goroutines/mock HTTP requests don't outlive this test.
	for _, id := range ids {
		if err := deps.svc.CancelTask(id); err != nil {
			t.Errorf("CancelTask(%d) returned error: %v", id, err)
		}
	}
}

// TestQueueCopyBetweenProfilesReturnsErrLockedWhenLocked is
// QueueCopyBetweenProfiles' own Этап 4 суб-этап 4.4 guard test - like
// QueueDownloadPrefix/QueueDownloadPrefixZip, it resolves the source
// profile synchronously and so has its own direct guard, returning
// domain.ErrLocked immediately rather than queuing anything at all.
func TestQueueCopyBetweenProfilesReturnsErrLockedWhenLocked(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	sourceProfileID := createTestProfile(t, deps.profileRepo, deps.key, "http://127.0.0.1:1")
	destProfileID := createTestProfile(t, deps.profileRepo, deps.key, "http://127.0.0.1:1")

	deps.keyBox.Clear()

	_, err := deps.svc.QueueCopyBetweenProfiles(sourceProfileID, destProfileID, "bucket1", []string{"key-a.txt"}, "bucket2", "dest-prefix/", false)
	if !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("QueueCopyBetweenProfiles() on a locked service error = %v, want errors.Is(_, domain.ErrLocked)", err)
	}
}

func TestEncodeSplitBucketKeyRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		bucket, key string
	}{
		{"bucket1", "key1"},
		{"my-bucket", "a/b/c/d.txt"},
		{"b", "k"},
	}

	for _, tt := range tests {
		encoded := encodeBucketKey(tt.bucket, tt.key)

		gotBucket, gotKey, err := splitBucketKey(encoded)
		if err != nil {
			t.Fatalf("splitBucketKey(%q) returned error: %v", encoded, err)
		}

		if gotBucket != tt.bucket || gotKey != tt.key {
			t.Errorf("splitBucketKey(encodeBucketKey(%q, %q)) = (%q, %q), want (%q, %q)", tt.bucket, tt.key, gotBucket, gotKey, tt.bucket, tt.key)
		}
	}
}

func TestSplitBucketKeyInvalid(t *testing.T) {
	t.Parallel()

	for _, s := range []string{"", "no-slash", "/leading-empty-bucket", "trailing-empty-key/"} {
		if _, _, err := splitBucketKey(s); err == nil {
			t.Errorf("splitBucketKey(%q) returned a nil error, want an error", s)
		}
	}
}

func TestObjectPrefixOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  string
		want string
	}{
		{"", ""},
		{"file.txt", ""},
		{"folder/file.txt", "folder/"},
		{"a/b/c/file.txt", "a/b/c/"},
	}

	for _, tt := range tests {
		if got := objectPrefixOf(tt.key); got != tt.want {
			t.Errorf("objectPrefixOf(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}
