package transfer

import (
	"context"
	"crypto/md5" //nolint:gosec // test-only content hashing to build a realistic single-part ETag, mirroring etag.go's own package-level rationale.
	"encoding/hex"
	"fmt"
	"math/rand"
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
	"time"

	"threev/internal/domain"
	"threev/internal/s3client"
)

// newTestDownloadParams returns a DownloadParams with Pooled/Fresh both set
// to a client pointed at serverURL (path-style, dummy static credentials),
// mirroring multipart_upload_test.go's newTestUploadParams exactly - see
// its doc comment for the rationale. Callers still need to set LocalPath
// and any of Bucket/Key/Concurrency/Hooks they care about.
func newTestDownloadParams(t *testing.T, serverURL string) DownloadParams {
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

	return DownloadParams{
		Pooled:  client,
		Fresh:   client,
		Breaker: s3client.NewCircuitBreaker(),
		Host:    parsed.Hostname(),
		Bucket:  "bucket1",
		Key:     "key1",
	}
}

// randomContent returns size deterministically-random bytes (seeded, so
// tests are reproducible), used as the mock object body for
// range_download_test.go's tests - real content (not a sparse/zero-filled
// file, unlike multipart_upload_test.go's createSparseFile) is needed here
// because these tests verify the downloaded bytes actually match what the
// mock server served, both directly (byte comparison) and via
// verifyDownloadIntegrity's MD5 check.
func randomContent(size int) []byte {
	content := make([]byte, size)
	rand.New(rand.NewSource(42)).Read(content) //nolint:gosec // deterministic test fixture content, not security-sensitive

	return content
}

// rangeMock is a minimal S3-compatible mock server implementing just the
// HeadObject and Range-GetObject operations range_download.go/download.go
// exercise: HeadObject -> HTTP HEAD /bucket/key, GetObject -> HTTP GET
// /bucket/key with a Range header (verified against the vendored SDK's
// generated serializers.go, mirroring mpuMock's own doc comment in
// multipart_upload_test.go).
type rangeMock struct {
	content []byte
	etag    string // already stripped of quotes

	mu        sync.Mutex
	headCount int
	getRanges []string // Range header value of every GetObject request, in arrival order

	// failOffset/failEnabled make a GetObject request whose Range starts
	// at failOffset respond 403 AccessDenied every time - auth errors are
	// never retried (s3client.isRetryable), so this fails that segment
	// (and, via the errgroup, the whole download) immediately rather than
	// only after exhausting s3client.PartRetryPolicy's real backoff
	// schedule, keeping the test fast without weakening what it
	// demonstrates - identical rationale to mpuMock.failPartNumber.
	failOffset  int64
	failEnabled bool

	// blockOffset/blockEnabled make a GetObject request whose Range starts
	// at blockOffset hang until the request's own context is canceled
	// (rather than ever completing normally) - used by
	// TestDownloadInterruptedResumeDoesNotSkipUnfinishedSegments to
	// simulate a real mid-segment interruption (a user cancel or a process
	// crash) deterministically: the test waits on blockStarted (closed via
	// blockOnce exactly once, the moment this request arrives) before
	// canceling the download's context, guaranteeing the interruption
	// happens strictly after every earlier segment has already finished
	// (see that test's doc comment for the full sequencing argument).
	blockOffset  int64
	blockEnabled bool
	blockOnce    sync.Once
	blockStarted chan struct{}
}

func newRangeMock(content []byte) *rangeMock {
	sum := md5.Sum(content) //nolint:gosec // see randomContent's doc comment

	return &rangeMock{
		content:      content,
		etag:         hex.EncodeToString(sum[:]),
		blockStarted: make(chan struct{}),
	}
}

func (m *rangeMock) getRangeCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.getRanges)
}

func (m *rangeMock) getRangesSnapshot() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]string, len(m.getRanges))
	copy(out, m.getRanges)

	return out
}

func (m *rangeMock) handler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodHead:
		m.handleHead(w)
	case http.MethodGet:
		m.handleGet(w, r)
	default:
		http.Error(w, "rangeMock: unexpected method "+r.Method, http.StatusBadRequest)
	}
}

