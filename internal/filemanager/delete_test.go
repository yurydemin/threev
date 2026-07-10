package filemanager

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"threev/internal/domain"
)

// deleteObjectsRequestBody mirrors the shape of the XML body a real S3
// DeleteObjects request sends (<Delete><Object><Key>...</Key></Object>...
// </Delete>), just enough to let deleteMock recover which keys a batch
// actually asked for.
type deleteObjectsRequestBody struct {
	XMLName xml.Name `xml:"Delete"`
	Objects []struct {
		Key string `xml:"Key"`
	} `xml:"Object"`
}

// deleteMock simulates the S3 DeleteObjects (POST .../bucket?delete)
// endpoint: it records every batch of keys it receives (in arrival order,
// for asserting batch boundaries), reports any key in failKeys back as a
// per-item DeleteObjectsOutput.Errors entry (S3's own "authoritative,
// never retried" per-key failure mode - see runDeleteObjects' doc comment),
// and can optionally block one specific request (1-based blockOnRequest)
// until its context is canceled - the same blockOffset/blockEnabled/
// blockStarted technique transfer's rangeMock uses for its own cancel-
// mid-flight tests, adapted to "block the Nth request" rather than "block a
// request for a specific byte range" since DeleteObjects batches don't have
// an analogous natural key to block on ahead of time.
type deleteMock struct {
	failKeys map[string]bool

	blockOnRequest int
	blockOnce      sync.Once
	blockStarted   chan struct{}

	mu              sync.Mutex
	requestCount    int
	receivedBatches [][]string
}

func newDeleteMockServer(t *testing.T, m *deleteMock) *httptest.Server {
	t.Helper()

	if m.blockStarted == nil {
		m.blockStarted = make(chan struct{})
	}

	server := httptest.NewServer(http.HandlerFunc(m.handler))
	t.Cleanup(server.Close)

	return server
}

func (m *deleteMock) getRequestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.requestCount
}

func (m *deleteMock) getReceivedBatches() [][]string {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([][]string, len(m.receivedBatches))
	copy(out, m.receivedBatches)

	return out
}

