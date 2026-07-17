package filemanager

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"threev/internal/connection"
	"threev/internal/crypto"
	"threev/internal/domain"
	"threev/internal/s3client"
	"threev/internal/storage"
)

// newTestFileManagerService opens a fresh migrated SQLite database backed by
// a temporary file and returns a FileManagerService over it, using a fixed
// (test-only) 32-byte encryption key already Set on a fresh *crypto.KeyBox -
// mirroring connection.newTestConnectionService (Этап 4 суб-этап 4.4: see
// TestFileManagerServiceListBucketsReturnsErrLockedWhenLocked for the
// dedicated locked-state test, which builds its own, never-Set KeyBox
// instead). connMgr/breaker are freshly constructed per call (never shared
// across tests), mirroring transfer.newTestTransferService's identical
// setup.
func newTestFileManagerService(t *testing.T) (*FileManagerService, *storage.ProfileRepository, [32]byte) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "filemanager_service_test.db")

	db, err := storage.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB(%q) returned error: %v", dbPath, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close() returned error: %v", err)
		}
	})

	repo := storage.NewProfileRepository(db)

	var key [32]byte
	for i := range key {
		key[i] = byte(i)
	}

	keyBox := crypto.NewKeyBox()
	keyBox.Set(key)

	connMgr := s3client.NewConnectionManager(repo, keyBox)
	breaker := s3client.NewCircuitBreaker()
	retryPolicies := s3client.NewRetryPolicyStore()

	return NewFileManagerService(repo, keyBox, connMgr, breaker, retryPolicies), repo, key
}

// saveTestProfile persists (via ConnectionService, so credentials are
// encrypted exactly as they would be in production) a profile pointed at
// endpoint, and returns its assigned ID.
func saveTestProfile(t *testing.T, repo *storage.ProfileRepository, key [32]byte, endpoint string) int64 {
	t.Helper()

	keyBox := crypto.NewKeyBox()
	keyBox.Set(key)

	connSvc := connection.NewConnectionService(repo, keyBox)

	saved, err := connSvc.SaveProfile(domain.Profile{
		Name:            "prod",
		EndpointURL:     endpoint,
		Region:          "us-east-1",
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "supersecret",
		PathStyle:       true,
		VerifySSL:       true,
	})
	if err != nil {
		t.Fatalf("SaveProfile() returned error: %v", err)
	}

	return saved.ID
}

const listBucketsSuccessBody = `<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Owner>
    <ID>owner-id</ID>
    <DisplayName>owner-name</DisplayName>
  </Owner>
  <Buckets>
    <Bucket>
      <Name>bucket1</Name>
      <CreationDate>2019-01-01T00:00:00.000Z</CreationDate>
    </Bucket>
    <Bucket>
      <Name>bucket2</Name>
      <CreationDate>2020-06-15T12:30:00.000Z</CreationDate>
    </Bucket>
  </Buckets>
</ListAllMyBucketsResult>`

func TestFileManagerServiceListBuckets(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(listBucketsSuccessBody))
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	buckets, err := fm.ListBuckets(profileID)
	if err != nil {
		t.Fatalf("ListBuckets() returned error: %v", err)
	}

	if len(buckets) != 2 {
		t.Fatalf("ListBuckets() returned %d buckets, want 2", len(buckets))
	}
	if buckets[0].Name != "bucket1" {
		t.Errorf("buckets[0].Name = %q, want %q", buckets[0].Name, "bucket1")
	}
	if buckets[0].CreationDate.IsZero() {
		t.Error("buckets[0].CreationDate is zero, want populated")
	}
	if buckets[1].Name != "bucket2" {
		t.Errorf("buckets[1].Name = %q, want %q", buckets[1].Name, "bucket2")
	}
}

// singlePageObjectsBody describes one un-truncated ListObjectsV2 page: a
// folder plus two files whose Key-ascending order differs from their
// Size-ascending order, so a re-sort is observable without a second S3
// request (file1.txt is larger than file2.txt, but sorts first by name).
const singlePageObjectsBody = `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>bucket1</Name>
  <Prefix></Prefix>
  <KeyCount>3</KeyCount>
  <MaxKeys>1000</MaxKeys>
  <Delimiter>/</Delimiter>
  <IsTruncated>false</IsTruncated>
  <Contents>
    <Key>file1.txt</Key>
    <LastModified>2024-01-01T00:00:00.000Z</LastModified>
    <ETag>"etag1"</ETag>
    <Size>200</Size>
    <StorageClass>STANDARD</StorageClass>
  </Contents>
  <Contents>
    <Key>file2.txt</Key>
    <LastModified>2024-02-01T00:00:00.000Z</LastModified>
    <ETag>"etag2"</ETag>
    <Size>100</Size>
    <StorageClass>STANDARD</StorageClass>
  </Contents>
  <CommonPrefixes>
    <Prefix>folder1/</Prefix>
  </CommonPrefixes>
</ListBucketResult>`

