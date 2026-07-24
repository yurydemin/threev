package transfer

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// Deliberately NOT t.Parallel(): unlike virtually every other test in this
// package, every test below actually exercises crossConnectionTempPath
// (cross_connection_copy.go) - a path rooted at the real, process-wide
// os.TempDir(), keyed only by taskID (never by anything test-specific like
// t.TempDir()), by design: production only ever has ONE shared
// transfer_queue database, so taskID alone is always globally unique there.
// Each test in THIS file, however, opens its own fresh, from-scratch SQLite
// database (newTestTransferService), so its own first queued task always
// gets the exact same taskID (1) as every other test's first queued task -
// if two such tests ran concurrently (t.Parallel()) using the same source
// key's basename, their two "copy_cross" tasks would download/upload
// through the literal same staging file path at the same time, corrupting
// each other's in-flight data (reproduced while writing this file: a
// content mismatch straight out of a DIFFERENT test's mock). Running these
// tests sequentially (the default without t.Parallel()) avoids the
// temporal overlap entirely; each test additionally uses its own distinct
// source key basename as a second, defense-in-depth layer.

// crossSourceMockBucket is the fixed "source" bucket name every test in
// this file queues a copy_cross task's source side against, mirroring
// zipObjectMock's identical fixed-bucket convention (zip_download_test.go's
// own doc comment) - since every one of this file's tests needs exactly
// one, unvarying source bucket, there is no reason for newCrossSourceMock
// to take one as a parameter.
const crossSourceMockBucket = "bucket1"

// crossSourceMock is the SOURCE-side mock server for
// runCrossConnectionCopyTask's tests: it embeds *rangeMock unchanged
// (HeadObject/Range-GetObject - exactly what Download's headObject/
// downloadSegment issue against the source profile, identical to every
// plain "download" task's own mock), adding only the one operation a
// "copy_cross" MOVE additionally needs from its source profile:
// DeleteObject, tracked here (deleteCount/deletedKey) so a test can assert
// whether/when it was called.
type crossSourceMock struct {
	*rangeMock

	mu          sync.Mutex
	deleteCount int
	deletedKey  string
}

func newCrossSourceMock(content []byte) *crossSourceMock {
	return &crossSourceMock{rangeMock: newRangeMock(content)}
}

func (m *crossSourceMock) deleteRequestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.deleteCount
}

func (m *crossSourceMock) handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		m.rangeMock.handler(w, r)
		return
	}

	m.mu.Lock()
	m.deleteCount++
	m.deletedKey = strings.TrimPrefix(r.URL.Path, "/"+crossSourceMockBucket+"/")
	m.mu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

// crossDestMockBucket is the fixed "destination" bucket name every test in
// this file queues a copy_cross task's destination side against - see
// crossSourceMockBucket's identical doc comment for why this is a constant
// rather than a newCrossDestMock parameter.
const crossDestMockBucket = "bucket2"

// crossDestMock is the DESTINATION-side mock server for
// runCrossConnectionCopyTask's tests: a single-PutObject-only mock
// (every test in this file uploads content well under singlePutThreshold,
// exactly like putObjectMock/service_test.go), additionally recording each
// uploaded key's full body (so a test can assert the destination actually
// received the right bytes) and supporting a blockEnabled hang (mirroring
// rangeMock.blockEnabled/queueBlockingDownload's identical technique) for
// the Pause-mid-upload-phase test.
//
// release is this mock's own addition, absent from rangeMock: a request
// whose BODY has already been fully sent (true of every PUT this mock ever
// receives, unlike a bodyless GET/HEAD) does not have its underlying TCP
// connection closed by Go's net/http client merely because the request's
// context was canceled - client.PutObject itself still returns promptly
// with a context-canceled error (confirmed empirically while writing this
// test: this is what lets PauseTask() below return in good time), but the
// SERVER-side handler goroutine blocked on r.Context().Done() is never
// actually woken by that cancellation and would otherwise hang forever,
// which in turn hangs httptest.Server.Close() in this test's own
// t.Cleanup (it waits for every connection to finish). release lets the
// test explicitly wake any such orphaned handler goroutine once it no
// longer needs it blocked, independent of - and unlike - r.Context().Done().
type crossDestMock struct {
	etag string

	mu       sync.Mutex
	putCount int
	fail     bool
	uploaded map[string][]byte

	blockEnabled bool
	blockOnce    sync.Once
	blockStarted chan struct{}

	releaseOnce sync.Once
	release     chan struct{}
}

