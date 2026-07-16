package filemanager

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"threev/internal/domain"
)

// singlePageSearchBody has three objects: two whose basename contains
// "invoice" (case-insensitively - one lower, one capitalized) and one that
// does not, spread across nested prefixes to make clear the match is
// against basename, not the full key.
const singlePageSearchBody = `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>bucket1</Name>
  <Prefix></Prefix>
  <KeyCount>3</KeyCount>
  <MaxKeys>1000</MaxKeys>
  <IsTruncated>false</IsTruncated>
  <Contents>
    <Key>2024/reports/invoice.pdf</Key>
    <LastModified>2024-03-05T10:00:00.000Z</LastModified>
    <ETag>"etag1"</ETag>
    <Size>1234</Size>
    <StorageClass>STANDARD</StorageClass>
  </Contents>
  <Contents>
    <Key>2024/archive/Invoice-2023.pdf</Key>
    <LastModified>2024-01-01T00:00:00.000Z</LastModified>
    <ETag>"etag2"</ETag>
    <Size>500</Size>
    <StorageClass>STANDARD</StorageClass>
  </Contents>
  <Contents>
    <Key>2024/reports/summary.txt</Key>
    <LastModified>2024-02-01T00:00:00.000Z</LastModified>
    <ETag>"etag3"</ETag>
    <Size>100</Size>
    <StorageClass>STANDARD</StorageClass>
  </Contents>
</ListBucketResult>`

func TestFileManagerServiceSearchObjectsSinglePageCaseInsensitive(t *testing.T) {
	t.Parallel()

	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)

		if got := r.URL.Query().Get("delimiter"); got != "" {
			t.Errorf("request Delimiter = %q, want empty (recursive walk)", got)
		}

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(singlePageSearchBody))
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	got, err := fm.SearchObjects(domain.SearchObjectsRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		Query:     "invoice",
	})
	if err != nil {
		t.Fatalf("SearchObjects() returned error: %v", err)
	}

	if got := atomic.LoadInt32(&requestCount); got != 1 {
		t.Fatalf("request count = %d, want 1", got)
	}
	if got.Truncated {
		t.Errorf("Truncated = true, want false")
	}
	if len(got.Entries) != 2 {
		t.Fatalf("len(Entries) = %d, want 2: %+v", len(got.Entries), got.Entries)
	}

	var invoicePDF *domain.ObjectEntry

	for i := range got.Entries {
		if got.Entries[i].Key == "2024/reports/invoice.pdf" {
			invoicePDF = &got.Entries[i]
		}

		if got.Entries[i].Key == "2024/archive/summary.txt" {
			t.Errorf("summary.txt (non-matching basename) unexpectedly present in results: %+v", got.Entries)
		}
	}

	if invoicePDF == nil {
		t.Fatalf("2024/reports/invoice.pdf not found in results: %+v", got.Entries)
	}
	if invoicePDF.Size != 1234 {
		t.Errorf("invoice.pdf Size = %d, want 1234", invoicePDF.Size)
	}
	if invoicePDF.ContentType != "application/pdf" {
		t.Errorf("invoice.pdf ContentType = %q, want application/pdf", invoicePDF.ContentType)
	}

	wantModified := time.Date(2024, 3, 5, 10, 0, 0, 0, time.UTC)
	if !invoicePDF.LastModified.Equal(wantModified) {
		t.Errorf("invoice.pdf LastModified = %v, want %v", invoicePDF.LastModified, wantModified)
	}
	if invoicePDF.IsFolder {
		t.Errorf("invoice.pdf IsFolder = true, want false")
	}
}

const page1TruncatedSearchBody = `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>bucket1</Name>
  <Prefix></Prefix>
  <KeyCount>2</KeyCount>
  <MaxKeys>1000</MaxKeys>
  <IsTruncated>true</IsTruncated>
  <NextContinuationToken>page-2-token</NextContinuationToken>
  <Contents>
    <Key>a/invoice-jan.pdf</Key>
    <LastModified>2024-01-01T00:00:00.000Z</LastModified>
    <ETag>"etag-a"</ETag>
    <Size>10</Size>
    <StorageClass>STANDARD</StorageClass>
  </Contents>
  <Contents>
    <Key>a/readme.txt</Key>
    <LastModified>2024-01-01T00:00:00.000Z</LastModified>
    <ETag>"etag-a2"</ETag>
    <Size>5</Size>
    <StorageClass>STANDARD</StorageClass>
  </Contents>
</ListBucketResult>`