func TestFileManagerServiceListObjectsFirstPage(t *testing.T) {
	t.Parallel()

	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(singlePageObjectsBody))
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	resp, err := fm.ListObjects(domain.ListObjectsRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		SortBy:    "name",
		SortOrder: "asc",
	})
	if err != nil {
		t.Fatalf("ListObjects() returned error: %v", err)
	}

	if got := atomic.LoadInt32(&requestCount); got != 1 {
		t.Fatalf("request count = %d, want 1", got)
	}
	if resp.IsTruncated {
		t.Error("IsTruncated = true, want false")
	}
	if len(resp.Entries) != 3 {
		t.Fatalf("len(Entries) = %d, want 3", len(resp.Entries))
	}
	if !resp.Entries[0].IsFolder || resp.Entries[0].Key != "folder1/" {
		t.Errorf("Entries[0] = %+v, want folder folder1/", resp.Entries[0])
	}
	if resp.Entries[1].Key != "file1.txt" || resp.Entries[2].Key != "file2.txt" {
		t.Errorf("file order = [%q %q], want [file1.txt file2.txt] (name asc)", resp.Entries[1].Key, resp.Entries[2].Key)
	}
}

func TestFileManagerServiceListObjectsResortUsesCacheNotServer(t *testing.T) {
	t.Parallel()

	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(singlePageObjectsBody))
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	req := domain.ListObjectsRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		SortBy:    "name",
		SortOrder: "asc",
	}

	if _, err := fm.ListObjects(req); err != nil {
		t.Fatalf("first ListObjects() returned error: %v", err)
	}
	if got := atomic.LoadInt32(&requestCount); got != 1 {
		t.Fatalf("request count after first call = %d, want 1", got)
	}

	// Same profile+bucket+prefix, first page (no ContinuationToken), only
	// SortBy/SortOrder changed, Refresh left false: must be served entirely
	// from cache.
	req.SortBy = "size"
	req.SortOrder = "asc"

	resp, err := fm.ListObjects(req)
	if err != nil {
		t.Fatalf("second ListObjects() returned error: %v", err)
	}

	if got := atomic.LoadInt32(&requestCount); got != 1 {
		t.Fatalf("request count after re-sort = %d, want 1 (must be served from cache)", got)
	}
	if len(resp.Entries) != 3 {
		t.Fatalf("len(Entries) = %d, want 3", len(resp.Entries))
	}
	if resp.Entries[1].Key != "file2.txt" || resp.Entries[2].Key != "file1.txt" {
		t.Errorf("file order = [%q %q], want [file2.txt file1.txt] (size asc)", resp.Entries[1].Key, resp.Entries[2].Key)
	}
}

const page1TruncatedBody = `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>bucket1</Name>
  <Prefix></Prefix>
  <KeyCount>1</KeyCount>
  <MaxKeys>1000</MaxKeys>
  <Delimiter>/</Delimiter>
  <IsTruncated>true</IsTruncated>
  <NextContinuationToken>page-2-token</NextContinuationToken>
  <Contents>
    <Key>file-a.txt</Key>
    <LastModified>2024-01-01T00:00:00.000Z</LastModified>
    <ETag>"etag-a"</ETag>
    <Size>10</Size>
    <StorageClass>STANDARD</StorageClass>
  </Contents>
</ListBucketResult>`

const page2FinalBody = `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>bucket1</Name>
  <Prefix></Prefix>
  <KeyCount>1</KeyCount>
  <MaxKeys>1000</MaxKeys>
  <Delimiter>/</Delimiter>
  <IsTruncated>false</IsTruncated>
  <Contents>
    <Key>file-b.txt</Key>
    <LastModified>2024-01-02T00:00:00.000Z</LastModified>
    <ETag>"etag-b"</ETag>
    <Size>20</Size>
    <StorageClass>STANDARD</StorageClass>
  </Contents>
</ListBucketResult>`

