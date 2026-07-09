package transfer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestDownloadCreatesParentDirectory verifies that Download creates
// LocalPath's parent directory itself (os.MkdirAll) before downloadRange
// ever tries to open/create the destination file - LocalPath here points
// into a nested directory that does not exist yet.
func TestDownloadCreatesParentDirectory(t *testing.T) {
	t.Parallel()

	content := randomContent(1024) // well under singlePutThreshold-sized concerns; download.go has no such threshold, this just needs to be a small, single-segment object
	mock := newRangeMock(content)

	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	localPath := filepath.Join(t.TempDir(), "nested", "dir", "downloaded.bin")

	p := newTestDownloadParams(t, server.URL)
	p.LocalPath = localPath

	if _, err := Download(context.Background(), p); err != nil {
		t.Fatalf("Download() returned error: %v", err)
	}

	got, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) returned error: %v", localPath, err)
	}

	if string(got) != string(content) {
		t.Error("downloaded file content does not match the source object's content")
	}
}

// headFailureMock always responds to HeadObject with a 403 and fails the
// test outright if any GetObject request is ever made - Download must
// never attempt to fetch segments once HeadObject itself fails, since
// there is no reliable Content-Length/ETag to plan a download around.
//
// Unlike mpuMock's failPartNumber (multipart_upload_test.go), this cannot
// use an XML AccessDenied body for the classification: HTTP HEAD responses
// never have a body (RFC 9110), so aws-sdk-go-v2's HeadObject error
// deserializer falls back to deriving a pseudo-code from the bare status
// text ("Forbidden") rather than a real API error code. s3client.
// ClassifyError's isAuthStatusCode fallback (added specifically for this
// case, found while building Download) recognizes the raw 401/403 HTTP
// status itself, so this still classifies as "auth" and fails on the first
// attempt - no MetadataRetryPolicy backoff burned retrying credentials that
// were never going to start working.
type headFailureMock struct {
	t *testing.T
}

func (m *headFailureMock) handler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodHead:
		w.WriteHeader(http.StatusForbidden) // HEAD responses never have a body (RFC 9110): no XML error body to write
	case http.MethodGet:
		m.t.Error("GetObject was called after HeadObject failed, want downloadRange to never run")
		http.Error(w, "unexpected GetObject", http.StatusBadRequest)
	default:
		http.Error(w, "headFailureMock: unexpected method "+r.Method, http.StatusBadRequest)
	}
}

func TestDownloadHeadObjectFailureIsFatal(t *testing.T) {
	t.Parallel()

	mock := &headFailureMock{t: t}
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	localPath := filepath.Join(t.TempDir(), "downloaded.bin")

	p := newTestDownloadParams(t, server.URL)
	p.LocalPath = localPath

	_, err := Download(context.Background(), p)
	if err == nil {
		t.Fatal("Download() returned a nil error, want HeadObject's AccessDenied to be fatal")
	}

	if _, statErr := os.Stat(localPath); !os.IsNotExist(statErr) {
		t.Errorf("os.Stat(%q) = (_, %v), want the file to never have been created", localPath, statErr)
	}
}
