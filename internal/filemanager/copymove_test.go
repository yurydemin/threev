package filemanager

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"threev/internal/domain"
)

// copyObjectSuccessBody is a minimal, valid CopyObjectResult response body.
const copyObjectSuccessBody = `<?xml version="1.0" encoding="UTF-8"?>
<CopyObjectResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <ETag>"etag-copy"</ETag>
  <LastModified>2024-01-01T00:00:00.000Z</LastModified>
</CopyObjectResult>`

// copyMock simulates the two S3 endpoints CopyObjects/MoveObjects actually
// call: CopyObject (PUT with an X-Amz-Copy-Source header) and DeleteObject
// (plain DELETE, used only by MoveObjects' copy-then-delete step). It
// records every CopySource header it receives, keyed by request path (the
// destination bucket/key), and every key DeleteObject is called for, in
// arrival order - so a test can assert both "what CopySource string was
// actually sent" (the copySourceFor regression case) and "was DeleteObject
// only ever called for a key whose CopyObject already succeeded" (the
// move-ordering case).
//
// blockAfterN/blockStarted mirror deleteMock's/transfer's rangeMock's own
// cancel-mid-flight technique, adapted for a worker pool rather than a
// single sequential loop: every CopyObject request whose 1-based arrival
// index is > blockAfterN blocks until its context is canceled (not just
// the first one) - with copyMoveWorkerCount (8) concurrent workers, a
// single blocked request would let every OTHER worker simply race through
// the rest of a small key set before the test ever gets a chance to call
// CancelBulkOperation, so every request past the threshold must block for
// a cancel-mid-pool test to be deterministic.
type copyMock struct {
	failCopySourceContains string

	blockAfterN  int
	blockOnce    sync.Once
	blockStarted chan struct{}

	mu                sync.Mutex
	copyCount         int
	copySourceHeaders map[string]string // request path -> X-Amz-Copy-Source header value
	deletedKeys       []string          // request path, in arrival order
}

func newCopyMock() *copyMock {
	return &copyMock{
		blockStarted:      make(chan struct{}),
		copySourceHeaders: make(map[string]string),
	}
}

func newCopyMockServer(t *testing.T, m *copyMock) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(m.handler))
	t.Cleanup(server.Close)

	return server
}

func (m *copyMock) getCopyCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.copyCount
}

func (m *copyMock) getDeletedKeys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]string, len(m.deletedKeys))
	copy(out, m.deletedKeys)

	return out
}

func (m *copyMock) getCopySourceFor(requestPath string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	v, ok := m.copySourceHeaders[requestPath]

	return v, ok
}

func (m *copyMock) handler(w http.ResponseWriter, r *http.Request) {
	copySource := r.Header.Get("X-Amz-Copy-Source")

	switch {
	case r.Method == http.MethodPut && copySource != "":
		m.handleCopy(w, r, copySource)
	case r.Method == http.MethodDelete:
		m.handleDelete(w, r)
	default:
		http.Error(w, "copyMock: unexpected request "+r.Method+" "+r.URL.String(), http.StatusBadRequest)
	}
}

func (m *copyMock) handleCopy(w http.ResponseWriter, r *http.Request, copySource string) {
	m.mu.Lock()
	m.copyCount++
	idx := m.copyCount
	m.copySourceHeaders[r.URL.Path] = copySource
	fail := m.failCopySourceContains != "" && strings.Contains(copySource, m.failCopySourceContains)
	block := m.blockAfterN > 0 && idx > m.blockAfterN
	m.mu.Unlock()

	if fail {
		writeXML(w, http.StatusForbidden, accessDeniedErrorBody)
		return
	}

	if block {
		m.blockOnce.Do(func() { close(m.blockStarted) })
		<-r.Context().Done() // hang until the client cancels, simulating an interrupted in-flight copy

		return
	}

	writeXML(w, http.StatusOK, copyObjectSuccessBody)
}

