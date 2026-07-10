package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"threev/internal/domain"
	"threev/internal/s3client"
)

// newTestUploadParams returns an UploadParams with Pooled/Fresh both set to
// a client pointed at serverURL (path-style, dummy static credentials -
// mirroring connection/tester_test.go's testerProfile and
// filemanager/list_test.go's saveTestProfile), a fresh CircuitBreaker, and
// Host resolved from serverURL exactly as the real caller (the future
// task.go) is expected to do. Callers still need to set LocalPath,
// TotalBytes, and any of Bucket/Key/ContentType/Concurrency/Hooks/
// ExistingUploadID they care about - the defaults here (bucket1/key1) are
// arbitrary but must match whatever the test's mock handler expects to see
// in the request path when it cares.
func newTestUploadParams(t *testing.T, serverURL string) UploadParams {
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

	return UploadParams{
		Pooled:      client,
		Fresh:       client,
		Breaker:     s3client.NewCircuitBreaker(),
		Host:        parsed.Hostname(),
		Bucket:      "bucket1",
		Key:         "key1",
		ContentType: "application/octet-stream",
	}
}

// createSparseFile creates a size-byte file in a temporary directory and
// returns its path. The file is sparse (created via Truncate, its content
// left as zero bytes on disk) rather than actually written byte-by-byte:
// these tests only care about file size (which part boundaries
// multipart_upload.go computes, how many bytes flow through
// io.NewSectionReader/io.TeeReader) and never inspect the uploaded
// content itself (the mock S3 server does not checksum bodies), so a
// sparse file lets tests exercise realistic multi-megabyte, multi-part
// uploads (matching the real docs/02-tech-spec.md section 10.2 PartSize
// table, rather than a separately parameterized "test part size") without
// the time/disk cost of actually writing megabytes of data.
func createSparseFile(t *testing.T, size int64) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "upload-source.bin")

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("os.Create(%q) returned error: %v", path, err)
	}

	if err := f.Truncate(size); err != nil {
		t.Fatalf("Truncate(%d) returned error: %v", size, err)
	}

	if err := f.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}

	return path
}

func writeXML(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

const mpuAccessDeniedBody = `<?xml version="1.0" encoding="UTF-8"?>
<Error>
  <Code>AccessDenied</Code>
  <Message>Access Denied</Message>
  <RequestId>test-request-id</RequestId>
  <HostId>test-host-id</HostId>
</Error>`

// mpuMock is a minimal S3-compatible mock server implementing just the
// multipart-upload (and plain PutObject) operations multipart_upload.go/
// upload.go exercise, dispatching purely on HTTP method + which query
// parameters are present - mirroring the real aws-sdk-go-v2 request shapes
// (verified against the vendored SDK's generated serializers.go):
//
//	CreateMultipartUpload:   POST   /bucket/key?uploads
//	UploadPart:              PUT    /bucket/key?partNumber=N&uploadId=ID
//	ListParts:               GET    /bucket/key?uploadId=ID
//	CompleteMultipartUpload: POST   /bucket/key?uploadId=ID
//	PutObject:               PUT    /bucket/key (no partNumber)
type mpuMock struct {
	uploadID string

	mu             sync.Mutex
	uploadPartHits map[int32]int
	createCount    int
	completeCount  int
	listCount      int

	// existingParts pre-seeds ListParts' response (part number -> ETag),
	// simulating parts already durably stored server-side from a prior,
	// interrupted attempt - used by the resume test.
	existingParts map[int32]string

	// failPartNumber, if non-zero, makes UploadPart respond 403
	// AccessDenied for that part number every time it is requested. auth
	// errors are never retried (s3client.isRetryable), so this fails the
	// part - and, via the errgroup, the whole upload - immediately rather
	// than only after exhausting s3client.PartRetryPolicy's real
	// (multi-second) backoff schedule, keeping this test fast without
	// weakening what it demonstrates: s3client.WithRetry's own
	// retry/backoff timing is already covered by
	// internal/s3client/retry_test.go.
	failPartNumber int32
}

func newMPUMock(uploadID string) *mpuMock {
	return &mpuMock{
		uploadID:       uploadID,
		uploadPartHits: make(map[int32]int),
		existingParts:  make(map[int32]string),
	}
}

func (m *mpuMock) counts() (create, complete, list int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.createCount, m.completeCount, m.listCount
}

func (m *mpuMock) uploadPartCount(partNumber int32) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.uploadPartHits[partNumber]
}

func (m *mpuMock) totalUploadPartRequests() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	total := 0
	for _, hits := range m.uploadPartHits {
		total += hits
	}

	return total
}