func (m *rangeMock) handleHead(w http.ResponseWriter) {
	m.mu.Lock()
	m.headCount++
	m.mu.Unlock()

	w.Header().Set("Content-Length", strconv.Itoa(len(m.content)))
	w.Header().Set("ETag", fmt.Sprintf(`"%s"`, m.etag))
	w.WriteHeader(http.StatusOK)
}

func (m *rangeMock) handleGet(w http.ResponseWriter, r *http.Request) {
	rangeHeader := r.Header.Get("Range")

	start, end, err := parseTestRangeHeader(rangeHeader, int64(len(m.content)))
	if err != nil {
		http.Error(w, "rangeMock: bad Range header "+rangeHeader+": "+err.Error(), http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	m.getRanges = append(m.getRanges, rangeHeader)
	fail := m.failEnabled && start == m.failOffset
	block := m.blockEnabled && start == m.blockOffset
	m.mu.Unlock()

	if fail {
		writeXML(w, http.StatusForbidden, mpuAccessDeniedBody) // reuse from multipart_upload_test.go, same package
		return
	}

	if block {
		m.blockOnce.Do(func() { close(m.blockStarted) })
		<-r.Context().Done() // hang until the client cancels, simulating an interrupted in-flight segment
		return
	}

	body := m.content[start : end+1]

	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(m.content)))
	w.Header().Set("ETag", fmt.Sprintf(`"%s"`, m.etag))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(body)
}

// parseTestRangeHeader parses the "bytes=start-end" form range_download.go
// always sends (downloadSegment never sends an open-ended "bytes=start-"
// range), clamping end to contentLen-1 defensively.
func parseTestRangeHeader(header string, contentLen int64) (start, end int64, err error) {
	const prefix = "bytes="
	if !strings.HasPrefix(header, prefix) {
		return 0, 0, fmt.Errorf("missing %q prefix", prefix)
	}

	parts := strings.SplitN(strings.TrimPrefix(header, prefix), "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected start-end")
	}

	start, err = strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse start: %w", err)
	}

	end, err = strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse end: %w", err)
	}

	if end >= contentLen {
		end = contentLen - 1
	}

	return start, end, nil
}

// downloadTestFileSize is 11MB: with the real (unmodified) PartSize table
// (docs/02-tech-spec.md section 10.2/10.3 - files under 100MB get 5MB
// segments), this splits into exactly 3 segments (5MB, 5MB, 1MB) -
// identical reasoning to multipart_upload_test.go's mpuTestFileSize, and
// deliberately the same value so both suites exercise the same real
// PartSize table boundary.
const downloadTestFileSize = 11 * 1024 * 1024

func TestDownloadSuccessMultiSegment(t *testing.T) {
	t.Parallel()

	content := randomContent(downloadTestFileSize)
	mock := newRangeMock(content)

	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	localPath := filepath.Join(t.TempDir(), "downloaded.bin")

	p := newTestDownloadParams(t, server.URL)
	p.LocalPath = localPath

	var bytesTransferred int64
	p.Hooks.OnBytesTransferred = func(delta int64) {
		atomic.AddInt64(&bytesTransferred, delta)
	}

	etag, err := Download(context.Background(), p)
	if err != nil {
		t.Fatalf("Download() returned error: %v", err)
	}

	if etag != mock.etag {
		t.Errorf("Download() ETag = %q, want %q", etag, mock.etag)
	}

	if got := mock.getRangeCount(); got != 3 {
		t.Errorf("GetObject request count = %d, want 3", got)
	}

	if got := atomic.LoadInt64(&bytesTransferred); got != downloadTestFileSize {
		t.Errorf("bytesTransferred = %d, want %d", got, int64(downloadTestFileSize))
	}

	got, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) returned error: %v", localPath, err)
	}

	if string(got) != string(content) {
		t.Error("downloaded file content does not match the source object's content")
	}
}