func (m *copyMock) handleDelete(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	m.deletedKeys = append(m.deletedKeys, r.URL.Path)
	m.mu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

func TestFileManagerServiceCopyObjectsRejectsEmptyKeys(t *testing.T) {
	t.Parallel()

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, "http://127.0.0.1:1") // never contacted

	req := domain.BulkCopyRequest{ProfileID: profileID, SourceBucket: "bucket1", DestBucket: "bucket1", DestPrefix: "archive/"}
	if _, err := fm.CopyObjects(req); err == nil {
		t.Fatal("CopyObjects() with no keys returned nil error, want an error")
	}
}

func TestFileManagerServiceMoveObjectsRejectsEmptyKeys(t *testing.T) {
	t.Parallel()

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, "http://127.0.0.1:1") // never contacted

	req := domain.BulkMoveRequest{ProfileID: profileID, SourceBucket: "bucket1", DestBucket: "bucket1", DestPrefix: "archive/"}
	if _, err := fm.MoveObjects(req); err == nil {
		t.Fatal("MoveObjects() with no keys returned nil error, want an error")
	}
}

// TestFileManagerServiceCopyObjectsSuccessAndCopySourceEscaping is the
// regression test for copySourceFor: sourceKey deliberately contains "/"
// AND a space, so a naive url.QueryEscape(bucket+"/"+key) (which would
// encode the internal "/" as "%2F", breaking S3's own parsing of
// CopySource - see copySourceFor's doc comment) would be caught by
// comparing the mock's actually-received X-Amz-Copy-Source header against
// copySourceFor's own output computed independently in this test.
func TestFileManagerServiceCopyObjectsSuccessAndCopySourceEscaping(t *testing.T) {
	t.Parallel()

	mock := newCopyMock()
	server := newCopyMockServer(t, mock)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	const sourceKey = "folder one/sub folder/report v2.txt"

	opID, err := fm.CopyObjects(domain.BulkCopyRequest{
		ProfileID:    profileID,
		SourceBucket: "src-bucket",
		Keys:         []string{sourceKey},
		DestBucket:   "dest-bucket",
		DestPrefix:   "archive/",
	})
	if err != nil {
		t.Fatalf("CopyObjects() returned error: %v", err)
	}

	waitForBulkOpDone(t, fm, opID)

	if got := mock.getCopyCount(); got != 1 {
		t.Fatalf("mock copy count = %d, want 1", got)
	}

	wantDestKey := "archive/" + path.Base(sourceKey)
	wantPath := "/dest-bucket/" + wantDestKey

	gotCopySource, ok := mock.getCopySourceFor(wantPath)
	if !ok {
		t.Fatalf("mock never received a CopyObject request for path %q; headers seen: %+v", wantPath, mock.copySourceHeaders)
	}

	// CopyObjectInput.CopySource is sent as the X-Amz-Copy-Source header
	// value verbatim - compare byte for byte to copySourceFor's own output,
	// computed independently here.
	wantCopySource := copySourceFor("src-bucket", sourceKey)
	if gotCopySource != wantCopySource {
		t.Errorf("X-Amz-Copy-Source = %q, want %q", gotCopySource, wantCopySource)
	}

	if strings.Contains(gotCopySource, "%2F") {
		t.Errorf("X-Amz-Copy-Source = %q contains %%2F - the source key's internal \"/\" was incorrectly escaped as a literal slash character", gotCopySource)
	}
}

func TestFileManagerServiceCopyObjectsDoesNotDeleteSource(t *testing.T) {
	t.Parallel()

	mock := newCopyMock()
	server := newCopyMockServer(t, mock)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	opID, err := fm.CopyObjects(domain.BulkCopyRequest{
		ProfileID:    profileID,
		SourceBucket: "bucket1",
		Keys:         []string{"a.txt", "b.txt"},
		DestBucket:   "bucket1",
		DestPrefix:   "archive/",
	})
	if err != nil {
		t.Fatalf("CopyObjects() returned error: %v", err)
	}

	waitForBulkOpDone(t, fm, opID)

	if got := mock.getCopyCount(); got != 2 {
		t.Fatalf("mock copy count = %d, want 2", got)
	}
	if deleted := mock.getDeletedKeys(); len(deleted) != 0 {
		t.Errorf("CopyObjects (not Move) triggered DeleteObject for %v, want none", deleted)
	}
}