func (m *mpuMock) handler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	switch {
	case r.Method == http.MethodPost && q.Has("uploads"):
		m.handleCreate(w)

	case r.Method == http.MethodPut && q.Get("partNumber") != "":
		m.handleUploadPart(w, r, q)

	case r.Method == http.MethodGet && q.Get("uploadId") != "":
		m.handleListParts(w)

	case r.Method == http.MethodPost && q.Get("uploadId") != "":
		m.handleComplete(w, r)

	case r.Method == http.MethodPut:
		m.handlePutObject(w, r)

	default:
		http.Error(w, "mpuMock: unexpected request "+r.Method+" "+r.URL.String(), http.StatusBadRequest)
	}
}

func (m *mpuMock) handleCreate(w http.ResponseWriter) {
	m.mu.Lock()
	m.createCount++
	m.mu.Unlock()

	writeXML(w, http.StatusOK, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<InitiateMultipartUploadResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Bucket>bucket1</Bucket>
  <Key>key1</Key>
  <UploadId>%s</UploadId>
</InitiateMultipartUploadResult>`, m.uploadID))
}

func (m *mpuMock) handleUploadPart(w http.ResponseWriter, r *http.Request, q url.Values) {
	n, err := strconv.Atoi(q.Get("partNumber"))
	if err != nil {
		http.Error(w, "mpuMock: bad partNumber", http.StatusBadRequest)
		return
	}

	partNumber := int32(n)

	m.mu.Lock()
	m.uploadPartHits[partNumber]++
	m.mu.Unlock()

	// Always drain the request body fully before writing any response,
	// including the failure response below: responding while the client
	// is still mid-write of a multi-megabyte part body risks a
	// connection reset race (the client observing a broken-pipe/network
	// error while writing, rather than cleanly reading the intended
	// response) - which would flakily reclassify this handler's
	// intentional, non-retryable AccessDenied as a retryable network
	// error from s3client's perspective, undermining exactly what this
	// mock is set up to test.
	_, _ = io.Copy(io.Discard, r.Body)

	if m.failPartNumber != 0 && partNumber == m.failPartNumber {
		writeXML(w, http.StatusForbidden, mpuAccessDeniedBody)
		return
	}

	w.Header().Set("ETag", fmt.Sprintf(`"%032d"`, partNumber))
	w.WriteHeader(http.StatusOK)
}

func (m *mpuMock) handleListParts(w http.ResponseWriter) {
	m.mu.Lock()
	m.listCount++
	m.mu.Unlock()

	var parts strings.Builder

	for n, etag := range m.existingParts {
		fmt.Fprintf(&parts, `
  <Part>
    <PartNumber>%d</PartNumber>
    <LastModified>2024-01-01T00:00:00.000Z</LastModified>
    <ETag>"%s"</ETag>
    <Size>5242880</Size>
  </Part>`, n, etag)
	}

	writeXML(w, http.StatusOK, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<ListPartsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Bucket>bucket1</Bucket>
  <Key>key1</Key>
  <UploadId>%s</UploadId>
  <MaxParts>1000</MaxParts>
  <IsTruncated>false</IsTruncated>%s
</ListPartsResult>`, m.uploadID, parts.String()))
}

func (m *mpuMock) handleComplete(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	m.completeCount++
	m.mu.Unlock()

	_, _ = io.Copy(io.Discard, r.Body)

	writeXML(w, http.StatusOK, `<?xml version="1.0" encoding="UTF-8"?>
<CompleteMultipartUploadResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Location>http://bucket1.s3.amazonaws.com/key1</Location>
  <Bucket>bucket1</Bucket>
  <Key>key1</Key>
  <ETag>"final-etag-placeholder"</ETag>
</CompleteMultipartUploadResult>`)
}

func (m *mpuMock) handlePutObject(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)

	w.Header().Set("ETag", `"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`)
	w.WriteHeader(http.StatusOK)
}

// mpuTestFileSize is 11MB: with the real (unmodified) PartSize table
// (docs/02-tech-spec.md section 10.2 - files under 100MB get 5MB parts),
// this splits into exactly 3 parts (5MB, 5MB, 1MB), the "2-3 parts"
// this suite's tests want, while staying small enough (and, via
// createSparseFile, sparse enough) to be fast and cheap to create.
const mpuTestFileSize = 11 * 1024 * 1024

