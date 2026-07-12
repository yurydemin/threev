//go:build integration

package integration

import (
	"fmt"
	"testing"

	"threev/internal/domain"
)

// TestIntegrationListBuckets verifies a bucket created directly against
// MinIO (newTestBucket) is actually visible through
// FileManagerService.ListBuckets (FR-FM-001) - the first place in this
// suite that exercises a Service method (rather than a throwaway
// *s3.Client built via s3client.NewS3Client) against real MinIO.
func TestIntegrationListBuckets(t *testing.T) {
	svc := newIntegrationServices(t)
	bucket := newTestBucket(t, svc.profile)

	buckets, err := svc.fm.ListBuckets(svc.profileID)
	if err != nil {
		t.Fatalf("ListBuckets(%d) returned error: %v", svc.profileID, err)
	}

	for _, b := range buckets {
		if b.Name == bucket {
			return
		}
	}

	t.Fatalf("ListBuckets(%d) = %+v, want bucket %q present", svc.profileID, buckets, bucket)
}

// TestIntegrationListObjectsAndHeadObject seeds a handful of objects
// directly (putTestObject), then verifies ListObjects (FR-FM-002/004/005)
// finds every one of them and HeadObject (docs/02-tech-spec.md section 9.2)
// reports the correct size/content-type for one of them - against real
// MinIO, not an httptest mock.
func TestIntegrationListObjectsAndHeadObject(t *testing.T) {
	svc := newIntegrationServices(t)
	bucket := newTestBucket(t, svc.profile)

	const (
		objectCount = 5
		prefix      = "integration-list/"
		contentType = "text/plain"
	)

	body := []byte("threev integration test object content\n")

	keys := make([]string, objectCount)
	for i := range keys {
		keys[i] = fmt.Sprintf("%sobject-%d.txt", prefix, i)
		putTestObject(t, svc.profile, bucket, keys[i], body, contentType)
	}

	resp, err := svc.fm.ListObjects(domain.ListObjectsRequest{
		ProfileID: svc.profileID,
		Bucket:    bucket,
		Prefix:    prefix,
	})
	if err != nil {
		t.Fatalf("ListObjects() returned error: %v", err)
	}

	if len(resp.Entries) != objectCount {
		t.Fatalf("ListObjects() returned %d entries, want %d (entries: %+v)", len(resp.Entries), objectCount, resp.Entries)
	}

	seen := make(map[string]domain.ObjectEntry, len(resp.Entries))
	for _, e := range resp.Entries {
		seen[e.Key] = e
	}

	for _, key := range keys {
		entry, ok := seen[key]
		if !ok {
			t.Errorf("ListObjects() missing key %q", key)
			continue
		}

		if entry.IsFolder {
			t.Errorf("entry %q IsFolder = true, want false", key)
		}
		if entry.Size != int64(len(body)) {
			t.Errorf("entry %q Size = %d, want %d", key, entry.Size, len(body))
		}
	}

	meta, err := svc.fm.HeadObject(svc.profileID, bucket, keys[0])
	if err != nil {
		t.Fatalf("HeadObject(%d, %q, %q) returned error: %v", svc.profileID, bucket, keys[0], err)
	}

	if meta.Size != int64(len(body)) {
		t.Errorf("HeadObject() Size = %d, want %d", meta.Size, len(body))
	}
	if meta.ContentType != contentType {
		t.Errorf("HeadObject() ContentType = %q, want %q", meta.ContentType, contentType)
	}
}