func newCrossDestMock() *crossDestMock {
	return &crossDestMock{
		etag:         "crossdestmock-etag",
		uploaded:     map[string][]byte{},
		blockStarted: make(chan struct{}),
		release:      make(chan struct{}),
	}
}

// releaseBlocked wakes every handler goroutine currently blocked in the
// `block` branch below (see release's own doc comment for why this is
// needed in addition to, not instead of, r.Context().Done()). Safe to call
// at most once per crossDestMock (sync.Once) and safe even if nothing is
// currently blocked.
func (m *crossDestMock) releaseBlocked() {
	m.releaseOnce.Do(func() { close(m.release) })
}

func (m *crossDestMock) setFail(fail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.fail = fail
}

func (m *crossDestMock) requestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.putCount
}

func (m *crossDestMock) uploadedContent(key string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	content, ok := m.uploaded[key]

	return content, ok
}

func (m *crossDestMock) handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "crossDestMock: unexpected method "+r.Method, http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	m.putCount++
	fail := m.fail
	block := m.blockEnabled
	m.mu.Unlock()

	if block {
		m.blockOnce.Do(func() { close(m.blockStarted) })

		select {
		case <-r.Context().Done(): // in practice, does not actually fire for a PUT whose body was already fully sent - see release's own doc comment
		case <-m.release:
		}

		return
	}

	body, _ := io.ReadAll(r.Body)

	if fail {
		writeXML(w, http.StatusForbidden, mpuAccessDeniedBody) // reused from multipart_upload_test.go, same package
		return
	}

	key := strings.TrimPrefix(r.URL.Path, "/"+crossDestMockBucket+"/")
	if unescaped, err := url.PathUnescape(key); err == nil {
		key = unescaped
	}

	m.mu.Lock()
	m.uploaded[key] = body
	m.mu.Unlock()

	w.Header().Set("ETag", `"`+m.etag+`"`)
	w.WriteHeader(http.StatusOK)
}

