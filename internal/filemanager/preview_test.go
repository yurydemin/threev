package filemanager

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"threev/internal/domain"
)

// smallPreviewContent is well under textPreviewLimitBytes.
const smallPreviewContent = "hello, this is a small text file"

func TestFileManagerServiceGetTextPreviewSmallFileReturnedInFull(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		size := strconv.Itoa(len(smallPreviewContent))

		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", size)
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)

			return
		}

		// GetObject: a small file must not be requested with a Range
		// header at all.
		if rng := r.Header.Get("Range"); rng != "" {
			t.Errorf("GetObject request has Range header %q, want none for a file under the preview limit", rng)
		}

		w.Header().Set("Content-Length", size)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(smallPreviewContent))
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	result, err := fm.GetTextPreview(profileID, "bucket1", "small.txt")
	if err != nil {
		t.Fatalf("GetTextPreview() returned error: %v", err)
	}

	if result.Content != smallPreviewContent {
		t.Errorf("result.Content = %q, want %q", result.Content, smallPreviewContent)
	}
	if result.Truncated {
		t.Error("result.Truncated = true, want false for a file under the preview limit")
	}
	if result.TotalSize != int64(len(smallPreviewContent)) {
		t.Errorf("result.TotalSize = %d, want %d", result.TotalSize, len(smallPreviewContent))
	}
}

func TestFileManagerServiceGetTextPreviewLargeFileTruncatedViaRange(t *testing.T) {
	t.Parallel()

	const totalSize = textPreviewLimitBytes + 50*1024
	fullBody := strings.Repeat("a", totalSize)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(totalSize))
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)

			return
		}

		wantRange := "bytes=0-" + strconv.Itoa(textPreviewLimitBytes-1)
		if got := r.Header.Get("Range"); got != wantRange {
			t.Errorf("GetObject Range header = %q, want %q", got, wantRange)
		}

		// A well-behaved server honors Range and returns only the
		// requested slice with a 206.
		w.Header().Set("Content-Range", "bytes 0-"+strconv.Itoa(textPreviewLimitBytes-1)+"/"+strconv.Itoa(totalSize))
		w.Header().Set("Content-Length", strconv.Itoa(textPreviewLimitBytes))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte(fullBody[:textPreviewLimitBytes]))
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	result, err := fm.GetTextPreview(profileID, "bucket1", "large.txt")
	if err != nil {
		t.Fatalf("GetTextPreview() returned error: %v", err)
	}

	if len(result.Content) != textPreviewLimitBytes {
		t.Errorf("len(result.Content) = %d, want %d", len(result.Content), textPreviewLimitBytes)
	}
	if !result.Truncated {
		t.Error("result.Truncated = false, want true for a file over the preview limit")
	}
	if result.TotalSize != totalSize {
		t.Errorf("result.TotalSize = %d, want %d", result.TotalSize, totalSize)
	}
}

// TestFileManagerServiceGetTextPreviewIgnoresRangeStillBounded simulates an
// S3-compatible server that does not honor the Range header at all and
// always returns the full object body with a plain 200 OK. GetTextPreview
// must still bound Content to textPreviewLimitBytes via defensive
// io.LimitReader rather than trusting the server to have respected Range.
func TestFileManagerServiceGetTextPreviewIgnoresRangeStillBounded(t *testing.T) {
	t.Parallel()

	const totalSize = textPreviewLimitBytes + 50*1024
	fullBody := strings.Repeat("b", totalSize)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(totalSize))
			w.WriteHeader(http.StatusOK)

			return
		}

		// Misbehaving server: ignores the Range header entirely and
		// returns everything with 200 OK.
		w.Header().Set("Content-Length", strconv.Itoa(totalSize))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fullBody))
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	result, err := fm.GetTextPreview(profileID, "bucket1", "large-ignoring-range.txt")
	if err != nil {
		t.Fatalf("GetTextPreview() returned error: %v", err)
	}

	if len(result.Content) > textPreviewLimitBytes {
		t.Fatalf("len(result.Content) = %d, want <= %d even when the server ignores Range", len(result.Content), textPreviewLimitBytes)
	}
	if !result.Truncated {
		t.Error("result.Truncated = false, want true (Size, from HeadObject, exceeds the preview limit)")
	}
}

func TestFileManagerServiceGetTextPreviewNotFoundError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	_, err := fm.GetTextPreview(profileID, "bucket1", "missing.txt")
	if err == nil {
		t.Fatal("GetTextPreview() returned nil error, want an error for a missing object")
	}
	if !strings.Contains(err.Error(), "head object") {
		t.Errorf("error = %q, want it to surface the underlying HeadObject failure (mentioning %q)", err.Error(), "head object")
	}
	if !strings.Contains(err.Error(), "not-found") {
		t.Errorf("error = %q, want it classified as not-found", err.Error())
	}
}

// TestFileManagerServiceGetTextPreviewReturnsErrLockedWhenLocked verifies
// GetTextPreview's Этап 4 суб-этап 4.4 guard - see its own doc comment for
// why this guard is deliberately repeated here even though the first thing
// GetTextPreview calls (f.HeadObject) already carries an identical guard of
// its own.
func TestFileManagerServiceGetTextPreviewReturnsErrLockedWhenLocked(t *testing.T) {
	t.Parallel()

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, "http://127.0.0.1:1")

	fm.keyBox.Clear()

	_, err := fm.GetTextPreview(profileID, "bucket1", "path/to/file.txt")
	if !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("GetTextPreview() on a locked service error = %v, want errors.Is(_, domain.ErrLocked)", err)
	}
}
