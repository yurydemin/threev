package filemanager

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"threev/internal/domain"
	"threev/internal/s3client"
)

// largeCopyMock is a minimal S3-compatible mock server implementing the
// operations copyOneObject/copyLargeObject exercise for a large-object copy,
// dispatching on HTTP method + which query parameters/headers are present -
// mirroring copymove_test.go's copyMock (small-object CopyObject/
// DeleteObject) and transfer's own mpuMock (ordinary multipart upload), but
// adapted for UploadPartCopy's request/response shape:
//
//	HeadObject:               HEAD   /bucket/key
//	CreateMultipartUpload:    POST   /bucket/key?uploads
//	UploadPartCopy:           PUT    /bucket/key?partNumber=N&uploadId=ID (X-Amz-Copy-Source set)
//	CompleteMultipartUpload:  POST   /bucket/key?uploadId=ID
//	AbortMultipartUpload:     DELETE /bucket/key?uploadId=ID
//	CopyObject (small path):  PUT    /bucket/key (X-Amz-Copy-Source set, no partNumber)
type largeCopyMock struct {
	uploadID string

	// headSize is the Content-Length HeadObject responds with - the value
	// copyOneObject's branching (single-shot CopyObject vs copyLargeObject)
	// actually keys off.
	headSize int64

	// failPartNumber, if non-zero, makes UploadPartCopy respond 403
	// AccessDenied for that part number every time - auth errors are never
	// retried (s3client.isRetryable), so this fails the part (and, via the
	// errgroup, the whole copy) quickly rather than only after exhausting
	// s3client.PartRetryPolicy's real multi-second backoff schedule.
	failPartNumber int32

	mu                 sync.Mutex
	createCount        int
	completeCount      int
	abortCount         int
	copyObjectCount    int
	uploadPartCopyHits map[int32]int
	copySourceRanges   map[int32]string // partNumber -> X-Amz-Copy-Source-Range header value received
	completedPartOrder []int32          // PartNumbers from the CompleteMultipartUpload request body, in the order the client sent them
}

func newLargeCopyMock(uploadID string, headSize int64) *largeCopyMock {
	return &largeCopyMock{
		uploadID:           uploadID,
		headSize:           headSize,
		uploadPartCopyHits: make(map[int32]int),
		copySourceRanges:   make(map[int32]string),
	}
}

func (m *largeCopyMock) counts() (create, complete, abort, copyObject int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.createCount, m.completeCount, m.abortCount, m.copyObjectCount
}

func (m *largeCopyMock) uploadPartCopyCount(partNumber int32) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.uploadPartCopyHits[partNumber]
}

func (m *largeCopyMock) totalUploadPartCopyRequests() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	total := 0
	for _, hits := range m.uploadPartCopyHits {
		total += hits
	}

	return total
}

func (m *largeCopyMock) copySourceRangeFor(partNumber int32) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	v, ok := m.copySourceRanges[partNumber]

	return v, ok
}

func (m *largeCopyMock) completedPartOrderSnapshot() []int32 {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]int32, len(m.completedPartOrder))
	copy(out, m.completedPartOrder)

	return out
}

func (m *largeCopyMock) handler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	copySource := r.Header.Get("X-Amz-Copy-Source")

	switch {
	case r.Method == http.MethodHead:
		m.handleHead(w)
	case r.Method == http.MethodPost && q.Has("uploads"):
		m.handleCreate(w)
	case r.Method == http.MethodPut && q.Get("partNumber") != "":
		m.handleUploadPartCopy(w, r, q)
	case r.Method == http.MethodPost && q.Get("uploadId") != "":
		m.handleComplete(w, r)
	case r.Method == http.MethodDelete && q.Get("uploadId") != "":
		m.handleAbort(w)
	case r.Method == http.MethodPut && copySource != "":
		m.handleCopyObject(w)
	default:
		http.Error(w, "largeCopyMock: unexpected request "+r.Method+" "+r.URL.String(), http.StatusBadRequest)
	}
}

