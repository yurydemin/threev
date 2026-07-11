package filemanager

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"threev/internal/domain"
)

// renameMock simulates the two S3 endpoints RenameObject calls: CopyObject
// (PUT with an X-Amz-Copy-Source header) and DeleteObject (plain DELETE) -
// the same two endpoints copyMock (copymove_test.go) simulates, but kept as
// its own small type here since RenameObject's mock needs a different knob
// (failDelete, rather than copyMock's failCopySourceContains/blockAfterN
// worker-pool-cancellation machinery, which has no equivalent in a
// synchronous single-object rename).
type renameMock struct {
	failDelete bool

	copyCount   int
	copySource  string
	deleteCount int
	deletedPath string
}

func (m *renameMock) handler(w http.ResponseWriter, r *http.Request) {
	copySource := r.Header.Get("X-Amz-Copy-Source")

	switch {
	case r.Method == http.MethodPut && copySource != "":
		m.copyCount++
		m.copySource = copySource
		writeXML(w, http.StatusOK, copyObjectSuccessBody)
	case r.Method == http.MethodDelete:
		m.deleteCount++
		m.deletedPath = r.URL.Path

		if m.failDelete {
			writeXML(w, http.StatusForbidden, accessDeniedErrorBody)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "renameMock: unexpected request "+r.Method+" "+r.URL.String(), http.StatusBadRequest)
	}
}

func TestFileManagerServiceRenameObjectSuccess(t *testing.T) {
	t.Parallel()

	mock := &renameMock{}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	err := fm.RenameObject(domain.RenameObjectRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		OldKey:    "docs/report.txt",
		NewKey:    "docs/report-final.txt",
	})
	if err != nil {
		t.Fatalf("RenameObject() returned error: %v", err)
	}

	if mock.copyCount != 1 {
		t.Errorf("copy count = %d, want 1", mock.copyCount)
	}

	wantCopySource := copySourceFor("bucket1", "docs/report.txt")
	if mock.copySource != wantCopySource {
		t.Errorf("X-Amz-Copy-Source = %q, want %q", mock.copySource, wantCopySource)
	}

	if mock.deleteCount != 1 {
		t.Errorf("delete count = %d, want 1", mock.deleteCount)
	}

	if want := "/bucket1/docs/report.txt"; mock.deletedPath != want {
		t.Errorf("DeleteObject path = %q, want %q", mock.deletedPath, want)
	}
}

func TestFileManagerServiceRenameObjectRejectsSameKeyWithoutNetworkCalls(t *testing.T) {
	t.Parallel()

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, "http://127.0.0.1:1") // never contacted

	err := fm.RenameObject(domain.RenameObjectRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		OldKey:    "docs/report.txt",
		NewKey:    "docs/report.txt",
	})
	if err == nil {
		t.Fatal("RenameObject() with NewKey == OldKey returned nil error, want an error")
	}
}

func TestFileManagerServiceRenameObjectRejectsEmptyNewKeyWithoutNetworkCalls(t *testing.T) {
	t.Parallel()

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, "http://127.0.0.1:1") // never contacted

	err := fm.RenameObject(domain.RenameObjectRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		OldKey:    "docs/report.txt",
		NewKey:    "",
	})
	if err == nil {
		t.Fatal("RenameObject() with an empty NewKey returned nil error, want an error")
	}
}

// TestFileManagerServiceRenameObjectDeleteFailureAfterSuccessfulCopy is the
// regression test for RenameObject's copy-then-delete ordering/no-rollback
// trade-off (see its doc comment, and copyOneObject's identical rationale in
// copymove.go): CopyObject succeeds, DeleteObject then fails - RenameObject
// must return an error, and the mock's copy already having happened (the
// duplicate is left in place, not undone) is the expected, documented
// outcome, not a bug this test is meant to catch.
func TestFileManagerServiceRenameObjectDeleteFailureAfterSuccessfulCopy(t *testing.T) {
	t.Parallel()

	mock := &renameMock{failDelete: true}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	err := fm.RenameObject(domain.RenameObjectRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		OldKey:    "docs/report.txt",
		NewKey:    "docs/report-final.txt",
	})
	if err == nil {
		t.Fatal("RenameObject() returned nil error when DeleteObject failed, want an error")
	}

	if mock.copyCount != 1 {
		t.Errorf("copy count = %d, want 1 (the copy should have already succeeded before the delete failure)", mock.copyCount)
	}

	if mock.deleteCount != 1 {
		t.Errorf("delete count = %d, want 1", mock.deleteCount)
	}
}

// TestFileManagerServiceRenameObjectReturnsErrLockedWhenLocked verifies
// RenameObject's Этап 4 суб-этап 4.4 guard.
func TestFileManagerServiceRenameObjectReturnsErrLockedWhenLocked(t *testing.T) {
	t.Parallel()

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, "http://127.0.0.1:1") // never contacted

	fm.keyBox.Clear()

	err := fm.RenameObject(domain.RenameObjectRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		OldKey:    "docs/report.txt",
		NewKey:    "docs/report-final.txt",
	})
	if !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("RenameObject() on a locked service error = %v, want errors.Is(_, domain.ErrLocked)", err)
	}
}
