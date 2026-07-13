package filemanager

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"threev/internal/domain"
)

// singlePageAllKeysBody describes one un-truncated ListObjectsV2 page under
// prefix "photos/vacation/": two real objects plus a zero-byte placeholder
// object whose key is exactly equal to the prefix itself (the case
// entriesFromPage/ListObjects would drop, but ListAllKeysUnderPrefix must
// keep - see TestFileManagerServiceListAllKeysUnderPrefixIncludesPlaceholderKey).
const singlePageAllKeysBody = `<?xml version="1.0" encoding="UTF-8"?>
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

func TestFileManagerServiceListAllKeysUnderPrefixSinglePage(t *testing.T) {
	t.Parallel()

	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)

		if got := r.URL.Query().Get("delimiter"); got != "" {
			t.Errorf("request Delimiter = %q, want empty (recursive walk)", got)
		}

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(singlePageAllKeysBody))
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	keys, err := fm.ListAllKeysUnderPrefix(profileID, "bucket1", "photos/vacation/")
	if err != nil {
		t.Fatalf("ListAllKeysUnderPrefix() returned error: %v", err)
	}

	if got := atomic.LoadInt32(&requestCount); got != 1 {
		t.Fatalf("request count = %d, want 1", got)
	}
	if len(keys) != 3 {
		t.Fatalf("len(keys) = %d, want 3: %v", len(keys), keys)
	}
}

// TestFileManagerServiceListAllKeysUnderPrefixIncludesPlaceholderKey makes
// explicit the one place ListAllKeysUnderPrefix deliberately differs from
// ListObjects/entriesFromPage: a Contents entry whose key equals prefix
// itself must be included in the result, not filtered out.
func TestFileManagerServiceListAllKeysUnderPrefixIncludesPlaceholderKey(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(singlePageAllKeysBody))
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	keys, err := fm.ListAllKeysUnderPrefix(profileID, "bucket1", "photos/vacation/")
	if err != nil {
		t.Fatalf("ListAllKeysUnderPrefix() returned error: %v", err)
	}

	found := false

	for _, k := range keys {
		if k == "photos/vacation/" {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("keys = %v, want placeholder key %q included", keys, "photos/vacation/")
	}
}

const page1TruncatedAllKeysBody = `<?xml version="1.0" encoding="UTF-8"?>
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

const page2FinalAllKeysBody = `<?xml version="1.0" encoding="UTF-8"?>
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

func TestFileManagerServiceListAllKeysUnderPrefixMultiplePages(t *testing.T) {
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

			_, _ = w.Write([]byte(page1TruncatedAllKeysBody))

			return
		}

		if got := r.URL.Query().Get("continuation-token"); got != "page-2-token" {
			t.Errorf("second request ContinuationToken = %q, want %q", got, "page-2-token")
		}

		_, _ = w.Write([]byte(page2FinalAllKeysBody))
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	keys, err := fm.ListAllKeysUnderPrefix(profileID, "bucket1", "photos/vacation/")
	if err != nil {
		t.Fatalf("ListAllKeysUnderPrefix() returned error: %v", err)
	}

	if got := atomic.LoadInt32(&requestCount); got != 2 {
		t.Fatalf("request count = %d, want 2", got)
	}
	if len(keys) != 2 {
		t.Fatalf("len(keys) = %d, want 2 (accumulated across both pages): %v", len(keys), keys)
	}
	if keys[0] != "photos/vacation/a.jpg" || keys[1] != "photos/vacation/b.jpg" {
		t.Errorf("keys = %v, want [photos/vacation/a.jpg photos/vacation/b.jpg]", keys)
	}
}

const emptyAllKeysBody = `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>bucket1</Name>
  <Prefix>photos/empty/</Prefix>
  <KeyCount>0</KeyCount>
  <MaxKeys>1000</MaxKeys>
  <IsTruncated>false</IsTruncated>
</ListBucketResult>`

func TestFileManagerServiceListAllKeysUnderPrefixEmptyIsNotError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(emptyAllKeysBody))
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	keys, err := fm.ListAllKeysUnderPrefix(profileID, "bucket1", "photos/empty/")
	if err != nil {
		t.Fatalf("ListAllKeysUnderPrefix() returned error: %v", err)
	}

	if keys == nil {
		t.Fatal("keys = nil, want non-nil empty slice")
	}
	if len(keys) != 0 {
		t.Fatalf("len(keys) = %d, want 0: %v", len(keys), keys)
	}
}

// TestFileManagerServiceListAllKeysUnderPrefixReturnsErrLockedWhenLocked
// verifies ListAllKeysUnderPrefix's Этап 4 суб-этап 4.4 guard.
func TestFileManagerServiceListAllKeysUnderPrefixReturnsErrLockedWhenLocked(t *testing.T) {
	t.Parallel()

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, "http://127.0.0.1:1") // never contacted

	fm.keyBox.Clear()

	_, err := fm.ListAllKeysUnderPrefix(profileID, "bucket1", "photos/vacation/")
	if !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("ListAllKeysUnderPrefix() on a locked service error = %v, want errors.Is(_, domain.ErrLocked)", err)
	}
}