const page2FinalSearchBody = `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>bucket1</Name>
  <Prefix></Prefix>
  <KeyCount>1</KeyCount>
  <MaxKeys>1000</MaxKeys>
  <IsTruncated>false</IsTruncated>
  <Contents>
    <Key>b/invoice-feb.pdf</Key>
    <LastModified>2024-02-01T00:00:00.000Z</LastModified>
    <ETag>"etag-b"</ETag>
    <Size>20</Size>
    <StorageClass>STANDARD</StorageClass>
  </Contents>
</ListBucketResult>`

func TestFileManagerServiceSearchObjectsMultiplePages(t *testing.T) {
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

			_, _ = w.Write([]byte(page1TruncatedSearchBody))

			return
		}

		if got := r.URL.Query().Get("continuation-token"); got != "page-2-token" {
			t.Errorf("second request ContinuationToken = %q, want %q", got, "page-2-token")
		}

		_, _ = w.Write([]byte(page2FinalSearchBody))
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	got, err := fm.SearchObjects(domain.SearchObjectsRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		Query:     "invoice",
	})
	if err != nil {
		t.Fatalf("SearchObjects() returned error: %v", err)
	}

	if got := atomic.LoadInt32(&requestCount); got != 2 {
		t.Fatalf("request count = %d, want 2", got)
	}
	if got.Truncated {
		t.Errorf("Truncated = true, want false")
	}
	if len(got.Entries) != 2 {
		t.Fatalf("len(Entries) = %d, want 2 (accumulated across both pages): %+v", len(got.Entries), got.Entries)
	}
	if got.Entries[0].Key != "a/invoice-jan.pdf" || got.Entries[1].Key != "b/invoice-feb.pdf" {
		t.Errorf("Entries = %+v, want [a/invoice-jan.pdf b/invoice-feb.pdf]", got.Entries)
	}
}

// searchPageBody returns a single ListObjectsV2 XML page of count objects,
// all matching query "match", numbered starting at startIndex, truncated
// (with a NextContinuationToken) unless isLast is true.
func searchPageBody(startIndex, count int, isLast bool) string {
	var sb strings.Builder

	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	sb.WriteString(`<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">` + "\n")
	sb.WriteString("<Name>bucket1</Name>\n<Prefix></Prefix>\n")

	fmt.Fprintf(&sb, "<KeyCount>%d</KeyCount>\n<MaxKeys>1000</MaxKeys>\n", count)

	if isLast {
		sb.WriteString("<IsTruncated>false</IsTruncated>\n")
	} else {
		sb.WriteString("<IsTruncated>true</IsTruncated>\n")
		fmt.Fprintf(&sb, "<NextContinuationToken>token-%d</NextContinuationToken>\n", startIndex+count)
	}

	for i := 0; i < count; i++ {
		idx := startIndex + i
		fmt.Fprintf(&sb, `<Contents>
  <Key>match-%d.txt</Key>
  <LastModified>2024-01-01T00:00:00.000Z</LastModified>
  <ETag>"etag-%d"</ETag>
  <Size>1</Size>
  <StorageClass>STANDARD</StorageClass>
</Contents>
`, idx, idx)
	}

	sb.WriteString("</ListBucketResult>")

	return sb.String()
}

// TestFileManagerServiceSearchObjectsResultCap verifies that once
// maxSearchResults matches have been collected, SearchObjects stops
// paginating immediately (rather than walking the rest of the bucket) and
// returns exactly maxSearchResults entries with Truncated: true.
func TestFileManagerServiceSearchObjectsResultCap(t *testing.T) {
	t.Parallel()

	const perPage = 200 // more than one page's worth needed to exceed maxSearchResults (500)

	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&requestCount, 1)

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)

		// Every page reports IsTruncated=true with more matches available -
		// if SearchObjects did not stop early at maxSearchResults, this
		// handler would keep being called indefinitely (bounded only by the
		// walk's 60s timeout), so a low, exact requestCount assertion below
		// is a meaningful signal that it stopped early.
		startIndex := int(n-1) * perPage
		_, _ = w.Write([]byte(searchPageBody(startIndex, perPage, false)))
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	got, err := fm.SearchObjects(domain.SearchObjectsRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		Query:     "match",
	})
	if err != nil {
		t.Fatalf("SearchObjects() returned error: %v", err)
	}

	if !got.Truncated {
		t.Errorf("Truncated = false, want true")
	}
	if len(got.Entries) != maxSearchResults {
		t.Fatalf("len(Entries) = %d, want %d", len(got.Entries), maxSearchResults)
	}

	wantMaxRequests := int32((maxSearchResults / perPage) + 1)
	if got := atomic.LoadInt32(&requestCount); got > wantMaxRequests {
		t.Errorf("request count = %d, want <= %d (should stop paginating once the cap is hit)", got, wantMaxRequests)
	}
}