func TestUploadMultipartSuccess(t *testing.T) {
	t.Parallel()

	mock := newMPUMock("upload-id-success")
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	localPath := createSparseFile(t, mpuTestFileSize)

	p := newTestUploadParams(t, server.URL)
	p.LocalPath = localPath
	p.TotalBytes = mpuTestFileSize

	var (
		assignedUploadID string
		bytesTransferred int64
	)

	p.Hooks.OnMultipartUploadIDAssigned = func(uploadID string) error {
		assignedUploadID = uploadID
		return nil
	}
	p.Hooks.OnBytesTransferred = func(delta int64) {
		atomic.AddInt64(&bytesTransferred, delta)
	}

	etag, err := Upload(context.Background(), p)
	if err != nil {
		t.Fatalf("Upload() returned error: %v", err)
	}

	if etag == "" {
		t.Error("Upload() returned an empty ETag")
	}
	if assignedUploadID != mock.uploadID {
		t.Errorf("OnMultipartUploadIDAssigned got uploadID %q, want %q", assignedUploadID, mock.uploadID)
	}
	if got := mock.totalUploadPartRequests(); got != 3 {
		t.Errorf("total UploadPart requests = %d, want 3", got)
	}

	create, complete, _ := mock.counts()
	if create != 1 {
		t.Errorf("createCount = %d, want 1", create)
	}
	if complete != 1 {
		t.Errorf("completeCount = %d, want 1", complete)
	}
	if got := atomic.LoadInt64(&bytesTransferred); got != mpuTestFileSize {
		t.Errorf("bytesTransferred = %d, want %d", got, int64(mpuTestFileSize))
	}
}

// TestUploadMultipartRespectsPartSizeOverride verifies UploadParams.
// PartSizeOverride (Этап 4 суб-этап 4.3, TransferService.
// SetPartSizeOverrideMB's effect on the actual part boundaries
// uploadMultipart computes) bypasses PartSize's adaptive table entirely
// when set: mpuTestFileSize (11MB) normally splits into 3 parts under the
// unmodified table (5MB, 5MB, 1MB - see mpuTestFileSize's own doc comment),
// but with a 4MB override it must split into 3 different-sized parts
// instead (4MB, 4MB, 3MB) - same COUNT here would be a weak assertion, so
// this additionally confirms against a size where the counts differ (a 2MB
// override yields 6 parts).
func TestUploadMultipartRespectsPartSizeOverride(t *testing.T) {
	t.Parallel()

	mock := newMPUMock("upload-id-override")
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	localPath := createSparseFile(t, mpuTestFileSize)

	p := newTestUploadParams(t, server.URL)
	p.LocalPath = localPath
	p.TotalBytes = mpuTestFileSize
	p.PartSizeOverride = 2 * 1024 * 1024 // 2MB, well below the adaptive table's 5MB answer for an 11MB file

	_, err := Upload(context.Background(), p)
	if err != nil {
		t.Fatalf("Upload() returned error: %v", err)
	}

	// ceil(11MB / 2MB) = 6 parts, vs the 3 parts the unmodified adaptive
	// table would have produced for the same file size (see
	// TestUploadMultipartSuccess).
	if got := mock.totalUploadPartRequests(); got != 6 {
		t.Errorf("total UploadPart requests = %d, want 6 (PartSizeOverride must override the adaptive table)", got)
	}
}

func TestUploadMultipartResumeSkipsExistingParts(t *testing.T) {
	t.Parallel()

	mock := newMPUMock("upload-id-resume")
	mock.existingParts[1] = fmt.Sprintf("%032d", 1) // part 1 already durably uploaded from a prior attempt

	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	localPath := createSparseFile(t, mpuTestFileSize)

	p := newTestUploadParams(t, server.URL)
	p.LocalPath = localPath
	p.TotalBytes = mpuTestFileSize
	p.ExistingUploadID = mock.uploadID

	hookCalled := false
	p.Hooks.OnMultipartUploadIDAssigned = func(string) error {
		hookCalled = true
		return nil
	}

	etag, err := Upload(context.Background(), p)
	if err != nil {
		t.Fatalf("Upload() returned error: %v", err)
	}
	if etag == "" {
		t.Error("Upload() returned an empty ETag")
	}
	if hookCalled {
		t.Error("OnMultipartUploadIDAssigned was called on resume, want it skipped (no new CreateMultipartUpload)")
	}

	create, complete, list := mock.counts()
	if create != 0 {
		t.Errorf("createCount = %d, want 0 (resume must reuse ExistingUploadID, not call CreateMultipartUpload)", create)
	}
	if list == 0 {
		t.Error("listCount = 0, want ListParts to have been called at least once")
	}
	if complete != 1 {
		t.Errorf("completeCount = %d, want 1", complete)
	}

	if hits := mock.uploadPartCount(1); hits != 0 {
		t.Errorf("UploadPart called %d times for already-uploaded part 1, want 0 (must not be re-uploaded)", hits)
	}
	if hits := mock.uploadPartCount(2); hits != 1 {
		t.Errorf("UploadPart called %d times for part 2, want 1", hits)
	}
	if hits := mock.uploadPartCount(3); hits != 1 {
		t.Errorf("UploadPart called %d times for part 3, want 1", hits)
	}
}