func (m *largeCopyMock) handleHead(w http.ResponseWriter) {
	w.Header().Set("Content-Length", strconv.FormatInt(m.headSize, 10))
	w.WriteHeader(http.StatusOK)
}

func (m *largeCopyMock) handleCreate(w http.ResponseWriter) {
	m.mu.Lock()
	m.createCount++
	m.mu.Unlock()

	writeXML(w, http.StatusOK, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<InitiateMultipartUploadResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Bucket>dest-bucket</Bucket>
  <Key>dest-key</Key>
  <UploadId>%s</UploadId>
</InitiateMultipartUploadResult>`, m.uploadID))
}

func (m *largeCopyMock) handleUploadPartCopy(w http.ResponseWriter, r *http.Request, q url.Values) {
	n, err := strconv.Atoi(q.Get("partNumber"))
	if err != nil {
		http.Error(w, "largeCopyMock: bad partNumber", http.StatusBadRequest)
		return
	}

	partNumber := int32(n)
	rangeHeader := r.Header.Get("X-Amz-Copy-Source-Range")

	m.mu.Lock()
	m.uploadPartCopyHits[partNumber]++
	m.copySourceRanges[partNumber] = rangeHeader
	fail := m.failPartNumber != 0 && partNumber == m.failPartNumber
	m.mu.Unlock()

	_, _ = io.Copy(io.Discard, r.Body)

	if fail {
		writeXML(w, http.StatusForbidden, accessDeniedErrorBody)
		return
	}

	writeXML(w, http.StatusOK, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<CopyPartResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <ETag>"part-etag-%d"</ETag>
  <LastModified>2024-01-01T00:00:00.000Z</LastModified>
</CopyPartResult>`, partNumber))
}

// completeMultipartUploadRequestBody mirrors the request body
// CompleteMultipartUpload sends (see aws-sdk-go-v2's
// awsRestxml_serializeDocumentCompletedMultipartUpload): a flat list of
// <Part><PartNumber>.../<ETag>...</Part> elements directly under the
// <CompleteMultipartUpload> root, in whatever order the caller supplied
// them - which is exactly the ordering this mock captures for the "ascending
// PartNumber order" test assertion.
type completeMultipartUploadRequestBody struct {
	XMLName xml.Name `xml:"CompleteMultipartUpload"`
	Parts   []struct {
		PartNumber int32  `xml:"PartNumber"`
		ETag       string `xml:"ETag"`
	} `xml:"Part"`
}

func (m *largeCopyMock) handleComplete(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	var parsed completeMultipartUploadRequestBody
	_ = xml.Unmarshal(body, &parsed)

	order := make([]int32, 0, len(parsed.Parts))
	for _, p := range parsed.Parts {
		order = append(order, p.PartNumber)
	}

	m.mu.Lock()
	m.completeCount++
	m.completedPartOrder = order
	m.mu.Unlock()

	writeXML(w, http.StatusOK, `<?xml version="1.0" encoding="UTF-8"?>
<CompleteMultipartUploadResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Location>http://dest-bucket.s3.amazonaws.com/dest-key</Location>
  <Bucket>dest-bucket</Bucket>
  <Key>dest-key</Key>
  <ETag>"final-etag-placeholder"</ETag>
</CompleteMultipartUploadResult>`)
}