// TestDownloadResumeSkipsCompletedSegments verifies resume driven by the
// resume-progress sidecar file (not local file size, see
// progressSidecarSuffix's doc comment): a sidecar recording the first
// segment as already completed means the plan must skip it entirely and
// only request the other two segments' FULL ranges (never a partial tail -
// resume tracking is whole-segment granularity, see planDownloadSegments's
// doc comment).
func TestDownloadResumeSkipsCompletedSegments(t *testing.T) {
	t.Parallel()

	content := randomContent(downloadTestFileSize)
	mock := newRangeMock(content)

	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	// Segment layout for an 11MB file at the real 5MB part size: [0,5MB),
	// [5MB,10MB), [10MB,11MB). Simulate a previous, interrupted run that
	// already durably finished the first segment: its bytes are already on
	// disk (exactly as downloadSegment's WriteAt would have left them) and
	// the sidecar records offset 0 as completed.
	const firstSegmentSize = 5 * 1024 * 1024

	localPath := filepath.Join(t.TempDir(), "downloaded.bin")
	if err := os.WriteFile(localPath, content[:firstSegmentSize], 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) returned error: %v", localPath, err)
	}

	if err := os.WriteFile(progressSidecarPath(localPath), []byte("0\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) returned error: %v", progressSidecarPath(localPath), err)
	}

	p := newTestDownloadParams(t, server.URL)
	p.LocalPath = localPath

	etag, err := Download(context.Background(), p)
	if err != nil {
		t.Fatalf("Download() returned error: %v", err)
	}

	if etag != mock.etag {
		t.Errorf("Download() ETag = %q, want %q", etag, mock.etag)
	}

	wantRanges := []string{"bytes=5242880-10485759", "bytes=10485760-11534335"}
	gotRanges := mock.getRangesSnapshot()

	if len(gotRanges) != len(wantRanges) {
		t.Fatalf("GetObject requests = %v, want exactly %v", gotRanges, wantRanges)
	}

	seen := make(map[string]bool, len(gotRanges))
	for _, r := range gotRanges {
		seen[r] = true
	}

	for _, want := range wantRanges {
		if !seen[want] {
			t.Errorf("GetObject requests %v missing expected range %q (already-completed segments must not be re-requested)", gotRanges, want)
		}
	}

	got, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) returned error: %v", localPath, err)
	}

	if string(got) != string(content) {
		t.Error("resumed download's final file content does not match the source object's content")
	}

	if _, statErr := os.Stat(progressSidecarPath(localPath)); !os.IsNotExist(statErr) {
		t.Errorf("progress sidecar file still exists after a fully successful download, want it removed (got stat error: %v)", statErr)
	}
}

