package filemanager

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"threev/internal/domain"
)

func TestFileManagerServiceUpdateMetadataSuccess(t *testing.T) {
	t.Parallel()

	var gotCopySource, gotDirective, gotContentType, gotCacheControl, gotUserMeta string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCopySource = r.Header.Get("X-Amz-Copy-Source")
		gotDirective = r.Header.Get("X-Amz-Metadata-Directive")
		gotContentType = r.Header.Get("Content-Type")
		gotCacheControl = r.Header.Get("Cache-Control")
		gotUserMeta = r.Header.Get("X-Amz-Meta-Owner")

		writeXML(w, http.StatusOK, copyObjectSuccessBody)
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	err := fm.UpdateMetadata(domain.UpdateMetadataRequest{
		ProfileID:    profileID,
		Bucket:       "bucket1",
		Key:          "path/to/file.txt",
		ContentType:  "text/plain",
		CacheControl: "no-cache",
		UserMetadata: map[string]string{"Owner": "alice"},
	})
	if err != nil {
		t.Fatalf("UpdateMetadata() returned error: %v", err)
	}

	wantCopySource := copySourceFor("bucket1", "path/to/file.txt")
	if gotCopySource != wantCopySource {
		t.Errorf("X-Amz-Copy-Source = %q, want %q", gotCopySource, wantCopySource)
	}
	if gotDirective != "REPLACE" {
		t.Errorf("X-Amz-Metadata-Directive = %q, want %q", gotDirective, "REPLACE")
	}
	if gotContentType != "text/plain" {
		t.Errorf("Content-Type = %q, want %q", gotContentType, "text/plain")
	}
	if gotCacheControl != "no-cache" {
		t.Errorf("Cache-Control = %q, want %q", gotCacheControl, "no-cache")
	}
	if gotUserMeta != "alice" {
		t.Errorf("X-Amz-Meta-Owner = %q, want %q", gotUserMeta, "alice")
	}
}

func TestFileManagerServiceUpdateMetadataError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeXML(w, http.StatusForbidden, accessDeniedErrorBody)
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	err := fm.UpdateMetadata(domain.UpdateMetadataRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		Key:       "file.txt",
	})
	if err == nil {
		t.Fatal("UpdateMetadata() returned nil error, want an error (mock always returns AccessDenied)")
	}
}

// TestFileManagerServiceUpdateMetadataReturnsErrLockedWhenLocked verifies
// UpdateMetadata's Этап 4 суб-этап 4.4 guard.
func TestFileManagerServiceUpdateMetadataReturnsErrLockedWhenLocked(t *testing.T) {
	t.Parallel()

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, "http://127.0.0.1:1")

	fm.keyBox.Clear()

	err := fm.UpdateMetadata(domain.UpdateMetadataRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		Key:       "file.txt",
	})
	if !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("UpdateMetadata() on a locked service error = %v, want errors.Is(_, domain.ErrLocked)", err)
	}
}