func (m *largeCopyMock) handleAbort(w http.ResponseWriter) {
	m.mu.Lock()
	m.abortCount++
	m.mu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

func (m *largeCopyMock) handleCopyObject(w http.ResponseWriter) {
	m.mu.Lock()
	m.copyObjectCount++
	m.mu.Unlock()

	writeXML(w, http.StatusOK, copyObjectSuccessBody)
}

// newLargeCopyTestClient returns an *s3.Client (and the bare hostname
// s3client.WithRetry's host parameter expects) pointed at serverURL,
// mirroring transfer's newTestUploadParams - needed here because
// copyLargeObject's own tests call it directly (not through
// FileManagerService.CopyObjects/resolveBulkClients) for the success/
// part-failure cases, which don't need a real *storage.ProfileRepository
// profile round-trip.
func newLargeCopyTestClient(t *testing.T, serverURL string) (*s3.Client, string) {
	t.Helper()

	profile := domain.Profile{
		Name:            "test",
		EndpointURL:     serverURL,
		Region:          "us-east-1",
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "supersecret",
		PathStyle:       true,
		VerifySSL:       true,
	}

	client, err := s3client.NewS3Client(profile)
	if err != nil {
		t.Fatalf("NewS3Client() returned error: %v", err)
	}

	parsed, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("url.Parse(%q) returned error: %v", serverURL, err)
	}

	return client, parsed.Hostname()
}

// largeCopyTestSize is small enough to keep this test fast (3 parts under
// the real, unmodified adaptive transfer.PartSize table - files under
// 100MB get 5MB parts, see transfer.PartSize's own doc comment) while still
// exercising copyLargeObject's full multi-part worker pool; it deliberately
// does NOT need to exceed maxSingleCopySize, since these tests call
// copyLargeObject directly rather than through copyOneObject's size
// branching (see TestFileManagerServiceCopyOneObjectSizeBranching below for
// that boundary check instead).
const largeCopyTestSize = 11 * 1024 * 1024

func TestCopyLargeObjectSuccess(t *testing.T) {
	t.Parallel()

	mock := newLargeCopyMock("upload-id-success", largeCopyTestSize)
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	fm, _, _ := newTestFileManagerService(t)
	client, host := newLargeCopyTestClient(t, server.URL)

	err := fm.copyLargeObject(context.Background(), client, client, host, "src-bucket", "big-file.bin", "dest-bucket", "dest-key", largeCopyTestSize)
	if err != nil {
		t.Fatalf("copyLargeObject() returned error: %v", err)
	}

	create, complete, abort, _ := mock.counts()
	if create != 1 {
		t.Errorf("createCount = %d, want 1", create)
	}
	if complete != 1 {
		t.Errorf("completeCount = %d, want 1", complete)
	}
	if abort != 0 {
		t.Errorf("abortCount = %d, want 0 (a fully successful copy must never abort)", abort)
	}

	if got := mock.totalUploadPartCopyRequests(); got != 3 {
		t.Errorf("total UploadPartCopy requests = %d, want 3 (11MB at the 5MB adaptive part size)", got)
	}

	wantRanges := map[int32]string{
		1: "bytes=0-5242879",
		2: "bytes=5242880-10485759",
		3: "bytes=10485760-11534335",
	}

	for partNumber, want := range wantRanges {
		if hits := mock.uploadPartCopyCount(partNumber); hits != 1 {
			t.Errorf("part %d: UploadPartCopy called %d times, want exactly 1", partNumber, hits)
		}

		got, ok := mock.copySourceRangeFor(partNumber)
		if !ok {
			t.Errorf("part %d: no UploadPartCopy request received", partNumber)
			continue
		}

		if got != want {
			t.Errorf("part %d: X-Amz-Copy-Source-Range = %q, want %q", partNumber, got, want)
		}
	}

	gotOrder := mock.completedPartOrderSnapshot()
	wantOrder := []int32{1, 2, 3}

	if len(gotOrder) != len(wantOrder) {
		t.Fatalf("CompleteMultipartUpload part list = %v, want %v", gotOrder, wantOrder)
	}

	for i, want := range wantOrder {
		if gotOrder[i] != want {
			t.Errorf("CompleteMultipartUpload part list = %v, want ascending PartNumber order %v", gotOrder, wantOrder)
			break
		}
	}
}

