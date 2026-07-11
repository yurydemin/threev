package filemanager

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"threev/internal/domain"
)

func TestFileManagerServiceCreateFolderSuccess(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("ETag", `"folder-etag"`)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	err := fm.CreateFolder(domain.CreateFolderRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		Prefix:    "parent/",
		Name:      "newfolder",
	})
	if err != nil {
		t.Fatalf("CreateFolder() returned error: %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Errorf("request method = %q, want %q", gotMethod, http.MethodPut)
	}

	wantPath := "/bucket1/parent/newfolder/"
	if gotPath != wantPath {
		t.Errorf("request path = %q, want %q", gotPath, wantPath)
	}
}

func TestFileManagerServiceCreateFolderAtBucketRoot(t *testing.T) {
	t.Parallel()

	var gotPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("ETag", `"folder-etag"`)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	err := fm.CreateFolder(domain.CreateFolderRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		Name:      "toplevel",
	})
	if err != nil {
		t.Fatalf("CreateFolder() returned error: %v", err)
	}

	if want := "/bucket1/toplevel/"; gotPath != want {
		t.Errorf("request path = %q, want %q", gotPath, want)
	}
}

func TestFileManagerServiceCreateFolderRejectsEmptyName(t *testing.T) {
	t.Parallel()

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, "http://127.0.0.1:1") // never contacted

	err := fm.CreateFolder(domain.CreateFolderRequest{ProfileID: profileID, Bucket: "bucket1", Name: ""})
	if err == nil {
		t.Fatal("CreateFolder() with an empty name returned nil error, want an error")
	}
}

func TestFileManagerServiceCreateFolderRejectsNameContainingSlash(t *testing.T) {
	t.Parallel()

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, "http://127.0.0.1:1") // never contacted

	err := fm.CreateFolder(domain.CreateFolderRequest{ProfileID: profileID, Bucket: "bucket1", Name: "a/b"})
	if err == nil {
		t.Fatal("CreateFolder() with a name containing \"/\" returned nil error, want an error")
	}
}

// TestFileManagerServiceCreateFolderReturnsErrLockedWhenLocked verifies
// CreateFolder's Этап 4 суб-этап 4.4 guard.
func TestFileManagerServiceCreateFolderReturnsErrLockedWhenLocked(t *testing.T) {
	t.Parallel()

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, "http://127.0.0.1:1") // never contacted

	fm.keyBox.Clear()

	err := fm.CreateFolder(domain.CreateFolderRequest{ProfileID: profileID, Bucket: "bucket1", Name: "newfolder"})
	if !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("CreateFolder() on a locked service error = %v, want errors.Is(_, domain.ErrLocked)", err)
	}
}
