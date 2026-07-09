package filemanager

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFileManagerServiceHeadObject(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("request method = %q, want HEAD", r.Method)
		}

		w.Header().Set("Content-Length", "1234")
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("ETag", `"abc123"`)
		w.Header().Set("Last-Modified", "Mon, 01 Jan 2024 00:00:00 GMT")
		w.Header().Set("x-amz-meta-owner", "alice")
		w.Header().Set("x-amz-meta-project", "threev")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	meta, err := fm.HeadObject(profileID, "bucket1", "path/to/file.txt")
	if err != nil {
		t.Fatalf("HeadObject() returned error: %v", err)
	}

	if meta.Key != "path/to/file.txt" {
		t.Errorf("meta.Key = %q, want %q", meta.Key, "path/to/file.txt")
	}
	if meta.Size != 1234 {
		t.Errorf("meta.Size = %d, want 1234", meta.Size)
	}
	if meta.ContentType != "text/plain" {
		t.Errorf("meta.ContentType = %q, want %q", meta.ContentType, "text/plain")
	}
	if meta.ETag != "abc123" {
		t.Errorf("meta.ETag = %q, want %q (unquoted)", meta.ETag, "abc123")
	}
	if meta.LastModified.IsZero() {
		t.Error("meta.LastModified is zero, want populated")
	}
	if meta.Metadata["owner"] != "alice" {
		t.Errorf("meta.Metadata[%q] = %q, want %q", "owner", meta.Metadata["owner"], "alice")
	}
	if meta.Metadata["project"] != "threev" {
		t.Errorf("meta.Metadata[%q] = %q, want %q", "project", meta.Metadata["project"], "threev")
	}
}

func TestFileManagerServiceHeadObjectFallsBackToExtensionContentType(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "10")
		// Deliberately no Content-Type header: some servers do not return
		// one for objects uploaded without an explicit MIME type.
		w.Header().Set("ETag", `"noquotes-should-still-work"`)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	meta, err := fm.HeadObject(profileID, "bucket1", "notes.md")
	if err != nil {
		t.Fatalf("HeadObject() returned error: %v", err)
	}

	if meta.ContentType != "text/markdown" {
		t.Errorf("meta.ContentType = %q, want %q (extension fallback)", meta.ContentType, "text/markdown")
	}
}

func TestFileManagerServiceHeadObjectNotFoundError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// HEAD responses carry no body per HTTP semantics; the SDK derives
		// the error purely from the status code.
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	_, err := fm.HeadObject(profileID, "bucket1", "missing.txt")
	if err == nil {
		t.Fatal("HeadObject() returned nil error, want an error for a 404 response")
	}
	if !strings.Contains(err.Error(), "head object") {
		t.Errorf("error = %q, want it to mention the operation (%q)", err.Error(), "head object")
	}
	if !strings.Contains(err.Error(), "not-found") {
		t.Errorf("error = %q, want it classified as not-found", err.Error())
	}
}