// TestCrossConnectionCopySuccessDownloadsThenUploads drives a full,
// realistic "copy_cross" task (move=false) end to end against two
// independent mock servers (standing in for two different connection
// profiles): the task completes, the destination mock actually received
// the source's exact bytes at the expected key, and - since this is a
// plain copy, not a move - the source's DeleteObject is never called.
func TestCrossConnectionCopySuccessDownloadsThenUploads(t *testing.T) {
	content := []byte("cross-connection copy test content, small enough for a single segment/PUT")

	sourceMock := newCrossSourceMock(content)
	sourceServer := httptest.NewServer(http.HandlerFunc(sourceMock.handler))
	t.Cleanup(sourceServer.Close)

	destMock := newCrossDestMock()
	destServer := httptest.NewServer(http.HandlerFunc(destMock.handler))
	t.Cleanup(destServer.Close)

	deps := newTestTransferService(t)
	sourceProfileID := createTestProfile(t, deps.profileRepo, deps.key, sourceServer.URL)
	destProfileID := createTestProfile(t, deps.profileRepo, deps.key, destServer.URL)

	ids, err := deps.svc.QueueCopyBetweenProfiles(sourceProfileID, destProfileID, crossSourceMockBucket, []string{"success-copy.txt"}, crossDestMockBucket, "dest-prefix/", false)
	if err != nil {
		t.Fatalf("QueueCopyBetweenProfiles() returned error: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("QueueCopyBetweenProfiles() returned %d ids, want 1", len(ids))
	}

	entry := waitForHistoryEntry(t, deps.svc, ids[0], 5*time.Second)
	if entry.Status != "completed" {
		t.Fatalf("history entry status = %q, want %q (error: %s)", entry.Status, "completed", entry.ErrorMessage)
	}

	if entry.TotalBytes != int64(len(content)) {
		t.Errorf("history entry TotalBytes = %d, want %d", entry.TotalBytes, len(content))
	}

	requireNotInQueue(t, deps.svc, ids[0])

	got, ok := destMock.uploadedContent("dest-prefix/success-copy.txt")
	if !ok {
		t.Fatal("destination mock never received a PutObject for \"dest-prefix/success-copy.txt\"")
	}
	if string(got) != string(content) {
		t.Errorf("uploaded content = %q, want %q", got, content)
	}

	if got := sourceMock.deleteRequestCount(); got != 0 {
		t.Errorf("source DeleteObject request count = %d, want 0 (this is a copy, not a move)", got)
	}
}

// TestCrossConnectionMoveDeletesSourceOnlyAfterUploadSucceeds verifies the
// move=true path: once the destination upload has fully succeeded, the
// source object is deleted exactly once from the SOURCE profile.
func TestCrossConnectionMoveDeletesSourceOnlyAfterUploadSucceeds(t *testing.T) {
	content := []byte("cross-connection move test content")

	sourceMock := newCrossSourceMock(content)
	sourceServer := httptest.NewServer(http.HandlerFunc(sourceMock.handler))
	t.Cleanup(sourceServer.Close)

	destMock := newCrossDestMock()
	destServer := httptest.NewServer(http.HandlerFunc(destMock.handler))
	t.Cleanup(destServer.Close)

	deps := newTestTransferService(t)
	sourceProfileID := createTestProfile(t, deps.profileRepo, deps.key, sourceServer.URL)
	destProfileID := createTestProfile(t, deps.profileRepo, deps.key, destServer.URL)

	ids, err := deps.svc.QueueCopyBetweenProfiles(sourceProfileID, destProfileID, crossSourceMockBucket, []string{"move-success.txt"}, crossDestMockBucket, "dest-prefix/", true)
	if err != nil {
		t.Fatalf("QueueCopyBetweenProfiles() returned error: %v", err)
	}

	entry := waitForHistoryEntry(t, deps.svc, ids[0], 5*time.Second)
	if entry.Status != "completed" {
		t.Fatalf("history entry status = %q, want %q (error: %s)", entry.Status, "completed", entry.ErrorMessage)
	}

	got, ok := destMock.uploadedContent("dest-prefix/move-success.txt")
	if !ok {
		t.Fatal("destination mock never received a PutObject for \"dest-prefix/move-success.txt\"")
	}
	if string(got) != string(content) {
		t.Errorf("uploaded content = %q, want %q", got, content)
	}

	if got := sourceMock.deleteRequestCount(); got != 1 {
		t.Errorf("source DeleteObject request count = %d, want 1 (a completed move must delete its source exactly once)", got)
	}
	if sourceMock.deletedKey != "move-success.txt" {
		t.Errorf("deleted source key = %q, want %q", sourceMock.deletedKey, "move-success.txt")
	}
}

// TestCrossConnectionMoveUploadFailureDoesNotDeleteSource verifies that
// when the UPLOAD phase fails (a non-retryable 403 AccessDenied from the
// destination profile's PutObject), the source object is never deleted -
// the move's own copy-then-delete-only-after-confirmed-success ordering
// (runCrossConnectionCopyTask's doc comment): the task ends up "failed",
// still sitting in transfer_queue (never archived, matching every other
// task type's identical failure handling), and the source is left
// completely untouched.
func TestCrossConnectionMoveUploadFailureDoesNotDeleteSource(t *testing.T) {
	content := []byte("cross-connection move test content, upload will fail")

	sourceMock := newCrossSourceMock(content)
	sourceServer := httptest.NewServer(http.HandlerFunc(sourceMock.handler))
	t.Cleanup(sourceServer.Close)

	destMock := newCrossDestMock()
	destMock.setFail(true)
	destServer := httptest.NewServer(http.HandlerFunc(destMock.handler))
	t.Cleanup(destServer.Close)

	deps := newTestTransferService(t)
	sourceProfileID := createTestProfile(t, deps.profileRepo, deps.key, sourceServer.URL)
	destProfileID := createTestProfile(t, deps.profileRepo, deps.key, destServer.URL)

	ids, err := deps.svc.QueueCopyBetweenProfiles(sourceProfileID, destProfileID, crossSourceMockBucket, []string{"move-upload-failure.txt"}, crossDestMockBucket, "dest-prefix/", true)
	if err != nil {
		t.Fatalf("QueueCopyBetweenProfiles() returned error: %v", err)
	}

	task := waitForTaskStatus(t, deps.svc, ids[0], "failed", 5*time.Second)
	if task.ErrorMessage == "" {
		t.Error("failed task's ErrorMessage is empty, want the upload failure recorded")
	}

	requireNotInHistory(t, deps.svc, ids[0])

	if got := destMock.requestCount(); got == 0 {
		t.Error("destination PutObject request count = 0, want at least 1 (the upload phase must actually have been attempted)")
	}

	if got := sourceMock.deleteRequestCount(); got != 0 {
		t.Errorf("source DeleteObject request count = %d, want 0 (a failed upload phase must never delete the source)", got)
	}
}

// TestCrossConnectionCopyPauseDuringUploadThenResumeCompletesWithoutCorruption
// verifies Pause/Resume spanning BOTH of a "copy_cross" task's phases: the
// task is paused genuinely mid-flight during its UPLOAD phase (the
// destination mock's PutObject request is held open, via
// crossDestMock.blockEnabled, until the test cancels it) - which can only
// happen once the DOWNLOAD phase has already fully finished (Upload never
// starts until Download has already returned successfully). On Resume,
// runCrossConnectionCopyTask restarts from the very beginning of runTask,
// so its DOWNLOAD phase runs a SECOND time against the exact same,
// already-fully-downloaded local staging file (crossConnectionTempPath is
// deterministic per taskID - see its own doc comment) - this is the
// specific scenario this test regression-guards: re-running Download() on
// an already-complete temp file must not corrupt it (Download's own
// resume-progress-sidecar mechanism has no sidecar to trust once a
// previous attempt already fully succeeded, so it re-fetches every byte
// from scratch rather than skipping anything - see
// TestDownloadWithoutSidecarRedownloadsEvenIfFileAlreadyComplete's
// identical scenario for the plain "download" task type), and the task
// still ends up completed with byte-for-byte correct content at the
// destination.
func TestCrossConnectionCopyPauseDuringUploadThenResumeCompletesWithoutCorruption(t *testing.T) {
	content := []byte("cross-connection copy content that will be paused mid-upload and resumed")

	sourceMock := newCrossSourceMock(content)
	sourceServer := httptest.NewServer(http.HandlerFunc(sourceMock.handler))
	t.Cleanup(sourceServer.Close)

	destMock := newCrossDestMock()
	destMock.blockEnabled = true
	destServer := httptest.NewServer(http.HandlerFunc(destMock.handler))
	t.Cleanup(destServer.Close)

	deps := newTestTransferService(t)
	sourceProfileID := createTestProfile(t, deps.profileRepo, deps.key, sourceServer.URL)
	destProfileID := createTestProfile(t, deps.profileRepo, deps.key, destServer.URL)

	ids, err := deps.svc.QueueCopyBetweenProfiles(sourceProfileID, destProfileID, crossSourceMockBucket, []string{"pause-resume.txt"}, crossDestMockBucket, "dest-prefix/", false)
	if err != nil {
		t.Fatalf("QueueCopyBetweenProfiles() returned error: %v", err)
	}
	id := ids[0]

	select {
	case <-destMock.blockStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the upload phase's blocking PutObject request to arrive")
	}

	if err := deps.svc.PauseTask(id); err != nil {
		t.Fatalf("PauseTask() returned error: %v", err)
	}

	task := waitForTaskStatus(t, deps.svc, id, "paused", 5*time.Second)
	if task.ID != id {
		t.Fatalf("waitForTaskStatus returned task %+v, want ID %d", task, id)
	}

	requireNotInHistory(t, deps.svc, id)

	firstDownloadCount := sourceMock.getRangeCount()
	if firstDownloadCount == 0 {
		t.Fatal("source GetObject request count = 0 after pausing, want >=1 (the download phase must have already fully completed before the upload phase - and its blocking PutObject - could ever start)")
	}

	// PauseTask already blocked until the paused task's own goroutine
	// fully exited (<-rt.done), so there is no concurrent access to
	// destMock left to race with this write - see PauseTask's doc comment
	// and queueBlockingDownload's identical technique.
	destMock.blockEnabled = false

	// Wakes the FIRST attempt's now-orphaned handler goroutine (see
	// release's own doc comment for why r.Context().Done() alone never
	// does this for a PUT) so its connection closes cleanly instead of
	// leaving httptest.Server.Close() (this test's own t.Cleanup) hanging
	// forever waiting for it once the test function returns.
	destMock.releaseBlocked()

	if err := deps.svc.ResumeTask(id); err != nil {
		t.Fatalf("ResumeTask() returned error: %v", err)
	}

	entry := waitForHistoryEntry(t, deps.svc, id, 5*time.Second)
	if entry.Status != "completed" {
		t.Fatalf("history entry status = %q, want %q (error: %s)", entry.Status, "completed", entry.ErrorMessage)
	}

	if got := sourceMock.getRangeCount(); got <= firstDownloadCount {
		t.Errorf("source GetObject request count after resume = %d, want more than %d (Resume re-runs the download phase from scratch - no resume-progress sidecar survives a previously fully-successful download)", got, firstDownloadCount)
	}

	got, ok := destMock.uploadedContent("dest-prefix/pause-resume.txt")
	if !ok {
		t.Fatal("destination mock never received a successful PutObject for \"dest-prefix/pause-resume.txt\"")
	}
	if string(got) != string(content) {
		t.Errorf("uploaded content after resume = %q, want %q (re-running Download() on an already-complete temp file must not corrupt it)", got, content)
	}
}