// TestDownloadInterruptedResumeDoesNotSkipUnfinishedSegments is a direct
// regression test for the bug the resume-progress sidecar mechanism
// (progressSidecarSuffix) was written to fix, originally reproduced
// manually against a real MinIO server: a resume check based on the local
// file's own size (os.Stat(LocalPath).Size()) always reports totalBytes
// immediately after downloadRange's very first file.Truncate(totalBytes)
// call - long before most segments have actually been transferred - so a
// resumed Download() of a genuinely partially-downloaded file wrongly
// concluded "already complete" and made zero GetObject requests, leaving
// most of the file as untouched, zero-filled sparse-hole bytes.
//
// This test drives the real interruption sequence end to end:
//
//  1. start Download() with Concurrency=1 (so segments run strictly one at
//     a time, in ascending-offset order - identical technique to
//     TestDownloadSegmentPermanentFailureStopsOthers) against a mock
//     server configured to hang the GetObject request for the SECOND
//     segment (mock.blockOffset) until its context is canceled;
//  2. wait for that second segment's request to actually arrive at the
//     mock (mock.blockStarted) - by the errgroup SetLimit(1) semantics
//     this is only possible once the first segment's downloadSegment call,
//     AND its subsequent progressSidecar.recordCompleted call, have both
//     already returned (a limit-1 errgroup only admits the next Go() call
//     once the previous goroutine has fully returned) - then cancel the
//     download's context, simulating a user cancel/process crash
//     mid-segment;
//  3. assert Download() returned a non-nil error, and that the
//     resume-progress sidecar file exists and contains EXACTLY the first
//     segment's offset (0) - not the second (which never finished) or
//     third (never even started);
//  4. call Download() again, for the SAME LocalPath, with a fresh,
//     non-canceled context (and blocking disabled on the mock) - assert it
//     makes NEW GetObject requests for the two unfinished segments (never
//     zero requests, which is exactly what the bug produced), succeeds,
//     and the sidecar file is removed;
//  5. assert the final file's content is byte-for-byte identical to the
//     source object's content.
func TestDownloadInterruptedResumeDoesNotSkipUnfinishedSegments(t *testing.T) {
	t.Parallel()

	content := randomContent(downloadTestFileSize)
	mock := newRangeMock(content)
	mock.blockEnabled = true
	mock.blockOffset = 5 * 1024 * 1024 // hang the second segment's GetObject request

	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	localPath := filepath.Join(t.TempDir(), "downloaded.bin")

	p := newTestDownloadParams(t, server.URL)
	p.LocalPath = localPath
	p.Concurrency = 1

	ctx, cancel := context.WithCancel(context.Background())

	resultCh := make(chan error, 1)
	go func() {
		_, downloadErr := Download(ctx, p)
		resultCh <- downloadErr
	}()

	select {
	case <-mock.blockStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the second segment's GetObject request to arrive")
	}

	cancel()

	var firstErr error
	select {
	case firstErr = <-resultCh:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the interrupted Download() call to return")
	}

	if firstErr == nil {
		t.Fatal("interrupted Download() returned a nil error, want a non-nil error from the canceled context")
	}

	sidecarData, err := os.ReadFile(progressSidecarPath(localPath))
	if err != nil {
		t.Fatalf("os.ReadFile(%q) returned error: %v (want the sidecar file to exist and record the first segment as completed)", progressSidecarPath(localPath), err)
	}

	if got := strings.TrimSpace(string(sidecarData)); got != "0" {
		t.Fatalf("progress sidecar content = %q, want exactly \"0\" (only the first segment finished before interruption)", got)
	}

	firstCallRangeCount := mock.getRangeCount()
	if firstCallRangeCount != 2 {
		t.Fatalf("GetObject request count after interruption = %d, want 2 (the completed first segment, plus the second segment's request that was hung and then canceled - the never-attempted third segment must not add a third)", firstCallRangeCount)
	}

	// Second call: same LocalPath, fresh context, blocking disabled -
	// nothing else about the interrupted first call's state is touched.
	mock.blockEnabled = false

	etag, err := Download(context.Background(), p)
	if err != nil {
		t.Fatalf("resumed Download() returned error: %v", err)
	}

	if etag != mock.etag {
		t.Errorf("resumed Download() ETag = %q, want %q", etag, mock.etag)
	}

	secondCallNewRanges := mock.getRangeCount() - firstCallRangeCount
	if secondCallNewRanges == 0 {
		t.Fatal("resumed Download() made 0 new GetObject requests - this is exactly the bug being regression-tested: a partially-downloaded file must never be mistaken for a fully-downloaded one")
	}

	if secondCallNewRanges != 2 {
		t.Errorf("resumed Download() made %d new GetObject requests, want 2 (the second and third segments, which never finished on the first call)", secondCallNewRanges)
	}

	if _, statErr := os.Stat(progressSidecarPath(localPath)); !os.IsNotExist(statErr) {
		t.Errorf("progress sidecar file still exists after the resumed download succeeded, want it removed (got stat error: %v)", statErr)
	}

	got, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) returned error: %v", localPath, err)
	}

	if string(got) != string(content) {
		t.Error("final downloaded file content does not match the source object's content")
	}
}

// TestDownloadSegmentPermanentFailureStopsOthers verifies that once one
// segment fails permanently (a non-retryable AccessDenied - see
// rangeMock.failOffset's doc comment for why this, rather than exhausting
// s3client.PartRetryPolicy's real backoff schedule, is used to keep this
// test fast), downloadRange's errgroup worker pool stops launching the
// remaining segments' work - mirroring
// TestUploadMultipartPartPermanentFailureStopsOthers's reasoning and
// Concurrency=1 technique exactly (see its doc comment in
// multipart_upload_test.go).
func TestDownloadSegmentPermanentFailureStopsOthers(t *testing.T) {
	t.Parallel()

	content := randomContent(downloadTestFileSize)
	mock := newRangeMock(content)
	mock.failEnabled = true
	mock.failOffset = 0 // fail the first (offset-0) segment

	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	localPath := filepath.Join(t.TempDir(), "downloaded.bin")

	p := newTestDownloadParams(t, server.URL)
	p.LocalPath = localPath
	p.Concurrency = 1

	_, err := Download(context.Background(), p)
	if err == nil {
		t.Fatal("Download() returned a nil error, want the permanently-failing segment's error")
	}

	if got := mock.getRangeCount(); got != 1 {
		t.Errorf("GetObject request count = %d, want 1 (only the failing first segment; later segments must never be attempted)", got)
	}
}