// TestFileManagerServiceMoveObjectsDeletesSourceOnlyAfterSuccessfulCopy is
// the regression test for copyOneObject's copy-then-delete-that-same-key
// ordering (see its doc comment): one of three keys is configured to fail
// its CopyObject call, and the test asserts DeleteObject was called for
// exactly the two keys whose copy actually succeeded - never for the one
// whose copy failed.
func TestFileManagerServiceMoveObjectsDeletesSourceOnlyAfterSuccessfulCopy(t *testing.T) {
	t.Parallel()

	mock := newCopyMock()
	mock.failCopySourceContains = "fails-to-copy.txt"
	server := newCopyMockServer(t, mock)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	opID, err := fm.MoveObjects(domain.BulkMoveRequest{
		ProfileID:    profileID,
		SourceBucket: "bucket1",
		Keys:         []string{"ok-1.txt", "fails-to-copy.txt", "ok-2.txt"},
		DestBucket:   "bucket1",
		DestPrefix:   "archive/",
	})
	if err != nil {
		t.Fatalf("MoveObjects() returned error: %v", err)
	}

	waitForBulkOpDone(t, fm, opID)

	deleted := mock.getDeletedKeys()
	if len(deleted) != 2 {
		t.Fatalf("deleted keys = %v, want exactly 2 (the two keys whose copy succeeded)", deleted)
	}

	for _, d := range deleted {
		if strings.Contains(d, "fails-to-copy.txt") {
			t.Errorf("DeleteObject was called for %q, whose CopyObject failed - copy-then-delete ordering was violated", d)
		}
	}
}

// TestFileManagerServiceCopyObjectsCancelMidPoolStopsFeedingNewJobs verifies
// CancelBulkOperation against a genuinely in-flight worker pool: the mock
// blocks every CopyObject request past the first 2 (so, with
// copyMoveWorkerCount=8 workers, at most 2 keys can ever fully succeed
// before every remaining/started worker is stuck blocked), the test waits
// for the first blocking request to arrive, cancels, and asserts that the
// total number of CopyObject calls the mock ever received stays well below
// the full key set - proving the job feeder (and idle workers) stopped
// taking new work once ctx was canceled, rather than draining the entire
// key set regardless.
func TestFileManagerServiceCopyObjectsCancelMidPoolStopsFeedingNewJobs(t *testing.T) {
	t.Parallel()

	if copyMoveWorkerCount != 8 {
		t.Fatalf("this test assumes copyMoveWorkerCount == 8, got %d - update blockAfterN below", copyMoveWorkerCount)
	}

	mock := newCopyMock()
	mock.blockAfterN = 2

	server := newCopyMockServer(t, mock)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	total := 500
	keys := make([]string, total)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%04d.txt", i)
	}

	opID, err := fm.CopyObjects(domain.BulkCopyRequest{
		ProfileID:    profileID,
		SourceBucket: "bucket1",
		Keys:         keys,
		DestBucket:   "bucket1",
		DestPrefix:   "archive/",
	})
	if err != nil {
		t.Fatalf("CopyObjects() returned error: %v", err)
	}

	select {
	case <-mock.blockStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a blocking CopyObject request to arrive")
	}

	if err := fm.CancelBulkOperation(opID); err != nil {
		t.Fatalf("CancelBulkOperation() returned error: %v", err)
	}

	// At most: 2 succeeded immediately + copyMoveWorkerCount requests that
	// were in flight (blocked) at the moment of cancellation. Comfortably
	// below total (500) either way - the real point of this assertion is
	// that the worker pool did NOT simply run to completion regardless of
	// cancellation.
	if got := mock.getCopyCount(); got >= total {
		t.Fatalf("mock copy count after cancel = %d, want well below total (%d) - cancellation did not stop the worker pool from draining the whole key set", got, total)
	}
}
