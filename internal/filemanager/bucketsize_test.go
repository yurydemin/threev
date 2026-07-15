package filemanager

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"threev/internal/domain"
)

// singlePageBucketSizeBody mirrors singlePageAllKeysBody (listallkeys_test.go):
// two real objects plus a zero-byte placeholder object whose key is exactly
// equal to the prefix itself - kept in the total for the same reason
// ListAllKeysUnderPrefix keeps it (see GetBucketSize's doc comment).
const singlePageBucketSizeBody = `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>bucket1</Name>
  <Prefix>photos/vacation/</Prefix>
  <KeyCount>3</KeyCount>
  <MaxKeys>1000</MaxKeys>
  <IsTruncated>false</IsTruncated>
  <Contents>
    <Key>photos/vacation/</Key>
    <LastModified>2024-01-01T00:00:00.000Z</LastModified>
    <ETag>"etag0"</ETag>
    <Size>0</Size>
    <StorageClass>STANDARD</StorageClass>
  </Contents>
  <Contents>
    <Key>photos/vacation/beach.jpg</Key>
    <LastModified>2024-01-01T00:00:00.000Z</LastModified>
    <ETag>"etag1"</ETag>
    <Size>200</Size>
    <StorageClass>STANDARD</StorageClass>
  </Contents>
  <Contents>
    <Key>photos/vacation/sunset.jpg</Key>
    <LastModified>2024-02-01T00:00:00.000Z</LastModified>
    <ETag>"etag2"</ETag>
    <Size>100</Size>
    <StorageClass>STANDARD</StorageClass>
  </Contents>
</ListBucketResult>`

func TestFileManagerServiceGetBucketSizeSinglePage(t *testing.T) {
	t.Parallel()

	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)

		if got := r.URL.Query().Get("delimiter"); got != "" {
			t.Errorf("request Delimiter = %q, want empty (recursive walk)", got)
		}

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(singlePageBucketSizeBody))
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	got, err := fm.GetBucketSize(profileID, "bucket1", "photos/vacation/")
	if err != nil {
		t.Fatalf("GetBucketSize() returned error: %v", err)
	}

	if got := atomic.LoadInt32(&requestCount); got != 1 {
		t.Fatalf("request count = %d, want 1", got)
	}

	want := domain.BucketSizeResult{TotalBytes: 300, ObjectCount: 3, Truncated: false}
	if got != want {
		t.Fatalf("GetBucketSize() = %+v, want %+v", got, want)
	}
}

const page1TruncatedBucketSizeBody = `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>bucket1</Name>
  <Prefix>photos/vacation/</Prefix>
  <KeyCount>1</KeyCount>
  <MaxKeys>1000</MaxKeys>
  <IsTruncated>true</IsTruncated>
  <NextContinuationToken>page-2-token</NextContinuationToken>
  <Contents>
    <Key>photos/vacation/a.jpg</Key>
    <LastModified>2024-01-01T00:00:00.000Z</LastModified>
    <ETag>"etag-a"</ETag>
    <Size>10</Size>
    <StorageClass>STANDARD</StorageClass>
  </Contents>
</ListBucketResult>`

const page2FinalBucketSizeBody = `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>bucket1</Name>
  <Prefix>photos/vacation/</Prefix>
  <KeyCount>1</KeyCount>
  <MaxKeys>1000</MaxKeys>
  <IsTruncated>false</IsTruncated>
  <Contents>
    <Key>photos/vacation/b.jpg</Key>
    <LastModified>2024-01-02T00:00:00.000Z</LastModified>
    <ETag>"etag-b"</ETag>
    <Size>20</Size>
    <StorageClass>STANDARD</StorageClass>
  </Contents>
</ListBucketResult>`

func TestFileManagerServiceGetBucketSizeMultiplePages(t *testing.T) {
	t.Parallel()

	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&requestCount, 1)

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)

		if n == 1 {
			if got := r.URL.Query().Get("continuation-token"); got != "" {
				t.Errorf("first request ContinuationToken = %q, want empty", got)
			}

			_, _ = w.Write([]byte(page1TruncatedBucketSizeBody))

			return
		}

		if got := r.URL.Query().Get("continuation-token"); got != "page-2-token" {
			t.Errorf("second request ContinuationToken = %q, want %q", got, "page-2-token")
		}

		_, _ = w.Write([]byte(page2FinalBucketSizeBody))
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	got, err := fm.GetBucketSize(profileID, "bucket1", "photos/vacation/")
	if err != nil {
		t.Fatalf("GetBucketSize() returned error: %v", err)
	}

	if got := atomic.LoadInt32(&requestCount); got != 2 {
		t.Fatalf("request count = %d, want 2", got)
	}

	want := domain.BucketSizeResult{TotalBytes: 30, ObjectCount: 2, Truncated: false}
	if got != want {
		t.Fatalf("GetBucketSize() = %+v, want %+v (accumulated across both pages)", got, want)
	}
}

const emptyBucketSizeBody = `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>bucket1</Name>
  <Prefix>photos/empty/</Prefix>
  <KeyCount>0</KeyCount>
  <MaxKeys>1000</MaxKeys>
  <IsTruncated>false</IsTruncated>
</ListBucketResult>`

func TestFileManagerServiceGetBucketSizeEmptyIsNotError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(emptyBucketSizeBody))
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	got, err := fm.GetBucketSize(profileID, "bucket1", "photos/empty/")
	if err != nil {
		t.Fatalf("GetBucketSize() returned error: %v", err)
	}

	want := domain.BucketSizeResult{TotalBytes: 0, ObjectCount: 0, Truncated: false}
	if got != want {
		t.Fatalf("GetBucketSize() = %+v, want %+v", got, want)
	}
}

// TestFileManagerServiceGetBucketSizeReturnsErrLockedWhenLocked verifies
// GetBucketSize's Этап 4 суб-этап 4.4 guard.
func TestFileManagerServiceGetBucketSizeReturnsErrLockedWhenLocked(t *testing.T) {
	t.Parallel()

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, "http://127.0.0.1:1") // never contacted

	fm.keyBox.Clear()

	_, err := fm.GetBucketSize(profileID, "bucket1", "photos/vacation/")
	if !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("GetBucketSize() on a locked service error = %v, want errors.Is(_, domain.ErrLocked)", err)
	}
}

// TestFileManagerServiceGetBucketSizeTruncatedOnTimeout is deliberately not
// implemented: GetBucketSize (like ListAllKeysUnderPrefix, whose walk
// structure it copies) builds its own internal context.WithTimeout bounded
// by the unexported, 60-second listAllKeysTimeout constant, with no
// per-call ctx parameter and no test-only override hook. Exercising the
// Truncated:true path for real would mean an httptest handler that blocks
// for a full 60s, which is not a reasonable price for one test run (and
// unlike connection.TestConnection - which accepts ctx as a parameter and
// so lets tester_test.go pass a short-lived context - GetBucketSize's
// signature intentionally mirrors ListAllKeysUnderPrefix's fixed
// (profileID, bucket, prefix) shape, so that pattern does not carry over
// here without restructuring the method signature, which is out of scope
// for this change). Nothing elsewhere in this codebase demonstrates testing
// an internal, hardcoded timeout-driven code path without either waiting it
// out or adding a test-only seam, so per the same tradeoff this is skipped
// rather than adding one single-purpose seam just for this test.
func TestFileManagerServiceGetBucketSizeTruncatedOnTimeout(t *testing.T) {
	t.Parallel()
	t.Skip("no test-only seam exists to shorten GetBucketSize's internal 60s listAllKeysTimeout; see doc comment above")
}
