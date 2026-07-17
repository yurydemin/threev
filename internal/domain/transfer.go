package domain

import "time"

// TransferTask is a single row of the "transfer_queue" table
// (docs/02-tech-spec.md section 8.2): one upload or download in flight or
// waiting to run.
type TransferTask struct {
	ID        int64
	ProfileID int64
	// Type is "upload", "download", or "download_zip" (a whole folder/
	// prefix downloaded as a single ZIP archive - see
	// transfer.TransferService.QueueDownloadPrefixZip). A "download_zip"
	// task's SourcePath/DestinationPath follow the same ORIGIN/TARGET
	// convention as a plain "download": SourcePath is bucket/prefix
	// (encodeBucketKey), DestinationPath is the local .zip file path.
	Type string
	// SourcePath is the transfer's origin: a local filesystem path for
	// uploads, or an S3 key (bucket is carried separately in
	// DestinationPath/SourcePath depending on direction - see
	// upload.go/download.go in package transfer for how the two are
	// combined) for downloads.
	SourcePath string
	// DestinationPath is the transfer's target: an S3 key for uploads, a
	// local filesystem path for downloads.
	DestinationPath string
	// Status is one of "pending", "running", "paused", "completed",
	// "failed", "cancelled" (FR-QUEUE-002).
	Status           string
	TotalBytes       int64
	TransferredBytes int64
	ErrorMessage     string
	// MultipartUploadID is the S3 UploadId, set once CreateMultipartUpload
	// succeeds for an upload task; used to resume via ListParts.
	MultipartUploadID string
	// PartsCompleted mirrors the "parts_completed" column (a JSON array of
	// completed part numbers). Retained for schema compatibility only: the
	// transfer engine treats ListParts (upload)/local file size (download)
	// as the sole source of truth for resume, not this column (documented
	// tech debt, see Этап 3 plan).
	PartsCompleted string
	// FileOffset mirrors the "file_offset" column, retained for schema
	// compatibility; not used as the resume source of truth (see
	// PartsCompleted).
	FileOffset int64
	// Priority orders pending tasks for the scheduler: lower values run
	// first (FR-QUEUE-003).
	Priority  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TransferHistoryEntry is a single row of the "transfer_history" table
// (docs/02-tech-spec.md section 8.3): a terminal record of a completed,
// failed, or cancelled transfer task (FR-QUEUE-006).
type TransferHistoryEntry struct {
	ID int64
	// QueueID is the originating transfer_queue.id, retained for
	// traceability. It is not a foreign key (the queue row is deleted as
	// part of the same move-to-history transaction) and may be 0 if the
	// history entry was not created by way of TransferQueueRepository.MoveToHistory.
	QueueID         int64
	ProfileID       int64
	Type            string
	SourcePath      string
	DestinationPath string
	TotalBytes      int64
	Status          string
	CompletedAt     time.Time
	ErrorMessage    string
}

// UploadRequest is the input to TransferService.QueueUpload
// (docs/02-tech-spec.md section 9.3): a single local file to be uploaded to
// bucket/key under the given profile.
type UploadRequest struct {
	ProfileID int64
	Bucket    string
	Key       string
	LocalPath string
	// Priority seeds the created task's Priority (FR-QUEUE-003); lower
	// values run first. Defaults to 0 (the zero value) when unset.
	Priority int
}

// DownloadRequest is the input to TransferService.QueueDownload
// (docs/02-tech-spec.md section 9.3): a single S3 object to be downloaded
// to a local path under the given profile.
type DownloadRequest struct {
	ProfileID int64
	Bucket    string
	Key       string
	LocalPath string
	// Priority seeds the created task's Priority (FR-QUEUE-003); lower
	// values run first. Defaults to 0 (the zero value) when unset.
	Priority int
}

// TransferProgressEvent is the payload of the Wails "transfer:progress"
// event (docs/02-tech-spec.md section 9.5), pushed to the frontend as a
// transfer task's progress changes.
type TransferProgressEvent struct {
	TaskID           int64
	TransferredBytes int64
	TotalBytes       int64
	SpeedBytesPerSec float64
	ETASeconds       int64
	Status           string
	Error            string
}

// ObjectChangeEvent is the payload of the Wails "object:change" event
// (docs/02-tech-spec.md section 9.5), pushed to the frontend so an open
// file manager listing can refresh when a transfer creates or removes an
// object under the bucket/prefix it is currently viewing.
type ObjectChangeEvent struct {
	Bucket string
	Prefix string
	// Type is "create" or "delete".
	Type string
}