func TestFileManagerServiceListObjectsContinuationTokenFetchesAndAccumulates(t *testing.T) {
	t.Parallel()

	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)

		if r.URL.Query().Get("continuation-token") == "page-2-token" {
			_, _ = w.Write([]byte(page2FinalBody))

			return
		}

		_, _ = w.Write([]byte(page1TruncatedBody))
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	first, err := fm.ListObjects(domain.ListObjectsRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		SortBy:    "name",
		SortOrder: "asc",
	})
	if err != nil {
		t.Fatalf("first ListObjects() returned error: %v", err)
	}
	if got := atomic.LoadInt32(&requestCount); got != 1 {
		t.Fatalf("request count after first page = %d, want 1", got)
	}
	if !first.IsTruncated {
		t.Fatal("first.IsTruncated = false, want true")
	}
	if first.NextContinuationToken != "page-2-token" {
		t.Fatalf("first.NextContinuationToken = %q, want %q", first.NextContinuationToken, "page-2-token")
	}
	if len(first.Entries) != 1 || first.Entries[0].Key != "file-a.txt" {
		t.Fatalf("first.Entries = %+v, want [file-a.txt]", first.Entries)
	}

	second, err := fm.ListObjects(domain.ListObjectsRequest{
		ProfileID:         profileID,
		Bucket:            "bucket1",
		ContinuationToken: "page-2-token",
		SortBy:            "name",
		SortOrder:         "asc",
	})
	if err != nil {
		t.Fatalf("second ListObjects() returned error: %v", err)
	}

	if got := atomic.LoadInt32(&requestCount); got != 2 {
		t.Fatalf("request count after second page = %d, want 2", got)
	}
	if second.IsTruncated {
		t.Error("second.IsTruncated = true, want false")
	}
	if len(second.Entries) != 2 {
		t.Fatalf("len(second.Entries) = %d, want 2 (accumulated across both pages)", len(second.Entries))
	}
	if second.Entries[0].Key != "file-a.txt" || second.Entries[1].Key != "file-b.txt" {
		t.Errorf("second.Entries keys = [%q %q], want [file-a.txt file-b.txt]", second.Entries[0].Key, second.Entries[1].Key)
	}
}

func TestFileManagerServiceListObjectsRefreshBypassesCache(t *testing.T) {
	t.Parallel()

	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(singlePageObjectsBody))
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	req := domain.ListObjectsRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		SortBy:    "name",
		SortOrder: "asc",
	}

	if _, err := fm.ListObjects(req); err != nil {
		t.Fatalf("first ListObjects() returned error: %v", err)
	}
	if got := atomic.LoadInt32(&requestCount); got != 1 {
		t.Fatalf("request count after first call = %d, want 1", got)
	}

	req.Refresh = true

	if _, err := fm.ListObjects(req); err != nil {
		t.Fatalf("second (Refresh=true) ListObjects() returned error: %v", err)
	}

	if got := atomic.LoadInt32(&requestCount); got != 2 {
		t.Fatalf("request count after Refresh=true call = %d, want 2 (must bypass cache)", got)
	}
}

const noSuchBucketErrorBody = `<?xml version="1.0" encoding="UTF-8"?>
<Error>
  <Code>NoSuchBucket</Code>
  <Message>The specified bucket does not exist</Message>
  <BucketName>missing-bucket</BucketName>
  <RequestId>test-request-id</RequestId>
  <HostId>test-host-id</HostId>
</Error>`

func TestFileManagerServiceListObjectsNoSuchBucketError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(noSuchBucketErrorBody))
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	_, err := fm.ListObjects(domain.ListObjectsRequest{
		ProfileID: profileID,
		Bucket:    "missing-bucket",
	})
	if err == nil {
		t.Fatal("ListObjects() returned nil error, want NoSuchBucket error")
	}
	if !strings.Contains(err.Error(), "list objects") {
		t.Errorf("error = %q, want it to mention the operation (%q)", err.Error(), "list objects")
	}
	if !strings.Contains(err.Error(), "not-found") {
		t.Errorf("error = %q, want it classified as category %q", err.Error(), "not-found")
	}
	if !strings.Contains(err.Error(), "Бакет не найден") {
		t.Errorf("error = %q, want it to contain the NoSuchBucket-specific message", err.Error())
	}
}

// TestFileManagerServiceListBucketsReturnsErrLockedWhenLocked verifies
// ListBuckets' Этап 4 суб-этап 4.4 guard.
func TestFileManagerServiceListBucketsReturnsErrLockedWhenLocked(t *testing.T) {
	t.Parallel()

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, "http://127.0.0.1:1") // never contacted

	fm.keyBox.Clear()

	_, err := fm.ListBuckets(profileID)
	if !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("ListBuckets() on a locked service error = %v, want errors.Is(_, domain.ErrLocked)", err)
	}
}

// TestFileManagerServiceListObjectsReturnsErrLockedWhenLocked verifies
// ListObjects' Этап 4 суб-этап 4.4 guard.
func TestFileManagerServiceListObjectsReturnsErrLockedWhenLocked(t *testing.T) {
	t.Parallel()

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, "http://127.0.0.1:1") // never contacted

	fm.keyBox.Clear()

	_, err := fm.ListObjects(domain.ListObjectsRequest{ProfileID: profileID, Bucket: "bucket1"})
	if !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("ListObjects() on a locked service error = %v, want errors.Is(_, domain.ErrLocked)", err)
	}
}
