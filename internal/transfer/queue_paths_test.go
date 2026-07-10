package transfer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"threev/internal/domain"
)

// TestQueueUploadPathsSingleFile verifies the plain-file branch of
// QueueUploadPaths: a bare file path (not a directory) is queued under
// destinationPrefix + filepath.Base(localPath), with no directory
// structure involved.
func TestQueueUploadPathsSingleFile(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	mock := &putObjectMock{etag: "11111111111111111111111111111111"}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	profileID := createTestProfile(t, deps.profileRepo, deps.key, server.URL)
	localPath := createSparseFile(t, 512)

	ids, err := deps.svc.QueueUploadPaths(profileID, "bucket1", "uploads/", []string{localPath})
	if err != nil {
		t.Fatalf("QueueUploadPaths() returned error: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("QueueUploadPaths() returned %d ids, want 1", len(ids))
	}

	entry := waitForHistoryEntry(t, deps.svc, ids[0], 5*time.Second)
	if entry.Status != "completed" {
		t.Errorf("history entry status = %q, want %q", entry.Status, "completed")
	}

	wantKey := "uploads/" + filepath.Base(localPath)
	if entry.DestinationPath != encodeBucketKey("bucket1", wantKey) {
		t.Errorf("history entry DestinationPath = %q, want %q", entry.DestinationPath, encodeBucketKey("bucket1", wantKey))
	}
}

// TestQueueUploadPathsDirectory verifies the directory branch of
// QueueUploadPaths: every regular file nested (at any depth) under a
// directory path is queued, keyed by destinationPrefix + the file's path
// relative to the directory's OWN PARENT - i.e. including the directory's
// name itself as the first key segment.
func TestQueueUploadPathsDirectory(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	mock := &putObjectMock{etag: "22222222222222222222222222222222"}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	profileID := createTestProfile(t, deps.profileRepo, deps.key, server.URL)

	root := t.TempDir()
	dirPath := filepath.Join(root, "mydir")
	if err := os.MkdirAll(filepath.Join(dirPath, "sub"), 0o755); err != nil {
		t.Fatalf("MkdirAll() returned error: %v", err)
	}

	file1 := filepath.Join(dirPath, "file1.txt")
	file2 := filepath.Join(dirPath, "sub", "file2.txt")
	if err := os.WriteFile(file1, []byte("content one"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) returned error: %v", file1, err)
	}
	if err := os.WriteFile(file2, []byte("content two"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) returned error: %v", file2, err)
	}

	ids, err := deps.svc.QueueUploadPaths(profileID, "bucket1", "dest", []string{dirPath})
	if err != nil {
		t.Fatalf("QueueUploadPaths() returned error: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("QueueUploadPaths() returned %d ids, want 2", len(ids))
	}

	wantKeys := map[string]bool{
		"dest/mydir/file1.txt":     false,
		"dest/mydir/sub/file2.txt": false,
	}

	for _, id := range ids {
		entry := waitForHistoryEntry(t, deps.svc, id, 5*time.Second)
		if entry.Status != "completed" {
			t.Errorf("history entry (id %d) status = %q, want %q", id, entry.Status, "completed")
		}

		_, bucketKey, splitErr := splitBucketKey(entry.DestinationPath)
		if splitErr != nil {
			t.Fatalf("splitBucketKey(%q) returned error: %v", entry.DestinationPath, splitErr)
		}

		if _, ok := wantKeys[bucketKey]; !ok {
			t.Errorf("unexpected uploaded key %q", bucketKey)
			continue
		}

		wantKeys[bucketKey] = true
	}

	for key, seen := range wantKeys {
		if !seen {
			t.Errorf("expected key %q was never uploaded", key)
		}
	}

	if got := mock.requestCount(); got != 2 {
		t.Errorf("PutObject request count = %d, want 2", got)
	}
}

// TestQueueUploadPathsPartialFailureIsBestEffort verifies that one
// unreadable localPaths entry (a path that does not exist) does not prevent
// the other, valid entries from being queued - QueueUploadPaths' documented
// best-effort semantics.
func TestQueueUploadPathsPartialFailureIsBestEffort(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	mock := &putObjectMock{etag: "33333333333333333333333333333333"}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	profileID := createTestProfile(t, deps.profileRepo, deps.key, server.URL)
	localPath := createSparseFile(t, 256)

	missingPath := filepath.Join(t.TempDir(), "does-not-exist.bin")

	ids, err := deps.svc.QueueUploadPaths(profileID, "bucket1", "up/", []string{missingPath, localPath})
	if err != nil {
		t.Fatalf("QueueUploadPaths() returned error: %v, want nil (one of two paths was valid)", err)
	}
	if len(ids) != 1 {
		t.Fatalf("QueueUploadPaths() returned %d ids, want 1", len(ids))
	}

	waitForHistoryEntry(t, deps.svc, ids[0], 5*time.Second)
}

// TestQueueUploadPathsAllFailuresReturnsError verifies that when nothing at
// all could be queued (every localPaths entry unreadable, including the
// degenerate empty-slice case), QueueUploadPaths returns a non-nil error
// rather than a silent, empty, unexplained result.
func TestQueueUploadPathsAllFailuresReturnsError(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	profileID := createTestProfile(t, deps.profileRepo, deps.key, "http://127.0.0.1:1") // never contacted

	missingPath := filepath.Join(t.TempDir(), "does-not-exist.bin")

	if ids, err := deps.svc.QueueUploadPaths(profileID, "bucket1", "up/", []string{missingPath}); err == nil {
		t.Errorf("QueueUploadPaths() returned ids %v, err nil, want a non-nil error", ids)
	}

	if ids, err := deps.svc.QueueUploadPaths(profileID, "bucket1", "up/", nil); err == nil {
		t.Errorf("QueueUploadPaths(nil) returned ids %v, err nil, want a non-nil error", ids)
	}
}

// TestNormalizeS3Prefix is a small table-driven unit test of
// normalizeS3Prefix.
func TestNormalizeS3Prefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"foo", "foo/"},
		{"foo/", "foo/"},
		{"/foo", "foo/"},
		{"/foo/bar", "foo/bar/"},
		{"/foo/bar/", "foo/bar/"},
	}

	for _, tt := range tests {
		if got := normalizeS3Prefix(tt.in); got != tt.want {
			t.Errorf("normalizeS3Prefix(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// multiObjectMock is a minimal S3-compatible mock server implementing just
// enough of ListObjectsV2 (paginated, delimiter-less), HeadObject, and
// range GetObject for QueueDownloadPrefix's own tests: a fixed, static set
// of ListObjectsV2 pages keyed by the continuation-token that requests
// them, plus per-key downloadable content for everything downloadRange
// (range_download.go) subsequently HEADs/GETs.
type multiObjectMock struct {
	// listPages maps the continuation-token a ListObjectsV2 request carries
	// ("" for the first page) to the raw XML body to respond with.
	listPages map[string]string

	// contents maps an object key (as it appears in the S3 request path,
	// under bucketPrefix) to its full downloadable body.
	contents map[string][]byte

	// bucketPrefix is the leading URL path segment (e.g. "/bucket1/") every
	// HeadObject/GetObject request's path is expected to start with -
	// stripped before looking a key up in contents.
	bucketPrefix string
}

func (m *multiObjectMock) handler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("list-type") == "2" {
		m.handleList(w, r)
		return
	}

	key := strings.TrimPrefix(r.URL.Path, m.bucketPrefix)
	if unescaped, err := url.PathUnescape(key); err == nil {
		key = unescaped
	}

	content, ok := m.contents[key]
	if !ok {
		http.Error(w, "multiObjectMock: unknown key "+key, http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodHead:
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.Header().Set("ETag", `"multiobjectmock-etag"`)
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		m.handleGet(w, r, content)
	default:
		http.Error(w, "multiObjectMock: unexpected method "+r.Method, http.StatusBadRequest)
	}
}

func (m *multiObjectMock) handleGet(w http.ResponseWriter, r *http.Request, content []byte) {
	rangeHeader := r.Header.Get("Range")

	start, end, err := parseTestRangeHeader(rangeHeader, int64(len(content)))
	if err != nil {
		http.Error(w, "multiObjectMock: bad Range header "+rangeHeader+": "+err.Error(), http.StatusBadRequest)
		return
	}

	body := content[start : end+1]

	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
	w.Header().Set("ETag", `"multiobjectmock-etag"`)
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(body)
}

func (m *multiObjectMock) handleList(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("continuation-token")

	body, ok := m.listPages[token]
	if !ok {
		http.Error(w, "multiObjectMock: unexpected continuation-token "+token, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

// listObjectsPageXML builds one ListObjectsV2 XML response body, listing
// entries and, if nextToken is non-empty, marking the page as truncated
// with that NextContinuationToken.
func listObjectsPageXML(entries []struct {
	key  string
	size int64
}, nextToken string) string {
	var sb strings.Builder

	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	sb.WriteString(`<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">` + "\n")
	sb.WriteString(`  <Name>bucket1</Name>` + "\n")

	if nextToken != "" {
		sb.WriteString("  <IsTruncated>true</IsTruncated>\n")
		sb.WriteString("  <NextContinuationToken>" + nextToken + "</NextContinuationToken>\n")
	} else {
		sb.WriteString("  <IsTruncated>false</IsTruncated>\n")
	}

	for _, e := range entries {
		sb.WriteString("  <Contents>\n")
		sb.WriteString("    <Key>" + e.key + "</Key>\n")
		sb.WriteString("    <LastModified>2024-01-01T00:00:00.000Z</LastModified>\n")
		sb.WriteString(`    <ETag>"multiobjectmock-etag"</ETag>` + "\n")
		fmt.Fprintf(&sb, "    <Size>%d</Size>\n", e.size)
		sb.WriteString("    <StorageClass>STANDARD</StorageClass>\n")
		sb.WriteString("  </Contents>\n")
	}

	sb.WriteString(`</ListBucketResult>`)

	return sb.String()
}

// TestQueueDownloadPrefixPaginatesAndSanitizesKeys drives QueueDownloadPrefix
// against a 2-page ListObjectsV2 mock, verifying: full pagination
// (continuation-token handling), the zero-byte folder-placeholder key being
// skipped, a directory-traversal key (".." in its path relative to prefix)
// being rejected rather than downloaded, and every remaining object ending
// up on disk at the expected mirrored path with the right content.
func TestQueueDownloadPrefixPaginatesAndSanitizesKeys(t *testing.T) {
	t.Parallel()

	const prefix = "myprefix/"

	contentA := []byte("content of file-a, downloaded via page 1")
	contentB := []byte("content of file-b, downloaded via page 2")

	page1 := listObjectsPageXML([]struct {
		key  string
		size int64
	}{
		{key: prefix, size: 0}, // zero-byte folder placeholder - must be skipped
		{key: prefix + "file-a.txt", size: int64(len(contentA))},
	}, "page-2-token")

	page2 := listObjectsPageXML([]struct {
		key  string
		size int64
	}{
		{key: prefix + "subdir/file-b.txt", size: int64(len(contentB))},
		{key: prefix + "../../evil.txt", size: 5}, // directory traversal attempt - must be rejected
	}, "")

	mock := &multiObjectMock{
		bucketPrefix: "/bucket1/",
		listPages: map[string]string{
			"":             page1,
			"page-2-token": page2,
		},
		contents: map[string][]byte{
			prefix + "file-a.txt":        contentA,
			prefix + "subdir/file-b.txt": contentB,
		},
	}

	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	deps := newTestTransferService(t)
	profileID := createTestProfile(t, deps.profileRepo, deps.key, server.URL)

	destDir := t.TempDir()

	ids, err := deps.svc.QueueDownloadPrefix(profileID, "bucket1", prefix, destDir)
	if err != nil {
		t.Fatalf("QueueDownloadPrefix() returned error: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("QueueDownloadPrefix() returned %d ids, want 2 (want the placeholder and the traversal key rejected)", len(ids))
	}

	for _, id := range ids {
		entry := waitForHistoryEntry(t, deps.svc, id, 5*time.Second)
		if entry.Status != "completed" {
			t.Errorf("history entry (id %d) status = %q, want %q (error: %s)", id, entry.Status, "completed", entry.ErrorMessage)
		}
	}

	wantFileA := filepath.Join(destDir, "file-a.txt")
	gotA, err := os.ReadFile(wantFileA)
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error: %v", wantFileA, err)
	}
	if string(gotA) != string(contentA) {
		t.Errorf("file-a.txt content = %q, want %q", gotA, contentA)
	}

	wantFileB := filepath.Join(destDir, "subdir", "file-b.txt")
	gotB, err := os.ReadFile(wantFileB)
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error: %v", wantFileB, err)
	}
	if string(gotB) != string(contentB) {
		t.Errorf("subdir/file-b.txt content = %q, want %q", gotB, contentB)
	}

	// The traversal key must never have escaped destDir - nothing should
	// exist immediately outside it under the name "evil.txt".
	escapedPath := filepath.Join(filepath.Dir(filepath.Dir(destDir)), "evil.txt")
	if _, statErr := os.Stat(escapedPath); statErr == nil {
		t.Errorf("traversal key was written to %q, want it rejected entirely", escapedPath)
	}
}

// TestQueueDownloadPrefixNothingQueuedReturnsError verifies that when every
// listed key is rejected/skipped (so nothing at all could be queued),
// QueueDownloadPrefix returns a non-nil error.
func TestQueueDownloadPrefixNothingQueuedReturnsError(t *testing.T) {
	t.Parallel()

	const prefix = "empty/"

	page1 := listObjectsPageXML([]struct {
		key  string
		size int64
	}{
		{key: prefix, size: 0}, // only a folder placeholder - nothing downloadable
	}, "")

	mock := &multiObjectMock{
		bucketPrefix: "/bucket1/",
		listPages:    map[string]string{"": page1},
		contents:     map[string][]byte{},
	}

	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	deps := newTestTransferService(t)
	profileID := createTestProfile(t, deps.profileRepo, deps.key, server.URL)

	destDir := t.TempDir()

	ids, err := deps.svc.QueueDownloadPrefix(profileID, "bucket1", prefix, destDir)
	if err == nil {
		t.Errorf("QueueDownloadPrefix() returned ids %v, err nil, want a non-nil error", ids)
	}
}

// TestRecoverOrphanedTasksResetsRunningToPaused directly exercises the
// crash-recovery scenario RecoverOrphanedTasks exists for: a transfer_queue
// row left "running" (inserted here directly via queueRepo.Create,
// bypassing QueueUpload/dispatch entirely, simulating exactly what a
// process kill mid-transfer would leave behind - see runTask's own
// UpdateStatus(..., "running", ...) call, which has no matching "finally"
// if the process itself dies before runTask's own deferred cleanup ever
// runs) must be reset to "paused" - and, since RecoverOrphanedTasks never
// calls dispatch(), must NOT be picked up and actually run afterward.
func TestRecoverOrphanedTasksResetsRunningToPaused(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	profileID := createTestProfile(t, deps.profileRepo, deps.key, "http://127.0.0.1:1") // never contacted

	ctx := context.Background()

	created, err := deps.svc.queueRepo.Create(ctx, domain.TransferTask{
		ProfileID:       profileID,
		Type:            "upload",
		SourcePath:      "/tmp/orphaned-source",
		DestinationPath: "bucket1/orphaned-key",
		Status:          "running",
	})
	if err != nil {
		t.Fatalf("queueRepo.Create() returned error: %v", err)
	}

	recovered, err := deps.svc.RecoverOrphanedTasks()
	if err != nil {
		t.Fatalf("RecoverOrphanedTasks() returned error: %v", err)
	}
	if len(recovered) != 1 || recovered[0] != created.ID {
		t.Errorf("RecoverOrphanedTasks() returned ids %v, want [%d]", recovered, created.ID)
	}

	task, err := deps.svc.queueRepo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("queueRepo.GetByID() returned error: %v", err)
	}
	if task.Status != "paused" {
		t.Errorf("task status = %q, want %q", task.Status, "paused")
	}

	// RecoverOrphanedTasks must not itself have started any goroutine for
	// this task - give any errant dispatch a moment to (wrongly) pick it up
	// before asserting it is still sitting untouched as "paused".
	time.Sleep(50 * time.Millisecond)

	deps.svc.mu.Lock()
	_, running := deps.svc.running[created.ID]
	deps.svc.mu.Unlock()

	if running {
		t.Error("task is present in TransferService.running, want it left untouched (never started) by RecoverOrphanedTasks")
	}

	task, err = deps.svc.queueRepo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("queueRepo.GetByID() returned error: %v", err)
	}
	if task.Status != "paused" {
		t.Errorf("task status after settling = %q, want %q (still not picked up)", task.Status, "paused")
	}
}

// TestRecoverOrphanedTasksLeavesOtherStatusesAlone verifies
// RecoverOrphanedTasks only ever touches "running" rows, leaving every
// other status (here, "pending") untouched.
func TestRecoverOrphanedTasksLeavesOtherStatusesAlone(t *testing.T) {
	t.Parallel()

	deps := newTestTransferService(t)

	profileID := createTestProfile(t, deps.profileRepo, deps.key, "http://127.0.0.1:1") // never contacted

	ctx := context.Background()

	created, err := deps.svc.queueRepo.Create(ctx, domain.TransferTask{
		ProfileID:       profileID,
		Type:            "upload",
		SourcePath:      "/tmp/pending-source",
		DestinationPath: "bucket1/pending-key",
		Status:          "pending",
	})
	if err != nil {
		t.Fatalf("queueRepo.Create() returned error: %v", err)
	}

	recovered, err := deps.svc.RecoverOrphanedTasks()
	if err != nil {
		t.Fatalf("RecoverOrphanedTasks() returned error: %v", err)
	}
	if len(recovered) != 0 {
		t.Errorf("RecoverOrphanedTasks() returned ids %v, want none (only \"running\" rows are ever recovered)", recovered)
	}

	task, err := deps.svc.queueRepo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("queueRepo.GetByID() returned error: %v", err)
	}
	if task.Status != "pending" {
		t.Errorf("task status = %q, want %q (RecoverOrphanedTasks must only touch \"running\" rows)", task.Status, "pending")
	}
}
