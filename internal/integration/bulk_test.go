//go:build integration

package integration

import (
	"testing"
	"time"

	"threev/internal/domain"
)

// bulkOperationTimeout bounds how long this file's tests wait
// (waitForCondition) for an async DeleteObjects/CopyObjects/MoveObjects
// call to become observable via a follow-up ListObjects - generous for the
// handful of small objects these tests seed, even against a local MinIO
// instance under CI load.
const bulkOperationTimeout = 30 * time.Second

// TestIntegrationDeleteObjects verifies FileManagerService.DeleteObjects
// (FR-BULK-002) against real MinIO: a handful of objects are seeded, an
// async delete is started, and ListObjects (Refresh: true, so the
// session cache from before the delete cannot mask a stale result) is
// polled until they are all gone.
func TestIntegrationDeleteObjects(t *testing.T) {
	svc := newIntegrationServices(t)
	bucket := newTestBucket(t, svc.profile)

	const prefix = "bulk-delete/"

	keys := []string{prefix + "one.txt", prefix + "two.txt", prefix + "three.txt"}
	for _, key := range keys {
		putTestObject(t, svc.profile, bucket, key, []byte("delete me"), "text/plain")
	}

	if _, err := svc.fm.DeleteObjects(domain.DeleteObjectsRequest{
		ProfileID: svc.profileID,
		Bucket:    bucket,
		Keys:      keys,
	}); err != nil {
		t.Fatalf("DeleteObjects() returned error: %v", err)
	}

	waitForCondition(t, bulkOperationTimeout, "objects deleted", func() (bool, error) {
		resp, err := svc.fm.ListObjects(domain.ListObjectsRequest{
			ProfileID: svc.profileID,
			Bucket:    bucket,
			Prefix:    prefix,
			Refresh:   true,
		})
		if err != nil {
			return false, err
		}

		return len(resp.Entries) == 0, nil
	})
}

// TestIntegrationCopyMoveObjects exercises FileManagerService.CopyObjects
// and MoveObjects (FR-BULK-003) against real MinIO - including
// copyOneObject's copySourceFor URL-encoding of nested (multi-segment)
// object keys in CopyObjectInput.CopySource, which until now was only ever
// exercised against an httptest mock (copymove_test.go), never against a
// real S3-protocol server's own CopySource parsing.
func TestIntegrationCopyMoveObjects(t *testing.T) {
	svc := newIntegrationServices(t)
	sourceBucket := newTestBucket(t, svc.profile)
	destBucket := newTestBucket(t, svc.profile)

	const sourcePrefix = "nested/dir/"

	copyKeys := []string{sourcePrefix + "copy-1.txt", sourcePrefix + "copy-2.txt"}
	for _, key := range copyKeys {
		putTestObject(t, svc.profile, sourceBucket, key, []byte("copy me"), "text/plain")
	}

	if _, err := svc.fm.CopyObjects(domain.BulkCopyRequest{
		ProfileID:    svc.profileID,
		SourceBucket: sourceBucket,
		Keys:         copyKeys,
		DestBucket:   destBucket,
		DestPrefix:   "copied/",
	}); err != nil {
		t.Fatalf("CopyObjects() returned error: %v", err)
	}

	waitForCondition(t, bulkOperationTimeout, "copied objects appear in destination bucket", func() (bool, error) {
		resp, err := svc.fm.ListObjects(domain.ListObjectsRequest{
			ProfileID: svc.profileID,
			Bucket:    destBucket,
			Prefix:    "copied/",
			Refresh:   true,
		})
		if err != nil {
			return false, err
		}

		return len(resp.Entries) == len(copyKeys), nil
	})

	// Copy (not move): the source objects must still be present.
	srcResp, err := svc.fm.ListObjects(domain.ListObjectsRequest{
		ProfileID: svc.profileID,
		Bucket:    sourceBucket,
		Prefix:    sourcePrefix,
		Refresh:   true,
	})
	if err != nil {
		t.Fatalf("ListObjects() (source, post-copy) returned error: %v", err)
	}
	if len(srcResp.Entries) != len(copyKeys) {
		t.Fatalf("source bucket after copy has %d entries, want %d still present (entries: %+v)", len(srcResp.Entries), len(copyKeys), srcResp.Entries)
	}

	moveKey := sourcePrefix + "move-1.txt"
	putTestObject(t, svc.profile, sourceBucket, moveKey, []byte("move me"), "text/plain")

	if _, err := svc.fm.MoveObjects(domain.BulkMoveRequest{
		ProfileID:    svc.profileID,
		SourceBucket: sourceBucket,
		Keys:         []string{moveKey},
		DestBucket:   destBucket,
		DestPrefix:   "moved/",
	}); err != nil {
		t.Fatalf("MoveObjects() returned error: %v", err)
	}

	waitForCondition(t, bulkOperationTimeout, "moved object appears in destination bucket", func() (bool, error) {
		resp, err := svc.fm.ListObjects(domain.ListObjectsRequest{
			ProfileID: svc.profileID,
			Bucket:    destBucket,
			Prefix:    "moved/",
			Refresh:   true,
		})
		if err != nil {
			return false, err
		}

		return len(resp.Entries) == 1, nil
	})

	waitForCondition(t, bulkOperationTimeout, "moved object removed from source bucket", func() (bool, error) {
		resp, err := svc.fm.ListObjects(domain.ListObjectsRequest{
			ProfileID: svc.profileID,
			Bucket:    sourceBucket,
			Prefix:    sourcePrefix,
			Refresh:   true,
		})
		if err != nil {
			return false, err
		}

		for _, entry := range resp.Entries {
			if entry.Key == moveKey {
				return false, nil
			}
		}

		return true, nil
	})
}
