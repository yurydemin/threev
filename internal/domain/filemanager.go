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
