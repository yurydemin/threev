package filemanager

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"threev/internal/domain"
)

// waitForBulkOpDoneTimeout bounds every waitForBulkOpDone call in this
// package's tests - generous enough for the slowest of them (the
// 500-key/8-worker cancel-mid-pool test in copymove_test.go) while still
// failing a genuinely hung test in reasonable time.
const waitForBulkOpDoneTimeout = 5 * time.Second

// waitForBulkOpDone polls fm.running (white-box - this test file lives in
// package filemanager, not filemanager_test) until operationID is no
// longer present, i.e. its goroutine has called finishBulkOp - the bulk-op
// analogue of transfer's own poll-until-persisted-state test helpers
// (waitForTaskStatus/waitForHistoryEntry), needed here because bulk
// operations have no persisted queue/history table to poll instead.
func waitForBulkOpDone(t *testing.T, fm *FileManagerService, operationID int64) {
	t.Helper()

	deadline := time.Now().Add(waitForBulkOpDoneTimeout)

	for {
		fm.mu.Lock()
		_, running := fm.running[operationID]
		fm.mu.Unlock()

		if !running {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("bulk operation %d did not finish within %v", operationID, waitForBulkOpDoneTimeout)
		}

		time.Sleep(10 * time.Millisecond)
	}
}

// writeXML writes an XML response body with the given HTTP status, mirroring
// transfer's identically-named test helper (a different package, so not
// importable/reusable directly - see this package's other small,
// deliberately-duplicated test/production patterns, e.g. extractHostname).
func writeXML(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// accessDeniedErrorBody is a generic S3 AccessDenied error response,
// reused by every mock in this package's bulk-operation tests that needs to
// simulate a non-retryable, per-request auth failure.
const accessDeniedErrorBody = `<?xml version="1.0" encoding="UTF-8"?>
<Error>
  <Code>AccessDenied</Code>
  <Message>Access Denied</Message>
  <RequestId>test-request-id</RequestId>
  <HostId>test-host-id</HostId>
</Error>`

// genKeys returns n sequential, deterministically-ordered object keys
// ("obj-0000", "obj-0001", ...) - used by the batching/worker-pool tests
// that need a large key set (e.g. to exercise DeleteObjects' 1000-key batch
// boundary).
func genKeys(n int) []string {
	keys := make([]string, n)
	for i := range keys {
		keys[i] = fmt.Sprintf("obj-%04d", i)
	}

	return keys
}

func TestFileManagerServiceCancelBulkOperationNotFound(t *testing.T) {
	t.Parallel()

	fm, _, _ := newTestFileManagerService(t)

	err := fm.CancelBulkOperation(999999)
	if err == nil {
		t.Fatal("CancelBulkOperation() on an unknown operation id returned nil error, want a not-found error")
	}
}

func TestFileManagerServiceCancelBulkOperationAlreadyFinished(t *testing.T) {
	t.Parallel()

	server := newDeleteMockServer(t, &deleteMock{})

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	opID, err := fm.DeleteObjects(domain.DeleteObjectsRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		Keys:      []string{"key1"},
	})
	if err != nil {
		t.Fatalf("DeleteObjects() returned error: %v", err)
	}

	waitForBulkOpDone(t, fm, opID)

	if err := fm.CancelBulkOperation(opID); err == nil {
		t.Fatal("CancelBulkOperation() on an already-finished operation returned nil error, want a not-found error")
	}
}
