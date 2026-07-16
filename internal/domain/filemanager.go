package domain

import "time"

// Bucket is a single S3 bucket as returned by ListBuckets
// (docs/02-tech-spec.md section 4.2, FR-FM-001).
type Bucket struct {
	Name         string
	CreationDate time.Time
}

// ObjectEntry is a single row in a bucket/prefix listing (FR-FM-002/003): it
// represents either a "folder" (an S3 CommonPrefix - a delimiter-collapsed
// group of keys sharing prefix Key) or a concrete object. IsFolder
// distinguishes the two; Size, ContentType, LastModified, and StorageClass
// are only meaningful for objects and are left at their zero value for
// folders.
type ObjectEntry struct {
	Key          string
	IsFolder     bool
	Size         int64
	ContentType  string
	LastModified time.Time
	// StorageClass is the S3 storage class (e.g. "STANDARD", "GLACIER").
	// Optional: left empty when not returned by/relevant to the underlying
	// ListObjectsV2 call.
	StorageClass string
}

// ListObjectsRequest is the input to FileManagerService.ListObjects
// (FR-FM-002/004/005/006).
type ListObjectsRequest struct {
	ProfileID int64
	Bucket    string
	Prefix    string
	// ContinuationToken requests the next page of a previous listing; empty
	// requests the first page.
	ContinuationToken string
	// SortBy is one of "name", "size", "type", "modified", or "" (no
	// explicit sort - server/cache default order).
	SortBy string
	// SortOrder is "asc" or "desc"; meaningless when SortBy is "".
	SortOrder string
	// Refresh, when true, discards any cached listing for this
	// profile+bucket+prefix and re-fetches from S3 instead of serving from
	// cache.
	Refresh bool
}

// ListObjectsResponse is the result of FileManagerService.ListObjects.
type ListObjectsResponse struct {
	Entries []ObjectEntry
	// NextContinuationToken is empty when there is no further page.
	NextContinuationToken string
	IsTruncated           bool
}

// ObjectMeta is the result of a HeadObject call (docs/02-tech-spec.md
// section 9.2).
type ObjectMeta struct {
	Key          string
	Size         int64
	ContentType  string
	ETag         string
	LastModified time.Time
	// Metadata holds S3 user-metadata (the "x-amz-meta-*" headers), keyed
	// without that prefix.
	Metadata map[string]string
}

// BucketSizeResult is the result of FileManagerService.GetBucketSize: the
// aggregate size and object count of everything found under a bucket/prefix
// by a recursive walk (docs backlog "Блок D — Дашборд размера бакета").
type BucketSizeResult struct {
	TotalBytes  int64
	ObjectCount int64
	// Truncated is true when the walk's time budget was exhausted before
	// every object under the bucket/prefix could be visited: TotalBytes and
	// ObjectCount hold the partial totals accumulated so far, not the true
	// totals.
	Truncated bool
}

// SearchObjectsRequest is the input to FileManagerService.SearchObjects
// (docs backlog "Блок F — Поиск по всему бакету"): unlike the client-side
// filename filter applied to an already-loaded listing page,
// SearchObjects walks the whole scope server-side so a match nested many
// folders deep - invisible to a filter over only what is currently on
// screen - is still found.
type SearchObjectsRequest struct {
	ProfileID int64
	Bucket    string
	// Prefix scopes the search to everything nested under it; empty
	// searches the whole bucket.
	Prefix string
	// Query is matched case-insensitively as a substring against each
	// candidate key's basename - the last "/"-separated segment of the key,
	// not the full key - mirroring how the frontend's own
	// getEntryDisplayName already treats a key for display purposes. A
	// search for "invoice" therefore matches "2024/reports/invoice.pdf" (the
	// basename "invoice.pdf" contains it) even though "invoice" does not
	// appear anywhere else in the path.
	Query string
}

// SearchObjectsResponse is the result of FileManagerService.SearchObjects.
type SearchObjectsResponse struct {
	Entries []ObjectEntry
	// Truncated mirrors BucketSizeResult.Truncated's meaning: either the
	// walk's time budget or its result-count budget ran out before the
	// whole scope could be covered, so Entries is a partial, not
	// authoritative, result set.
	Truncated bool
}

// TextPreviewResult is the result of FileManagerService.GetTextPreview
// (FR-FM-007): a bounded read of an object's contents for inline text
// preview.
type TextPreviewResult struct {
	Content string
	// Truncated is true when Content was cut short of the object's full
	// size (only the first slice of the object was fetched).
	Truncated bool
	// TotalSize is the object's full size, regardless of how much of it
	// Content actually holds.
	TotalSize int64
}