// TestDownloadWithoutSidecarRedownloadsEvenIfFileAlreadyComplete documents
// and verifies the intentional safe-default behavior described in
// readCompletedSegmentOffsets's doc comment: a local file that already
// holds the object's full content, but has no resume-progress sidecar file
// next to it (e.g. leftover content from before this sidecar mechanism
// existed, or simply unrelated content that happens to be the right size),
// is NOT trusted as "already downloaded" - Download re-fetches every
// segment in that case. This is deliberately different from ever using the
// local file's own size as a resume signal (the bug the sidecar mechanism
// was written to fix, see progressSidecarSuffix's doc comment): a few extra
// megabytes of redundant download is an acceptable cost; trusting an
// untracked local file's size is not.
func TestDownloadWithoutSidecarRedownloadsEvenIfFileAlreadyComplete(t *testing.T) {
	t.Parallel()

	content := randomContent(downloadTestFileSize)
	mock := newRangeMock(content)

	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	localPath := filepath.Join(t.TempDir(), "downloaded.bin")
	if err := os.WriteFile(localPath, content, 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) returned error: %v", localPath, err)
	}

	p := newTestDownloadParams(t, server.URL)
	p.LocalPath = localPath

	etag, err := Download(context.Background(), p)
	if err != nil {
		t.Fatalf("Download() returned error: %v", err)
	}

	if etag != mock.etag {
		t.Errorf("Download() ETag = %q, want %q", etag, mock.etag)
	}

	if got := mock.getRangeCount(); got != 3 {
		t.Errorf("GetObject request count = %d, want 3 (no sidecar file present, so every segment must be re-requested rather than trusting the pre-existing local file's size)", got)
	}

	if got := mock.headCount; got != 1 {
		t.Errorf("HeadObject request count = %d, want 1 (Download must still HEAD to learn size/ETag before planning segments)", got)
	}

	got, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) returned error: %v", localPath, err)
	}

	if string(got) != string(content) {
		t.Error("downloaded file content does not match the source object's content")
	}
}

// TestDownloadSidecarCoveringAllSegmentsMakesNoGetRequests verifies the
// resume-complete fast path: a resume-progress sidecar file that already
// records every segment as completed means Download returns without making
// any GetObject request at all - and cleans up the now-redundant sidecar
// file, mirroring the old (localSize-based, since removed) "already fully
// downloaded" fast path but driven by the sidecar instead.
func TestDownloadSidecarCoveringAllSegmentsMakesNoGetRequests(t *testing.T) {
	t.Parallel()

	content := randomContent(downloadTestFileSize)
	mock := newRangeMock(content)

	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	localPath := filepath.Join(t.TempDir(), "downloaded.bin")
	if err := os.WriteFile(localPath, content, 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) returned error: %v", localPath, err)
	}

	// Offsets of all 3 segments (5MB, 5MB, 1MB) for an 11MB file.
	sidecar := "0\n5242880\n10485760\n"
	if err := os.WriteFile(progressSidecarPath(localPath), []byte(sidecar), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) returned error: %v", progressSidecarPath(localPath), err)
	}

	p := newTestDownloadParams(t, server.URL)
	p.LocalPath = localPath

	etag, err := Download(context.Background(), p)
	if err != nil {
		t.Fatalf("Download() returned error: %v", err)
	}

	if etag != mock.etag {
		t.Errorf("Download() ETag = %q, want %q", etag, mock.etag)
	}

	if got := mock.getRangeCount(); got != 0 {
		t.Errorf("GetObject request count = %d, want 0 (sidecar already records every segment as completed)", got)
	}

	if got := mock.headCount; got != 1 {
		t.Errorf("HeadObject request count = %d, want 1 (Download must still HEAD to learn size/ETag before checking sidecar/plan state)", got)
	}

	if _, statErr := os.Stat(progressSidecarPath(localPath)); !os.IsNotExist(statErr) {
		t.Errorf("progress sidecar file still exists after Download() found every segment already complete, want it removed (got stat error: %v)", statErr)
	}
}

