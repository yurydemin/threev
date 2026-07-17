package transfer

import (
	"archive/zip"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// zipObjectMock is a minimal S3-compatible mock server implementing just
// what zip_download.go exercises: a single, static ListObjectsV2 page
// (list-type=2, no pagination needed by these tests - QueueDownloadPrefix's
// own tests, queue_paths_test.go's multiObjectMock, already cover
// pagination against the shared listDownloadableKeysUnderPrefix helper) and
// full-object (never Range) GetObject per key, with per-key
// failure/blocking hooks controlled by key name - mirroring
// putObjectMock.setFail (service_test.go) and rangeMock.blockOffset/
// blockEnabled (range_download_test.go)'s own mutex-guarded, toggleable
// designs.
type zipObjectMock struct {
	bucketPrefix string
	listBody     string
	contents     map[string][]byte

	mu        sync.Mutex
	getCounts map[string]int
	failKey   string // GetObject for this key always fails (non-retryable 403 AccessDenied)

	blockKey     string
	blockOnce    sync.Once
	blockStarted chan struct{}
}

// newZipObjectMock returns a *zipObjectMock for "/bucket1/" - the bucket
// name every test in this file queues against, mirroring
// multiObjectMock/rangeMock's own fixed "bucket1" convention elsewhere in
// this package's tests.
func newZipObjectMock() *zipObjectMock {
	return &zipObjectMock{
		bucketPrefix: "/bucket1/",
		contents:     map[string][]byte{},
		getCounts:    map[string]int{},
		blockStarted: make(chan struct{}),
	}
}

func (m *zipObjectMock) setFailKey(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.failKey = key
}

func (m *zipObjectMock) getCount(key string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.getCounts[key]
}

func (m *zipObjectMock) handler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("list-type") == "2" {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(m.listBody))

		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "zipObjectMock: unexpected method "+r.Method, http.StatusBadRequest)
		return
	}

	key := strings.TrimPrefix(r.URL.Path, m.bucketPrefix)
	if unescaped, err := url.PathUnescape(key); err == nil {
		key = unescaped
	}

	m.mu.Lock()
	m.getCounts[key]++
	fail := m.failKey != "" && key == m.failKey
	block := m.blockKey != "" && key == m.blockKey
	m.mu.Unlock()

	if fail {
		writeXML(w, http.StatusForbidden, mpuAccessDeniedBody) // reused from multipart_upload_test.go, same package
		return
	}

	if block {
		m.blockOnce.Do(func() { close(m.blockStarted) })
		<-r.Context().Done() // hang until the client cancels, simulating a mid-archive interruption

		return
	}

	content, ok := m.contents[key]
	if !ok {
		http.Error(w, "zipObjectMock: unknown key "+key, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.Header().Set("ETag", `"zipobjectmock-etag"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

// readZipEntries opens the zip archive at path and returns its entries'
// names mapped to their uncompressed content, failing the test on any
// error - the test-side counterpart to writeZipArchive, verifying the
// archive it produced is actually valid and contains what was expected.
func readZipEntries(t *testing.T, path string) map[string][]byte {
	t.Helper()

	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("zip.OpenReader(%q) returned error: %v", path, err)
	}
	defer func() { _ = reader.Close() }()

	entries := make(map[string][]byte, len(reader.File))

	for _, f := range reader.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open zip entry %q returned error: %v", f.Name, err)
		}

		content := make([]byte, f.UncompressedSize64)
		if _, err := io.ReadFull(rc, content); err != nil {
			_ = rc.Close()
			t.Fatalf("read zip entry %q returned error: %v", f.Name, err)
		}
		_ = rc.Close()

		entries[f.Name] = content
	}

	return entries
}

// requireFileNotExist fails the test if path exists on disk - used to
// verify a failed/cancelled runZipDownloadTask left no stray partial
// archive behind.
func requireFileNotExist(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err == nil {
		t.Errorf("file %q exists, want it removed", path)
	} else if !os.IsNotExist(err) {
		t.Errorf("os.Stat(%q) returned unexpected error: %v", path, err)
	}
}

// TestQueueDownloadPrefixZipHappyPath drives QueueDownloadPrefixZip against
// a 3-key prefix (two siblings, one nested one level deep in "sub/"),
// verifying the resulting task completes and the archive on disk actually
// contains the expected entries/content, and that the completed history
// entry's TotalBytes reflects the summed object sizes computed upfront by
// QueueDownloadPrefixZip's own listing pass.
func TestQueueDownloadPrefixZipHappyPath(t *testing.T) {
	t.Parallel()

	const prefix = "docs/"

	contentA := []byte("content of file-a")
	contentB := []byte("content of file-b, a bit longer than file-a")
	contentC := []byte("content of nested file-c")

	mock := newZipObjectMock()
	mock.contents[prefix+"file-a.txt"] = contentA
	mock.contents[prefix+"file-b.txt"] = contentB
	mock.contents[prefix+"sub/file-c.txt"] = contentC
	mock.listBody = listObjectsPageXML([]struct {
		key  string
		size int64
	}{
		{key: prefix, size: 0}, // zero-byte folder placeholder - must be skipped
		{key: prefix + "file-a.txt", size: int64(len(contentA))},
		{key: prefix + "file-b.txt", size: int64(len(contentB))},
		{key: prefix + "sub/file-c.txt", size: int64(len(contentC))},
	}, "")

	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	deps := newTestTransferService(t)
	profileID := createTestProfile(t, deps.profileRepo, deps.key, server.URL)

	zipPath := filepath.Join(t.TempDir(), "archive.zip")

	id, err := deps.svc.QueueDownloadPrefixZip(profileID, "bucket1", prefix, zipPath)
	if err != nil {
		t.Fatalf("QueueDownloadPrefixZip() returned error: %v", err)
	}
	if id == 0 {
		t.Fatal("QueueDownloadPrefixZip() returned id 0")
	}

	entry := waitForHistoryEntry(t, deps.svc, id, 5*time.Second)
	if entry.Status != "completed" {
		t.Fatalf("history entry status = %q, want %q (error: %s)", entry.Status, "completed", entry.ErrorMessage)
	}

	wantTotal := int64(len(contentA) + len(contentB) + len(contentC))
	if entry.TotalBytes != wantTotal {
		t.Errorf("history entry TotalBytes = %d, want %d", entry.TotalBytes, wantTotal)
	}

	requireNotInQueue(t, deps.svc, id)

	got := readZipEntries(t, zipPath)

	want := map[string][]byte{
		"file-a.txt":     contentA,
		"file-b.txt":     contentB,
		"sub/file-c.txt": contentC,
	}

	if len(got) != len(want) {
		t.Fatalf("archive has %d entries, want %d (entries: %v)", len(got), len(want), entryNames(got))
	}

	for name, wantContent := range want {
		gotContent, ok := got[name]
		if !ok {
			t.Errorf("archive missing entry %q", name)
			continue
		}
		if string(gotContent) != string(wantContent) {
			t.Errorf("archive entry %q content = %q, want %q", name, gotContent, wantContent)
		}
	}
}

func entryNames(m map[string][]byte) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}

	return names
}

// TestZipDownloadMidArchiveObjectFailureRemovesPartialFile verifies that
// when one object (the second of two) fails past its retry budget (a
// non-retryable 403 AccessDenied - chosen so the test does not have to wait
// through s3client.MetadataRetryPolicy's real backoff schedule, mirroring
// putObjectMock/rangeMock's own failure-injection tests), the whole
// "download_zip" task ends up "failed" and the partial .zip file it was
// writing to is removed from disk entirely - never left behind half-written.
func TestZipDownloadMidArchiveObjectFailureRemovesPartialFile(t *testing.T) {
	t.Parallel()

	const prefix = "docs/"

	contentA := []byte("content of file-a, downloaded successfully")
	contentB := []byte("content of file-b, never actually reached")

	mock := newZipObjectMock()
	mock.contents[prefix+"file-a.txt"] = contentA
	mock.contents[prefix+"file-b.txt"] = contentB
	mock.failKey = prefix + "file-b.txt"
	mock.listBody = listObjectsPageXML([]struct {
		key  string
		size int64
	}{
		{key: prefix + "file-a.txt", size: int64(len(contentA))},
		{key: prefix + "file-b.txt", size: int64(len(contentB))},
	}, "")

	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	deps := newTestTransferService(t)
	profileID := createTestProfile(t, deps.profileRepo, deps.key, server.URL)

	zipPath := filepath.Join(t.TempDir(), "archive.zip")

	id, err := deps.svc.QueueDownloadPrefixZip(profileID, "bucket1", prefix, zipPath)
	if err != nil {
		t.Fatalf("QueueDownloadPrefixZip() returned error: %v", err)
	}

	task := waitForTaskStatus(t, deps.svc, id, "failed", 5*time.Second)
	if task.ErrorMessage == "" {
		t.Error("failed task ErrorMessage is empty, want it to identify the failing key")
	}

	requireNotInHistory(t, deps.svc, id)
	requireFileNotExist(t, zipPath)
}

// TestZipDownloadCancelMidArchiveRemovesPartialFile verifies Cancel on a
// genuinely mid-flight "download_zip" task (the first, and only, key's
// GetObject request is held open via mock.blockKey/blockStarted until the
// test cancels it - mirroring queueBlockingDownload's identical technique
// in service_test.go): the task ends up "cancelled" in history, and - per
// this Block's explicit decision that a cancelled ZIP task leaves no stray
// file behind - the partial .zip file does not exist on disk afterward.
func TestZipDownloadCancelMidArchiveRemovesPartialFile(t *testing.T) {
	t.Parallel()

	const prefix = "docs/"

	content := []byte("content that will never finish downloading")

	mock := newZipObjectMock()
	mock.contents[prefix+"file-a.txt"] = content
	mock.blockKey = prefix + "file-a.txt"
	mock.listBody = listObjectsPageXML([]struct {
		key  string
		size int64
	}{
		{key: prefix + "file-a.txt", size: int64(len(content))},
	}, "")

	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	deps := newTestTransferService(t)
	profileID := createTestProfile(t, deps.profileRepo, deps.key, server.URL)

	zipPath := filepath.Join(t.TempDir(), "archive.zip")

	id, err := deps.svc.QueueDownloadPrefixZip(profileID, "bucket1", prefix, zipPath)
	if err != nil {
		t.Fatalf("QueueDownloadPrefixZip() returned error: %v", err)
	}

	select {
	case <-mock.blockStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the blocking GetObject request to arrive")
	}

	if err := deps.svc.CancelTask(id); err != nil {
		t.Fatalf("CancelTask() returned error: %v", err)
	}

	entry := waitForHistoryEntry(t, deps.svc, id, 5*time.Second)
	if entry.Status != "cancelled" {
		t.Errorf("history entry status = %q, want %q", entry.Status, "cancelled")
	}

	requireNotInQueue(t, deps.svc, id)
	requireFileNotExist(t, zipPath)
}

// TestZipDownloadRetryAfterFailureSucceedsWithFullArchive verifies the
// truncate-and-restart-from-scratch behavior RetryTask relies on for
// "download_zip" tasks (see runZipDownloadTask's doc comment): a task that
// failed once (mock.failKey set) is retried after the failure is cleared,
// and ends up with a valid, COMPLETE archive - not a corrupted/partial-
// then-appended one - containing every key.
func TestZipDownloadRetryAfterFailureSucceedsWithFullArchive(t *testing.T) {
	t.Parallel()

	const prefix = "docs/"

	contentA := []byte("content of file-a")
	contentB := []byte("content of file-b")

	mock := newZipObjectMock()
	mock.contents[prefix+"file-a.txt"] = contentA
	mock.contents[prefix+"file-b.txt"] = contentB
	mock.failKey = prefix + "file-b.txt"
	mock.listBody = listObjectsPageXML([]struct {
		key  string
		size int64
	}{
		{key: prefix + "file-a.txt", size: int64(len(contentA))},
		{key: prefix + "file-b.txt", size: int64(len(contentB))},
	}, "")

	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	deps := newTestTransferService(t)
	profileID := createTestProfile(t, deps.profileRepo, deps.key, server.URL)

	zipPath := filepath.Join(t.TempDir(), "archive.zip")

	id, err := deps.svc.QueueDownloadPrefixZip(profileID, "bucket1", prefix, zipPath)
	if err != nil {
		t.Fatalf("QueueDownloadPrefixZip() returned error: %v", err)
	}

	waitForTaskStatus(t, deps.svc, id, "failed", 5*time.Second)
	requireFileNotExist(t, zipPath)

	mock.setFailKey("")

	retryID, err := deps.svc.RetryTask(id)
	if err != nil {
		t.Fatalf("RetryTask() returned error: %v", err)
	}
	if retryID != id {
		t.Errorf("RetryTask() returned id %d, want the same id %d", retryID, id)
	}

	entry := waitForHistoryEntry(t, deps.svc, id, 5*time.Second)
	if entry.Status != "completed" {
		t.Fatalf("history entry status = %q, want %q (error: %s)", entry.Status, "completed", entry.ErrorMessage)
	}

	got := readZipEntries(t, zipPath)

	want := map[string][]byte{
		"file-a.txt": contentA,
		"file-b.txt": contentB,
	}

	if len(got) != len(want) {
		t.Fatalf("archive has %d entries, want %d (entries: %v)", len(got), len(want), entryNames(got))
	}

	for name, wantContent := range want {
		gotContent, ok := got[name]
		if !ok {
			t.Errorf("archive missing entry %q", name)
			continue
		}
		if string(gotContent) != string(wantContent) {
			t.Errorf("archive entry %q content = %q, want %q", name, gotContent, wantContent)
		}
	}

	// The retry re-lists and re-fetches every key from scratch (per
	// runZipDownloadTask's doc comment: os.Create always truncates, there
	// is no partial-archive resume) - file-a.txt, which already succeeded
	// once on the failed first attempt, must have been fetched again by the
	// retry rather than somehow reused/skipped.
	if got := mock.getCount(prefix + "file-a.txt"); got != 2 {
		t.Errorf("GetObject request count for %s = %d, want 2 (once on the failed attempt, once on retry)", prefix+"file-a.txt", got)
	}
}

// TestPauseRejectedForZipDownloadTask verifies PauseTask's dedicated
// "download_zip" guard (service.go): called against a still-"pending" zip
// task (kept from ever running by occupying both concurrency slots with
// blocking downloads, mirroring TestCancelPendingTaskArchivesDirectlyWithoutRunning's
// technique in service_test.go), it returns a non-nil error and leaves the
// task's status unchanged.
func TestPauseRejectedForZipDownloadTask(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	if DefaultMaxConcurrentTasks != 2 {
		t.Fatalf("this test assumes DefaultMaxConcurrentTasks == 2, got %d - update the number of occupier tasks below", DefaultMaxConcurrentTasks)
	}

	occupier1ID, _ := queueBlockingDownload(t, deps, "occupier1")
	occupier2ID, _ := queueBlockingDownload(t, deps, "occupier2")

	const prefix = "docs/"

	content := []byte("content of file-a")

	mock := newZipObjectMock()
	mock.contents[prefix+"file-a.txt"] = content
	mock.listBody = listObjectsPageXML([]struct {
		key  string
		size int64
	}{
		{key: prefix + "file-a.txt", size: int64(len(content))},
	}, "")

	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	profileID := createTestProfile(t, deps.profileRepo, deps.key, server.URL)
	zipPath := filepath.Join(t.TempDir(), "archive.zip")

	id, err := deps.svc.QueueDownloadPrefixZip(profileID, "bucket1", prefix, zipPath)
	if err != nil {
		t.Fatalf("QueueDownloadPrefixZip() returned error: %v", err)
	}

	tasks, err := deps.svc.GetQueue()
	if err != nil {
		t.Fatalf("GetQueue() returned error: %v", err)
	}

	found := false
	for _, task := range tasks {
		if task.ID == id {
			found = true
			if task.Status != "pending" {
				t.Fatalf("task %d status = %q, want %q (both concurrency slots should still be held by the occupier tasks)", id, task.Status, "pending")
			}
		}
	}
	if !found {
		t.Fatalf("task %d not found in GetQueue()", id)
	}

	if err := deps.svc.PauseTask(id); err == nil {
		t.Error("PauseTask() on a download_zip task returned nil error, want a non-nil error")
	}

	tasks, err = deps.svc.GetQueue()
	if err != nil {
		t.Fatalf("GetQueue() returned error: %v", err)
	}

	for _, task := range tasks {
		if task.ID == id && task.Status != "pending" {
			t.Errorf("task %d status = %q after rejected PauseTask, want unchanged %q", id, task.Status, "pending")
		}
	}

	// Clean up the two occupier tasks and this test's own now-pending zip
	// task so their goroutines/mock HTTP requests don't outlive this test.
	if err := deps.svc.CancelTask(occupier1ID); err != nil {
		t.Errorf("CancelTask(occupier1) returned error: %v", err)
	}
	if err := deps.svc.CancelTask(occupier2ID); err != nil {
		t.Errorf("CancelTask(occupier2) returned error: %v", err)
	}
	if err := deps.svc.CancelTask(id); err != nil {
		t.Errorf("CancelTask(zip task) returned error: %v", err)
	}
}

// TestQueueDownloadPrefixZipEmptyPrefixFailsAtRunTimeNotAtQueueTime
// documents and verifies the exact behavior an empty prefix produces (per
// QueueDownloadPrefixZip's own doc comment): encodeBucketKey never fails
// (it is plain string concatenation), so queueing itself succeeds and
// returns a valid task id - but the resulting task then fails, loudly, the
// moment dispatch() runs it and taskBucketKey's splitBucketKey call rejects
// the empty key component ("invalid bucket/key encoding"). Confirms this is
// a graceful failure (task ends up "failed" with a clear error message),
// never a panic.
func TestQueueDownloadPrefixZipEmptyPrefixFailsAtRunTimeNotAtQueueTime(t *testing.T) {
	t.Parallel()

	mock := newZipObjectMock()
	// An empty prefix lists the whole bucket root - give it one (irrelevant)
	// page so QueueDownloadPrefixZip's own upfront listing pass succeeds and
	// actually creates the task.
	mock.contents["file-a.txt"] = []byte("irrelevant")
	mock.listBody = listObjectsPageXML([]struct {
		key  string
		size int64
	}{
		{key: "file-a.txt", size: 10},
	}, "")

	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	deps := newTestTransferService(t)
	profileID := createTestProfile(t, deps.profileRepo, deps.key, server.URL)

	zipPath := filepath.Join(t.TempDir(), "archive.zip")

	id, err := deps.svc.QueueDownloadPrefixZip(profileID, "bucket1", "", zipPath)
	if err != nil {
		t.Fatalf("QueueDownloadPrefixZip(\"\") returned error: %v, want nil (queueing itself must succeed with an empty prefix)", err)
	}
	if id == 0 {
		t.Fatal("QueueDownloadPrefixZip(\"\") returned id 0")
	}

	task := waitForTaskStatus(t, deps.svc, id, "failed", 5*time.Second)
	if task.ErrorMessage == "" {
		t.Error("failed task ErrorMessage is empty, want a clear splitBucketKey-originated error")
	}

	requireFileNotExist(t, zipPath)
}
