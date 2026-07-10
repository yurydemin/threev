// Package transfer implements the S3 upload/download engine: adaptive
// part/range sizing, multipart-upload and range-download worker pools, ETag
// verification, and the transfer queue (docs/02-tech-spec.md sections 9.3,
// 10.1-10.6). This file is the first piece: adaptive part sizing, which
// later steps (multipart_upload.go, range_download.go) use to decide how to
// slice a file for parallel transfer.
package transfer

// Part-size table constants (docs/02-tech-spec.md section 10.2, Stage 3
// plan). The table buckets a file by its total size and answers with a
// fixed part size for that bucket; see PartSize for the exact boundary
// semantics.
const (
	// minPartSize is both the S3 protocol floor for any part except the
	// last one in a multipart upload ("не менее 5 МБ") and PartSize's
	// answer for the smallest bucket (< smallFileThreshold).
	minPartSize = 5 * 1024 * 1024 // 5 MB

	// defaultPartSize is the Stage 3 plan's default part size, used for
	// files in [smallFileThreshold, mediumFileThreshold).
	defaultPartSize = 16 * 1024 * 1024 // 16 MB

	// largePartSize is used for files in [mediumFileThreshold,
	// largeFileThreshold).
	largePartSize = 64 * 1024 * 1024 // 64 MB

	// maxPartSize is the Stage 3 plan's table ceiling, used for files >=
	// largeFileThreshold. The 10000-part clamp in PartSize can still push
	// the returned part size above this for exceptionally large files
	// (see PartSize).
	maxPartSize = 128 * 1024 * 1024 // 128 MB

	// smallFileThreshold is the boundary below which minPartSize applies.
	smallFileThreshold = 100 * 1024 * 1024 // 100 MB

	// mediumFileThreshold is the boundary below which defaultPartSize
	// applies (and at/above which largePartSize starts).
	mediumFileThreshold = 1024 * 1024 * 1024 // 1 GB

	// largeFileThreshold is the boundary at/above which maxPartSize
	// applies.
	largeFileThreshold = 10 * 1024 * 1024 * 1024 // 10 GB

	// maxPartsPerUpload is S3's hard protocol limit on the number of
	// parts in a single multipart upload (docs/02-tech-spec.md section
	// 10.2: "лимит S3 в 10000 частей"). PartSize clamps its table answer
	// upward whenever using it would exceed this.
	maxPartsPerUpload = 10000

	// clampRoundingUnit is the granularity the 10000-part clamp in
	// PartSize rounds its computed part size up to, so the clamp never
	// returns an oddly-precise byte count.
	clampRoundingUnit = 1024 * 1024 // 1 MB

	// partSizeOverrideMinMB/partSizeOverrideMaxMB are the bounds
	// TransferService.SetPartSizeOverrideMB clamps a user-configured fixed
	// part size to (Этап 4 суб-этап 4.3, UX-спека 5.7): the same [5,128] MB
	// range PartSize's own adaptive table ever produces (minPartSize's 5MB
	// floor through maxPartSize's 128MB ceiling), so an override can never
	// pick a part size the adaptive table itself would never have chosen.
	partSizeOverrideMinMB = 5
	partSizeOverrideMaxMB = 128
)

// PartSize returns the adaptive part size, in bytes, to use for a
// multipart upload (or range-download segmentation) of a file totalBytes
// long, per the heuristic table in the Stage 3 plan
// (docs/02-tech-spec.md section 10.2):
//
//	totalBytes < 100MB           -> 5MB   (minPartSize)
//	100MB <= totalBytes < 1GB    -> 16MB  (defaultPartSize)
//	1GB   <= totalBytes < 10GB   -> 64MB  (largePartSize)
//	10GB  <= totalBytes          -> 128MB (maxPartSize)
//
// Each boundary belongs to the bucket above it (the comparisons above are
// all "<", never "<="), so a file of exactly 100MB, 1GB, or 10GB gets the
// larger of its two neighboring part sizes.
//
// After picking a part size from the table, PartSize applies a protective
// clamp: if PartCount(totalBytes, partSize) would exceed
// maxPartsPerUpload (S3's hard 10000-parts-per-upload protocol limit),
// the part size is recomputed as ceil(totalBytes / maxPartsPerUpload),
// rounded up to the nearest clampRoundingUnit (1 MB) - even if that
// pushes the result above the table's nominal 128MB ceiling. This only
// happens for files well beyond the table's own range (roughly >1.28TB
// at the 128MB/10000-part boundary), which is an intentional rare-case
// safety valve, not a normal code path.
//
// PartSize is not expected to be called for files under 5MB (per
// FR-TR-001, those are dispatched as plain PutObject uploads by
// upload.go, not multipart), but it still returns a sane, positive value
// (minPartSize) for that case rather than 0 or a negative number.
func PartSize(totalBytes int64) int64 {
	var partSize int64

	switch {
	case totalBytes < smallFileThreshold:
		partSize = minPartSize
	case totalBytes < mediumFileThreshold:
		partSize = defaultPartSize
	case totalBytes < largeFileThreshold:
		partSize = largePartSize
	default:
		partSize = maxPartSize
	}

	if PartCount(totalBytes, partSize) > maxPartsPerUpload {
		raw := ceilDiv(totalBytes, maxPartsPerUpload)
		partSize = ceilDiv(raw, clampRoundingUnit) * clampRoundingUnit
	}

	return partSize
}

// PartCount returns the number of parts a file totalBytes long splits
// into at partSize bytes per part (ceil division: a trailing partial part
// still counts as one whole part). It is used both by PartSize's own
// 10000-part clamp and, later, by multipart_upload.go to size its worker
// pool.
//
// If partSize <= 0, PartCount returns 0 rather than dividing by zero or
// returning a negative/nonsensical count; this is not expected to happen
// in normal operation (PartSize never returns a non-positive value), but
// PartCount is defensive against it since it is also called directly by
// other Stage 3 code. Likewise, totalBytes <= 0 (an empty or
// not-yet-known-size file) returns 0 parts.
func PartCount(totalBytes, partSize int64) int64 {
	if partSize <= 0 || totalBytes <= 0 {
		return 0
	}

	return ceilDiv(totalBytes, partSize)
}

// ceilDiv returns ceil(a / b) for positive a and b.
func ceilDiv(a, b int64) int64 {
	return (a + b - 1) / b
}