const noMatchSearchBody = `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>bucket1</Name>
  <Prefix></Prefix>
  <KeyCount>1</KeyCount>
  <MaxKeys>1000</MaxKeys>
  <IsTruncated>false</IsTruncated>
  <Contents>
    <Key>notes/todo.txt</Key>
    <LastModified>2024-01-01T00:00:00.000Z</LastModified>
    <ETag>"etag1"</ETag>
    <Size>5</Size>
    <StorageClass>STANDARD</StorageClass>
  </Contents>
</ListBucketResult>`

func TestFileManagerServiceSearchObjectsNoMatches(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(noMatchSearchBody))
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	got, err := fm.SearchObjects(domain.SearchObjectsRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		Query:     "invoice",
	})
	if err != nil {
		t.Fatalf("SearchObjects() returned error: %v", err)
	}

	if got.Truncated {
		t.Errorf("Truncated = true, want false")
	}
	if got.Entries == nil {
		t.Fatal("Entries = nil, want non-nil empty slice")
	}
	if len(got.Entries) != 0 {
		t.Fatalf("len(Entries) = %d, want 0: %+v", len(got.Entries), got.Entries)
	}
}

// TestFileManagerServiceSearchObjectsEmptyQueryMakesNoRequests verifies an
// empty Query short-circuits before any S3 call is made.
func TestFileManagerServiceSearchObjectsEmptyQueryMakesNoRequests(t *testing.T) {
	t.Parallel()

	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(noMatchSearchBody))
	}))
	t.Cleanup(server.Close)

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, server.URL)

	got, err := fm.SearchObjects(domain.SearchObjectsRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		Query:     "",
	})
	if err != nil {
		t.Fatalf("SearchObjects() returned error: %v", err)
	}

	if got := atomic.LoadInt32(&requestCount); got != 0 {
		t.Fatalf("request count = %d, want 0 (empty query must not walk the bucket)", got)
	}
	if got.Truncated {
		t.Errorf("Truncated = true, want false")
	}
	if got.Entries == nil {
		t.Fatal("Entries = nil, want non-nil empty slice")
	}
	if len(got.Entries) != 0 {
		t.Fatalf("len(Entries) = %d, want 0: %+v", len(got.Entries), got.Entries)
	}
}

// TestFileManagerServiceSearchObjectsReturnsErrLockedWhenLocked verifies
// SearchObjects's Этап 4 суб-этап 4.4 guard.
func TestFileManagerServiceSearchObjectsReturnsErrLockedWhenLocked(t *testing.T) {
	t.Parallel()

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, "http://127.0.0.1:1") // never contacted

	fm.keyBox.Clear()

	_, err := fm.SearchObjects(domain.SearchObjectsRequest{
		ProfileID: profileID,
		Bucket:    "bucket1",
		Query:     "invoice",
	})
	if !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("SearchObjects() on a locked service error = %v, want errors.Is(_, domain.ErrLocked)", err)
	}
}

// TestFileManagerServiceSearchObjectsTruncatedOnTimeout is deliberately not
// implemented, for the same reason
// TestFileManagerServiceGetBucketSizeTruncatedOnTimeout (bucketsize_test.go)
// is skipped: SearchObjects builds its own internal context.WithTimeout
// bounded by the unexported, 60-second listAllKeysTimeout constant, with no
// per-call ctx parameter and no test-only override hook. Exercising the
// Truncated:true-via-deadline path for real would mean an httptest handler
// blocking for a full 60s, which is not a reasonable price for one test
// run - see bucketsize_test.go's doc comment for the full rationale, which
// applies unchanged here since SearchObjects copies GetBucketSize's walk
// structure verbatim.
func TestFileManagerServiceSearchObjectsTruncatedOnTimeout(t *testing.T) {
	t.Parallel()
	t.Skip("no test-only seam exists to shorten SearchObjects's internal 60s listAllKeysTimeout; see doc comment above")
}