func (m *deleteMock) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	var parsed deleteObjectsRequestBody
	if err := xml.Unmarshal(body, &parsed); err != nil {
		http.Error(w, "deleteMock: failed to parse request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	keys := make([]string, len(parsed.Objects))
	for i, obj := range parsed.Objects {
		keys[i] = obj.Key
	}

	m.mu.Lock()
	m.requestCount++
	idx := m.requestCount
	m.receivedBatches = append(m.receivedBatches, keys)
	m.mu.Unlock()

	if m.blockOnRequest != 0 && idx == m.blockOnRequest {
		m.blockOnce.Do(func() { close(m.blockStarted) })
		<-r.Context().Done() // hang until the client cancels, simulating an interrupted in-flight batch
		return
	}

	var buf strings.Builder

	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?><DeleteResult>`)

	for _, k := range keys {
		m.mu.Lock()
		fail := m.failKeys[k]
		m.mu.Unlock()

		if fail {
			fmt.Fprintf(&buf, `<Error><Key>%s</Key><Code>AccessDenied</Code><Message>no permission</Message></Error>`, k)
		} else {
			fmt.Fprintf(&buf, `<Deleted><Key>%s</Key></Deleted>`, k)
		}
	}

	buf.WriteString(`</DeleteResult>`)

	writeXML(w, http.StatusOK, buf.String())
}

func TestFileManagerServiceDeleteObjectsRejectsEmptyKeys(t *testing.T) {
	t.Parallel()

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, "http://127.0.0.1:1") // never contacted

	if _, err := fm.DeleteObjects(domain.DeleteObjectsRequest{ProfileID: profileID, Bucket: "bucket1"}); err == nil {
		t.Fatal("DeleteObjects() with no keys returned nil error, want an error")
	}
}

func TestFileManagerServiceDeleteObjectsSingleBatchSuccess(t *testing.T) {
	t.Parallel()

	mock := &deleteMock{}
	server := newDeleteMockServer(t, mock)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	keys := []string{"file1.txt", "file2.txt", "file3.txt"}

	opID, err := fm.DeleteObjects(domain.DeleteObjectsRequest{ProfileID: profileID, Bucket: "bucket1", Keys: keys})
	if err != nil {
		t.Fatalf("DeleteObjects() returned error: %v", err)
	}

	waitForBulkOpDone(t, fm, opID)

	if got := mock.getRequestCount(); got != 1 {
		t.Fatalf("mock request count = %d, want 1 (all 3 keys fit in a single batch)", got)
	}

	batches := mock.getReceivedBatches()
	if len(batches) != 1 || len(batches[0]) != 3 {
		t.Fatalf("received batches = %+v, want one batch of 3 keys", batches)
	}
}

func TestFileManagerServiceDeleteObjectsTwoBatchesOver1000Keys(t *testing.T) {
	t.Parallel()

	mock := &deleteMock{}
	server := newDeleteMockServer(t, mock)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	keys := genKeys(1500)

	opID, err := fm.DeleteObjects(domain.DeleteObjectsRequest{ProfileID: profileID, Bucket: "bucket1", Keys: keys})
	if err != nil {
		t.Fatalf("DeleteObjects() returned error: %v", err)
	}

	waitForBulkOpDone(t, fm, opID)

	if got := mock.getRequestCount(); got != 2 {
		t.Fatalf("mock request count = %d, want 2 (1500 keys split into 1000 + 500)", got)
	}

	batches := mock.getReceivedBatches()
	if len(batches) != 2 {
		t.Fatalf("received %d batches, want 2", len(batches))
	}
	if len(batches[0]) != 1000 {
		t.Errorf("first batch has %d keys, want 1000", len(batches[0]))
	}
	if len(batches[1]) != 500 {
		t.Errorf("second batch has %d keys, want 500", len(batches[1]))
	}
}

// TestFileManagerServiceDeleteObjectsPartialPerKeyErrorsAreNotRetried is a
// regression test for runDeleteObjects' documented distinction between a
// transport failure (retried by s3client.WithRetry) and S3's own per-key
// DeleteObjectsOutput.Errors (authoritative, final, never retried - the
// batch call itself already succeeded with an HTTP 200): with one key
// configured to come back as a per-item error, the mock must still see
// exactly ONE request for the batch - if per-key errors were (incorrectly)
// treated as retryable, s3client.MetadataRetryPolicy would cause up to 3.
func TestFileManagerServiceDeleteObjectsPartialPerKeyErrorsAreNotRetried(t *testing.T) {
	t.Parallel()

	mock := &deleteMock{failKeys: map[string]bool{"file2.txt": true}}
	server := newDeleteMockServer(t, mock)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	keys := []string{"file1.txt", "file2.txt", "file3.txt"}

	opID, err := fm.DeleteObjects(domain.DeleteObjectsRequest{ProfileID: profileID, Bucket: "bucket1", Keys: keys})
	if err != nil {
		t.Fatalf("DeleteObjects() returned error: %v", err)
	}

	waitForBulkOpDone(t, fm, opID)

	if got := mock.getRequestCount(); got != 1 {
		t.Fatalf("mock request count = %d, want 1 (a per-key DeleteObjectsOutput.Errors entry must never trigger a retry of the whole batch)", got)
	}
}

// TestFileManagerServiceDeleteObjectsCancelBetweenBatchesStopsSecondBatch
// verifies CancelBulkOperation against a genuinely in-flight delete: the
// mock blocks the FIRST batch's request; the test waits for it to arrive,
// cancels, and then asserts the SECOND batch (of a 2-batch, 1200-key
// delete) was never even sent - runDeleteObjects' ctx.Err() check at the
// top of its loop must have stopped the loop before starting it.
func TestFileManagerServiceDeleteObjectsCancelBetweenBatchesStopsSecondBatch(t *testing.T) {
	t.Parallel()

	mock := &deleteMock{blockOnRequest: 1}
	server := newDeleteMockServer(t, mock)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	keys := genKeys(1200) // 2 batches: 1000 + 200

	opID, err := fm.DeleteObjects(domain.DeleteObjectsRequest{ProfileID: profileID, Bucket: "bucket1", Keys: keys})
	if err != nil {
		t.Fatalf("DeleteObjects() returned error: %v", err)
	}

	select {
	case <-mock.blockStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the blocking first-batch DeleteObjects request to arrive")
	}

	if err := fm.CancelBulkOperation(opID); err != nil {
		t.Fatalf("CancelBulkOperation() returned error: %v", err)
	}

	if got := mock.getRequestCount(); got != 1 {
		t.Fatalf("mock request count after cancel = %d, want 1 (the second batch must never have been sent)", got)
	}
}
