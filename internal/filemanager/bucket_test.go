package filemanager

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"threev/internal/domain"
)

const invalidBucketNameErrorBody = `<?xml version="1.0" encoding="UTF-8"?>
<Error>
  <Code>InvalidBucketName</Code>
  <Message>The specified bucket is not valid.</Message>
  <BucketName>Invalid_Bucket</BucketName>
  <RequestId>test-request-id</RequestId>
  <HostId>test-host-id</HostId>
</Error>`

const bucketNotEmptyErrorBody = `<?xml version="1.0" encoding="UTF-8"?>
<Error>
  <Code>BucketNotEmpty</Code>
  <Message>The bucket you tried to delete is not empty</Message>
  <BucketName>bucket1</BucketName>
  <RequestId>test-request-id</RequestId>
  <HostId>test-host-id</HostId>
</Error>`

func TestFileManagerServiceCreateBucketSuccess(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	err := fm.CreateBucket(profileID, "bucketname")
	if err != nil {
		t.Fatalf("CreateBucket() returned error: %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Errorf("request method = %q, want %q", gotMethod, http.MethodPut)
	}

	if want := "/bucketname"; gotPath != want {
		t.Errorf("request path = %q, want %q", gotPath, want)
	}
}

func TestFileManagerServiceCreateBucketInvalidName(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(invalidBucketNameErrorBody))
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	err := fm.CreateBucket(profileID, "Invalid_Bucket")
	if err == nil {
		t.Fatal("CreateBucket() with an invalid name returned nil error, want an error")
	}
	if !strings.Contains(err.Error(), "create bucket") {
		t.Errorf("error = %q, want it to mention the operation (%q)", err.Error(), "create bucket")
	}
}

// TestFileManagerServiceCreateBucketReturnsErrLockedWhenLocked verifies
// CreateBucket's Этап 4 суб-этап 4.4 guard.
func TestFileManagerServiceCreateBucketReturnsErrLockedWhenLocked(t *testing.T) {
	t.Parallel()

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, "http://127.0.0.1:1") // never contacted

	fm.keyBox.Clear()

	err := fm.CreateBucket(profileID, "bucketname")
	if !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("CreateBucket() on a locked service error = %v, want errors.Is(_, domain.ErrLocked)", err)
	}
}

func TestFileManagerServiceDeleteBucketSuccess(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	err := fm.DeleteBucket(profileID, "bucketname")
	if err != nil {
		t.Fatalf("DeleteBucket() returned error: %v", err)
	}

	if gotMethod != http.MethodDelete {
		t.Errorf("request method = %q, want %q", gotMethod, http.MethodDelete)
	}

	if want := "/bucketname"; gotPath != want {
		t.Errorf("request path = %q, want %q", gotPath, want)
	}
}

func TestFileManagerServiceDeleteBucketNonEmptyError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(bucketNotEmptyErrorBody))
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	err := fm.DeleteBucket(profileID, "bucket1")
	if err == nil {
		t.Fatal("DeleteBucket() on a non-empty bucket returned nil error, want an error")
	}
	if !strings.Contains(err.Error(), "delete bucket") {
		t.Errorf("error = %q, want it to mention the operation (%q)", err.Error(), "delete bucket")
	}
	if !strings.Contains(err.Error(), "not-empty") {
		t.Errorf("error = %q, want it classified as category %q", err.Error(), "not-empty")
	}
	if !strings.Contains(err.Error(), "не пуст") {
		t.Errorf("error = %q, want it to contain the BucketNotEmpty-specific message", err.Error())
	}
}

// TestFileManagerServiceDeleteBucketReturnsErrLockedWhenLocked verifies
// DeleteBucket's Этап 4 суб-этап 4.4 guard.
func TestFileManagerServiceDeleteBucketReturnsErrLockedWhenLocked(t *testing.T) {
	t.Parallel()

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, "http://127.0.0.1:1") // never contacted

	fm.keyBox.Clear()

	err := fm.DeleteBucket(profileID, "bucketname")
	if !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("DeleteBucket() on a locked service error = %v, want errors.Is(_, domain.ErrLocked)", err)
	}
}
