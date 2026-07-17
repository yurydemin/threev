package filemanager

import (
	"errors"
	"fmt"

	"github.com/aws/smithy-go"

	"threev/internal/s3client"
)

// noSuchBucketErrorCode is the AWS/S3 API error code returned when an
// operation targets a bucket that does not exist (or that the caller has no
// permission to see, which S3 also reports as NoSuchBucket rather than
// AccessDenied for ListObjectsV2 in most implementations).
const noSuchBucketErrorCode = "NoSuchBucket"

// noSuchKeyErrorCodes are the AWS/S3 API error codes returned when an
// operation targets an object key that does not exist. GetObject reports
// "NoSuchKey"; HeadObject reports "NotFound" instead, since a HEAD response
// has no body to carry a structured S3 error code, so the SDK synthesizes
// this code from the bare 404 status.
var noSuchKeyErrorCodes = map[string]bool{
	"NoSuchKey": true,
	"NotFound":  true,
}

// bucketNotEmptyErrorCode is the AWS/S3 API error code returned when
// DeleteBucket targets a bucket that still contains objects. S3 already
// refuses the delete cleanly on its own; this code is only translated into a
// friendlier message here (Блок B decision 2) rather than this package
// implementing any "empty the bucket first" convenience of its own.
const bucketNotEmptyErrorCode = "BucketNotEmpty"

// classifyOperationError turns a raw S3/network error from operation (e.g.
// "list buckets", "list objects") into a single wrapped error carrying a
// human-readable message and category, reusing s3client.ClassifyError for
// the categories it already knows about (network/auth/tls/timeout/...).
//
// It additionally special-cases the NoSuchBucket API error code: unlike
// ClassifyError - which is shared by every caller and only classifies
// operation-agnostic failure kinds - "the bucket does not exist" is
// meaningful only to operations that take a bucket name, so it is resolved
// here rather than pushed down into ClassifyError.
func classifyOperationError(operation string, err error) error {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.ErrorCode() == noSuchBucketErrorCode:
			return fmt.Errorf("%s: %s (%s): %w", operation, "Бакет не найден", "not-found", err)
		case noSuchKeyErrorCodes[apiErr.ErrorCode()]:
			return fmt.Errorf("%s: %s (%s): %w", operation, "Объект не найден", "not-found", err)
		case apiErr.ErrorCode() == bucketNotEmptyErrorCode:
			return fmt.Errorf("%s: %s (%s): %w", operation, "Бакет не пуст — сначала удалите всё его содержимое", "not-empty", err)
		}
	}

	category, message := s3client.ClassifyError(err)

	return fmt.Errorf("%s: %s (%s): %w", operation, message, category, err)
}