// TestCopyLargeObjectPartFailureAbortsUpload verifies that once one part's
// UploadPartCopy fails permanently (a non-retryable AccessDenied - see
// largeCopyMock.failPartNumber's doc comment for why this, rather than
// exhausting s3client.PartRetryPolicy's real backoff schedule, keeps this
// test fast), copyLargeObject aborts the multipart upload (best-effort) and
// returns an error - never a panic, never a silent partial success (no
// CompleteMultipartUpload call).
func TestCopyLargeObjectPartFailureAbortsUpload(t *testing.T) {
	t.Parallel()

	mock := newLargeCopyMock("upload-id-fail", largeCopyTestSize)
	mock.failPartNumber = 1

	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	fm, _, _ := newTestFileManagerService(t)
	client, host := newLargeCopyTestClient(t, server.URL)

	err := fm.copyLargeObject(context.Background(), client, client, host, "src-bucket", "big-file.bin", "dest-bucket", "dest-key", largeCopyTestSize)
	if err == nil {
		t.Fatal("copyLargeObject() returned a nil error, want the permanently-failing part's error")
	}

	create, complete, abort, _ := mock.counts()
	if create != 1 {
		t.Errorf("createCount = %d, want 1", create)
	}
	if complete != 0 {
		t.Errorf("completeCount = %d, want 0 (CompleteMultipartUpload must not run after a part permanently fails)", complete)
	}
	if abort != 1 {
		t.Errorf("abortCount = %d, want 1 (a permanently-failed part must trigger a best-effort AbortMultipartUpload)", abort)
	}
}

// TestFileManagerServiceCopyOneObjectSizeBranching is the boundary check for
// copyOneObject's size-based branching: an object HeadObject reports as
// just AT maxSingleCopySize (not exceeding it) must still take the
// single-shot CopyObject path, while one reported as just OVER it must take
// copyLargeObject's multipart path instead - asserted here purely from
// which S3 API calls the mock server actually received (a
// CreateMultipartUpload call happening or not happening is the reliable
// signal), exactly as this Block's task description calls for.
func TestFileManagerServiceCopyOneObjectSizeBranching(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		headSize          int64
		wantMultipart     bool
		wantCopyObjectHit bool
	}{
		{name: "at threshold uses single-shot CopyObject", headSize: maxSingleCopySize, wantMultipart: false, wantCopyObjectHit: true},
		{name: "over threshold uses multipart copy", headSize: maxSingleCopySize + 1, wantMultipart: true, wantCopyObjectHit: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := newLargeCopyMock("upload-id-boundary", tt.headSize)

			server := httptest.NewServer(http.HandlerFunc(mock.handler))
			t.Cleanup(server.Close)

			fm, repo, key := newTestFileManagerService(t)
			profileID := saveTestProfile(t, repo, key, server.URL)

			opID, err := fm.CopyObjects(domain.BulkCopyRequest{
				ProfileID:    profileID,
				SourceBucket: "src-bucket",
				Keys:         []string{"big-file.bin"},
				DestBucket:   "dest-bucket",
				DestPrefix:   "archive/",
			})
			if err != nil {
				t.Fatalf("CopyObjects() returned error: %v", err)
			}

			waitForBulkOpDone(t, fm, opID)

			create, complete, _, copyObjectCount := mock.counts()

			gotMultipart := create > 0
			if gotMultipart != tt.wantMultipart {
				t.Errorf("CreateMultipartUpload called = %v (count %d), want called = %v", gotMultipart, create, tt.wantMultipart)
			}

			if tt.wantMultipart && complete != 1 {
				t.Errorf("completeCount = %d, want 1 for the multipart path", complete)
			}

			gotCopyObjectHit := copyObjectCount > 0
			if gotCopyObjectHit != tt.wantCopyObjectHit {
				t.Errorf("plain CopyObject called = %v (count %d), want called = %v", gotCopyObjectHit, copyObjectCount, tt.wantCopyObjectHit)
			}
		})
	}
}
