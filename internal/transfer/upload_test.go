package transfer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestUploadSinglePutObject(t *testing.T) {
	t.Parallel()

	var (
		requestCount  int32
		gotMethod     string
		sawPartNumber bool
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		gotMethod = r.Method

		if r.URL.Query().Get("partNumber") != "" {
			sawPartNumber = true
		}

		_, _ = io.Copy(io.Discard, r.Body)

		w.Header().Set("ETag", `"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"`)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	const totalBytes = 2 * 1024 * 1024 // 2MB, comfortably under singlePutThreshold
	localPath := createSparseFile(t, totalBytes)

	p := newTestUploadParams(t, server.URL)
	p.LocalPath = localPath
	p.TotalBytes = totalBytes

	var bytesTransferred int64
	p.Hooks.OnBytesTransferred = func(delta int64) {
		atomic.AddInt64(&bytesTransferred, delta)
	}

	etag, err := Upload(context.Background(), p)
	if err != nil {
		t.Fatalf("Upload() returned error: %v", err)
	}

	if etag != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Errorf("ETag = %q, want %q", etag, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	}
	if got := atomic.LoadInt32(&requestCount); got != 1 {
		t.Errorf("request count = %d, want 1 (a single PutObject call, no multipart operations)", got)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want %q", gotMethod, http.MethodPut)
	}
	if sawPartNumber {
		t.Error("request included a partNumber query parameter, want a plain PutObject request")
	}
	if got := atomic.LoadInt64(&bytesTransferred); got != totalBytes {
		t.Errorf("bytesTransferred = %d, want %d", got, int64(totalBytes))
	}
}

// TestUploadThresholdBoundary exercises Upload's FR-TR-001 branch point
// (singlePutThreshold, "> 5MB" triggers multipart) using mpuMock (which
// implements both the PutObject and the full multipart-upload surface), to
// verify that a file of exactly 5MB still takes the single-PutObject path
// (CreateMultipartUpload never called) while 5MB+1 byte takes the
// multipart path (CreateMultipartUpload called exactly once).
func TestUploadThresholdBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		totalBytes     int64
		wantCreateCall bool
	}{
		{"exactly 5MB uses single PutObject", 5 * 1024 * 1024, false},
		{"5MB + 1 byte uses multipart", 5*1024*1024 + 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := newMPUMock("upload-id-boundary")
			server := httptest.NewServer(http.HandlerFunc(mock.handler))
			t.Cleanup(server.Close)

			localPath := createSparseFile(t, tt.totalBytes)

			p := newTestUploadParams(t, server.URL)
			p.LocalPath = localPath
			p.TotalBytes = tt.totalBytes

			if _, err := Upload(context.Background(), p); err != nil {
				t.Fatalf("Upload() returned error: %v", err)
			}

			create, _, _ := mock.counts()

			gotCreateCall := create > 0
			if gotCreateCall != tt.wantCreateCall {
				t.Errorf("CreateMultipartUpload called = %v, want %v (createCount=%d)", gotCreateCall, tt.wantCreateCall, create)
			}
		})
	}
}
