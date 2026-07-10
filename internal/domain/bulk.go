package domain

// DeleteObjectsRequest is the input to FileManagerService.DeleteObjects.
//
// Keys are concrete object keys only (never folder/CommonPrefix entries,
// i.e. never an ObjectEntry with IsFolder == true) - the multi-select
// selection this request is built from (Stage 4 Block C, frontend) never
// offers a checkbox for a folder row in the Object List in the first place,
// so a folder key is not expected to reach this type in practice. Recursive
// folder deletion (enumerating every key nested under a "folder" prefix, the
// way transfer.QueueDownloadPrefix already does for downloads) is a
// separate, more involved feature, deliberately out of scope for this MVP -
// backlog.
type DeleteObjectsRequest struct {
	ProfileID int64
	Bucket    string
	Keys      []string
}

// BulkCopyRequest is the input to FileManagerService.CopyObjects.
//
// Keys are concrete object keys only, for the same reason documented on
// DeleteObjectsRequest (no folder/CommonPrefix entries ever reach this
// type). All Keys are treated as flat siblings - no relative sub-path
// between them is preserved - each resulting destination key is simply
// DestPrefix + basename(sourceKey). Copying/moving an entire "folder"
// (preserving its nested structure under DestPrefix) is, like recursive
// delete, out of scope for this MVP - backlog.
type BulkCopyRequest struct {
	ProfileID    int64
	SourceBucket string
	Keys         []string
	DestBucket   string
	DestPrefix   string
}

// BulkMoveRequest has the identical shape to BulkCopyRequest (see its doc
// comment for the same Keys/flat-siblings caveats) - kept as a separate
// named type (not a type alias) so MoveObjects' signature stays
// self-documenting and the two can never be silently interchanged by a
// caller typo.
type BulkMoveRequest struct {
	ProfileID    int64
	SourceBucket string
	Keys         []string
	DestBucket   string
	DestPrefix   string
}

// BulkOperationError is a single per-key failure within an otherwise
// possibly-successful bulk delete/copy/move operation.
type BulkOperationError struct {
	Key     string
	Message string
}

// BulkOperationResult is the final, synchronously-unavailable result of an
// async bulk operation - callers observe it either via the terminal
// "bulk:progress" event (Status "completed"/"cancelled") or are not
// expected to poll for it separately in this MVP (no GetBulkOperation
// getter - see the Этап 4 plan's architecture section).
type BulkOperationResult struct {
	OperationID int64
	Total       int
	Succeeded   int
	Failed      []BulkOperationError
	Cancelled   bool
}

// BulkOperationProgressEvent is the payload of the Wails "bulk:progress"
// event (by direct analogy with TransferProgressEvent/"transfer:progress"),
// published as a bulk delete/copy/move operation runs.
type BulkOperationProgressEvent struct {
	OperationID int64
	// Type is "delete" | "copy" | "move".
	Type        string
	Total       int
	Completed   int
	FailedCount int
	// Status is "running" | "completed" | "cancelled".
	Status string
}

// UpdateMetadataRequest is the input to FileManagerService.UpdateMetadata -
// a single-object, synchronous operation (no bulk metadata edit in this
// MVP - FR-BULK-004 does not call for one).
type UpdateMetadataRequest struct {
	ProfileID    int64
	Bucket       string
	Key          string
	ContentType  string
	CacheControl string
	// UserMetadata holds x-amz-meta-* values, keyed WITHOUT that prefix -
	// mirrors domain.ObjectMeta.Metadata's existing convention.
	UserMetadata map[string]string
}

// CreateFolderRequest is the input to FileManagerService.CreateFolder.
type CreateFolderRequest struct {
	ProfileID int64
	Bucket    string
	// Prefix is the parent prefix the new folder is created under (may be "").
	Prefix string
	// Name is the new folder's own name - no "/" allowed (reject if present).
	Name string
}