// TestUploadMultipartPartPermanentFailureStopsOthers verifies that once one
// part fails permanently (here, a non-retryable AccessDenied - see
// mpuMock.failPartNumber's doc comment for why this, rather than
// exhausting s3client.PartRetryPolicy's real backoff schedule, is used to
// keep this test fast), uploadMultipart's errgroup worker pool actually
// stops launching the remaining parts' work rather than letting every part
// run to completion regardless of a sibling's failure.
//
// p.Concurrency is deliberately set to 1 (fully serial) so this is
// observable deterministically, without depending on real-time
// in-flight-request-cancellation behavior (which is Go's net/http's own,
// already-relied-upon responsibility, not something this package's tests
// need to re-verify): with a worker pool of size 1, uploadMultipart's part
// loop launches parts strictly in ascending PartNumber order, one at a
// time (errgroup.Group's semaphore only admits the next Go call once the
// previous goroutine has fully returned - including having already
// invoked errgroup's cancel on error). So by the time part 2 could start,
// part 1 has already failed and canceled the shared context - part 2's
// (and part 3's) client.UploadPart call therefore returns
// context.Canceled immediately, without ever reaching the mock server at
// all, which this test confirms directly via mock.uploadPartCount.
func TestUploadMultipartPartPermanentFailureStopsOthers(t *testing.T) {
	t.Parallel()

	mock := newMPUMock("upload-id-fail")
	mock.failPartNumber = 1

	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	localPath := createSparseFile(t, mpuTestFileSize)

	p := newTestUploadParams(t, server.URL)
	p.LocalPath = localPath
	p.TotalBytes = mpuTestFileSize
	p.Concurrency = 1

	_, err := Upload(context.Background(), p)
	if err == nil {
		t.Fatal("Upload() returned a nil error, want the permanently-failing part's error")
	}

	if hits := mock.uploadPartCount(1); hits != 1 {
		t.Errorf("UploadPart called %d times for the failing part 1, want 1", hits)
	}
	if hits := mock.uploadPartCount(2); hits != 0 {
		t.Errorf("UploadPart called %d times for part 2, want 0 (must never be attempted once part 1 fails)", hits)
	}
	if hits := mock.uploadPartCount(3); hits != 0 {
		t.Errorf("UploadPart called %d times for part 3, want 0 (must never be attempted once part 1 fails)", hits)
	}

	_, complete, _ := mock.counts()
	if complete != 0 {
		t.Errorf("completeCount = %d, want 0 (CompleteMultipartUpload must not run after a part permanently fails)", complete)
	}
}

func TestUploadMultipartOnMultipartUploadIDAssignedHookFailureIsFatal(t *testing.T) {
	t.Parallel()

	mock := newMPUMock("upload-id-hookfail")
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	localPath := createSparseFile(t, mpuTestFileSize)

	p := newTestUploadParams(t, server.URL)
	p.LocalPath = localPath
	p.TotalBytes = mpuTestFileSize
	p.Hooks.OnMultipartUploadIDAssigned = func(string) error {
		return errors.New("boom: simulated persistence failure")
	}

	_, err := Upload(context.Background(), p)
	if err == nil {
		t.Fatal("Upload() returned a nil error, want the hook's persistence failure to be fatal (see UploadHooks doc comment)")
	}

	if got := mock.totalUploadPartRequests(); got != 0 {
		t.Errorf("total UploadPart requests = %d, want 0 (upload must not proceed once the hook fails)", got)
	}
}

func TestAbortMultipartUpload(t *testing.T) {
	t.Parallel()

	var (
		gotMethod string
		gotQuery  url.Values
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotQuery = r.URL.Query()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	p := newTestUploadParams(t, server.URL)

	if err := AbortMultipartUpload(context.Background(), p.Pooled, "bucket1", "key1", "upload-id-abort"); err != nil {
		t.Fatalf("AbortMultipartUpload() returned error: %v", err)
	}

	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want %q", gotMethod, http.MethodDelete)
	}
	if got := gotQuery.Get("uploadId"); got != "upload-id-abort" {
		t.Errorf("uploadId query param = %q, want %q", got, "upload-id-abort")
	}
}