func TestPlanDownloadSegments(t *testing.T) {
	t.Parallel()

	const totalBytes = downloadTestFileSize // 11MB -> 5MB, 5MB, 1MB segments

	tests := []struct {
		name      string
		completed map[int64]struct{}
		want      []downloadSegmentPlan
	}{
		{
			name:      "from scratch (nil completed set)",
			completed: nil,
			want: []downloadSegmentPlan{
				{offset: 0, size: 5 * 1024 * 1024},
				{offset: 5 * 1024 * 1024, size: 5 * 1024 * 1024},
				{offset: 10 * 1024 * 1024, size: 1 * 1024 * 1024},
			},
		},
		{
			name:      "from scratch (empty completed set)",
			completed: map[int64]struct{}{},
			want: []downloadSegmentPlan{
				{offset: 0, size: 5 * 1024 * 1024},
				{offset: 5 * 1024 * 1024, size: 5 * 1024 * 1024},
				{offset: 10 * 1024 * 1024, size: 1 * 1024 * 1024},
			},
		},
		{
			name:      "first segment already completed",
			completed: map[int64]struct{}{0: {}},
			want: []downloadSegmentPlan{
				{offset: 5 * 1024 * 1024, size: 5 * 1024 * 1024},
				{offset: 10 * 1024 * 1024, size: 1 * 1024 * 1024},
			},
		},
		{
			name:      "first two segments already completed",
			completed: map[int64]struct{}{0: {}, 5 * 1024 * 1024: {}},
			want: []downloadSegmentPlan{
				{offset: 10 * 1024 * 1024, size: 1 * 1024 * 1024},
			},
		},
		{
			name:      "every segment already completed",
			completed: map[int64]struct{}{0: {}, 5 * 1024 * 1024: {}, 10 * 1024 * 1024: {}},
			want:      []downloadSegmentPlan{},
		},
		{
			name:      "an offset not aligned to any real segment boundary is simply ignored",
			completed: map[int64]struct{}{123: {}},
			want: []downloadSegmentPlan{
				{offset: 0, size: 5 * 1024 * 1024},
				{offset: 5 * 1024 * 1024, size: 5 * 1024 * 1024},
				{offset: 10 * 1024 * 1024, size: 1 * 1024 * 1024},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := planDownloadSegments(totalBytes, 0, tt.completed)

			if len(got) != len(tt.want) {
				t.Fatalf("planDownloadSegments(%d, %v) = %+v, want %+v", totalBytes, tt.completed, got, tt.want)
			}

			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("planDownloadSegments(%d, %v)[%d] = %+v, want %+v", totalBytes, tt.completed, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestPlanDownloadSegmentsWithOverride verifies planDownloadSegments'
// partSizeOverride parameter (Этап 4 суб-этап 4.3, DownloadParams.
// PartSizeOverride) bypasses PartSize's adaptive table when > 0, laying out
// completely different segment boundaries than the same totalBytes would
// get with partSizeOverride == 0 (TestPlanDownloadSegments' "from scratch"
// case, which produces 5MB/5MB/1MB for this exact totalBytes).
func TestPlanDownloadSegmentsWithOverride(t *testing.T) {
	t.Parallel()

	const totalBytes = downloadTestFileSize // 11MB

	got := planDownloadSegments(totalBytes, 4*1024*1024, map[int64]struct{}{})

	want := []downloadSegmentPlan{
		{offset: 0, size: 4 * 1024 * 1024},
		{offset: 4 * 1024 * 1024, size: 4 * 1024 * 1024},
		{offset: 8 * 1024 * 1024, size: 3 * 1024 * 1024},
	}

	if len(got) != len(want) {
		t.Fatalf("planDownloadSegments(%d, 4MB override, {}) = %+v, want %+v", totalBytes, got, want)
	}

	for i := range got {
		if got[i] != want[i] {
			t.Errorf("planDownloadSegments(%d, 4MB override, {})[%d] = %+v, want %+v", totalBytes, i, got[i], want[i])
		}
	}
}

// TestPlanDownloadSegmentsZeroOverrideUsesAdaptiveTable verifies that
// passing partSizeOverride == 0 reproduces the exact same, override-less
// adaptive-table layout as before this parameter existed - a direct
// regression guard for every existing planDownloadSegments call site
// (downloadRange) that always passes p.PartSizeOverride, which is 0 unless
// a caller explicitly sets it.
func TestPlanDownloadSegmentsZeroOverrideUsesAdaptiveTable(t *testing.T) {
	t.Parallel()

	const totalBytes = downloadTestFileSize // 11MB -> 5MB, 5MB, 1MB segments

	got := planDownloadSegments(totalBytes, 0, map[int64]struct{}{})

	want := []downloadSegmentPlan{
		{offset: 0, size: 5 * 1024 * 1024},
		{offset: 5 * 1024 * 1024, size: 5 * 1024 * 1024},
		{offset: 10 * 1024 * 1024, size: 1 * 1024 * 1024},
	}

	if len(got) != len(want) {
		t.Fatalf("planDownloadSegments(%d, 0, {}) = %+v, want %+v", totalBytes, got, want)
	}

	for i := range got {
		if got[i] != want[i] {
			t.Errorf("planDownloadSegments(%d, 0, {})[%d] = %+v, want %+v", totalBytes, i, got[i], want[i])
		}
	}
}

func TestReadCompletedSegmentOffsets(t *testing.T) {
	t.Parallel()

	t.Run("no sidecar file", func(t *testing.T) {
		t.Parallel()

		localPath := filepath.Join(t.TempDir(), "downloaded.bin")

		got, err := readCompletedSegmentOffsets(localPath)
		if err != nil {
			t.Fatalf("readCompletedSegmentOffsets() returned error: %v", err)
		}

		if len(got) != 0 {
			t.Errorf("readCompletedSegmentOffsets() = %v, want an empty set when no sidecar file exists", got)
		}
	})

	t.Run("valid offsets", func(t *testing.T) {
		t.Parallel()

		localPath := filepath.Join(t.TempDir(), "downloaded.bin")
		if err := os.WriteFile(progressSidecarPath(localPath), []byte("0\n5242880\n"), 0o600); err != nil {
			t.Fatalf("os.WriteFile() returned error: %v", err)
		}

		got, err := readCompletedSegmentOffsets(localPath)
		if err != nil {
			t.Fatalf("readCompletedSegmentOffsets() returned error: %v", err)
		}

		want := map[int64]struct{}{0: {}, 5242880: {}}
		if len(got) != len(want) {
			t.Fatalf("readCompletedSegmentOffsets() = %v, want %v", got, want)
		}

		for offset := range want {
			if _, ok := got[offset]; !ok {
				t.Errorf("readCompletedSegmentOffsets() = %v, missing expected offset %d", got, offset)
			}
		}
	})

	t.Run("malformed lines are skipped, not fatal", func(t *testing.T) {
		t.Parallel()

		localPath := filepath.Join(t.TempDir(), "downloaded.bin")
		if err := os.WriteFile(progressSidecarPath(localPath), []byte("0\nnot-a-number\n\n5242880"), 0o600); err != nil {
			t.Fatalf("os.WriteFile() returned error: %v", err)
		}

		got, err := readCompletedSegmentOffsets(localPath)
		if err != nil {
			t.Fatalf("readCompletedSegmentOffsets() returned error: %v", err)
		}

		want := map[int64]struct{}{0: {}, 5242880: {}}
		if len(got) != len(want) {
			t.Fatalf("readCompletedSegmentOffsets() = %v, want %v (malformed/blank lines skipped)", got, want)
		}

		for offset := range want {
			if _, ok := got[offset]; !ok {
				t.Errorf("readCompletedSegmentOffsets() = %v, missing expected offset %d", got, offset)
			}
		}
	})
}

// TestProgressSidecarConcurrentRecordCompleted verifies progressSidecar's
// mutex-protected writes (progressSidecar.recordCompleted) are safe under
// real concurrent access from many goroutines at once (exactly how
// downloadRange's worker pool calls it, one call per completed segment) and
// that every one of their offsets ends up durably recorded, with no
// interleaved/corrupted lines - run with -race, this also directly
// exercises the data-race safety the mutex exists for.
func TestProgressSidecarConcurrentRecordCompleted(t *testing.T) {
	t.Parallel()

	localPath := filepath.Join(t.TempDir(), "downloaded.bin")

	sidecar, err := newProgressSidecar(localPath)
	if err != nil {
		t.Fatalf("newProgressSidecar() returned error: %v", err)
	}

	const goroutines = 50

	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)

		go func(offset int64) {
			defer wg.Done()

			if recordErr := sidecar.recordCompleted(offset); recordErr != nil {
				t.Errorf("recordCompleted(%d) returned error: %v", offset, recordErr)
			}
		}(int64(i) * 1024)
	}

	wg.Wait()

	if err := sidecar.close(); err != nil {
		t.Fatalf("close() returned error: %v", err)
	}

	got, err := readCompletedSegmentOffsets(localPath)
	if err != nil {
		t.Fatalf("readCompletedSegmentOffsets() returned error: %v", err)
	}

	if len(got) != goroutines {
		t.Fatalf("readCompletedSegmentOffsets() returned %d offsets, want %d (no lost or corrupted concurrent writes)", len(got), goroutines)
	}

	for i := 0; i < goroutines; i++ {
		offset := int64(i) * 1024
		if _, ok := got[offset]; !ok {
			t.Errorf("readCompletedSegmentOffsets() missing offset %d", offset)
		}
	}
}

func TestVerifyDownloadIntegritySinglePartETag(t *testing.T) {
	t.Parallel()

	content := []byte("hello, threev download integrity check")
	sum := md5.Sum(content) //nolint:gosec // see randomContent's doc comment
	matchingETag := hex.EncodeToString(sum[:])

	path := filepath.Join(t.TempDir(), "file.bin")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("os.WriteFile() returned error: %v", err)
	}

	t.Run("matching digest", func(t *testing.T) {
		t.Parallel()

		verified, err := verifyDownloadIntegrity(path, matchingETag, int64(len(content)))
		if err != nil {
			t.Fatalf("verifyDownloadIntegrity() returned error: %v", err)
		}
		if !verified {
			t.Error("verifyDownloadIntegrity() verified = false, want true for a matching MD5 ETag")
		}
	})

	t.Run("mismatching digest", func(t *testing.T) {
		t.Parallel()

		wrongETag := strings.Repeat("a", 32)

		verified, err := verifyDownloadIntegrity(path, wrongETag, int64(len(content)))
		if err != nil {
			t.Fatalf("verifyDownloadIntegrity() returned error: %v", err)
		}
		if verified {
			t.Error("verifyDownloadIntegrity() verified = true, want false for a mismatching MD5 ETag")
		}
	})
}

func TestVerifyDownloadIntegrityMultipartSourceETag(t *testing.T) {
	t.Parallel()

	content := []byte("some multipart-sourced object content")
	compositeETag := strings.Repeat("b", 32) + "-3" // format only, not a real composite digest - byte verification is intentionally skipped for this format

	t.Run("matching size", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "file.bin")
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatalf("os.WriteFile() returned error: %v", err)
		}

		verified, err := verifyDownloadIntegrity(path, compositeETag, int64(len(content)))
		if err != nil {
			t.Fatalf("verifyDownloadIntegrity() returned error: %v, want nil (size matches, MD5 verification is simply skipped for this ETag format)", err)
		}
		if verified {
			t.Error("verifyDownloadIntegrity() verified = true, want false (a multipart-source ETag can never be byte-verified)")
		}
	})

	t.Run("mismatching size (incomplete download)", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "file.bin")
		if err := os.WriteFile(path, content[:len(content)-5], 0o600); err != nil {
			t.Fatalf("os.WriteFile() returned error: %v", err)
		}

		_, err := verifyDownloadIntegrity(path, compositeETag, int64(len(content)))
		if err == nil {
			t.Fatal("verifyDownloadIntegrity() returned a nil error, want a fatal error: an incomplete file on disk is a genuine bug, not merely 'verification not applicable'")
		}
	})
}
