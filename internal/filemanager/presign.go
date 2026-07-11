package filemanager

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"threev/internal/domain"
)

// minPresignExpiry and maxPresignExpiry bound the TTL of a presigned URL
// (Stage 2 plan constraint 2, widened in Stage 4 per FR-BULK-005/
// docs/02-tech-spec.md section 4.4): 1 minute is short enough to be safe for
// a short-lived preview/"copy URL" action, 7 days is a generous upper bound
// that still limits how long a leaked URL remains valid. Widening this range
// in Stage 4 did not require changing GetPresignedURL's signature, as
// designed back in Stage 2.
const (
	minPresignExpiry = time.Minute
	maxPresignExpiry = 7 * 24 * time.Hour
)

// defaultPresignExpiry is used whenever the caller passes a non-positive or
// unset expirySeconds, matching the fixed 5-minute TTL the Stage 2
// frontend uses for preview/"copy URL" (plan constraint 2).
const defaultPresignExpiry = 5 * time.Minute

// GetPresignedURL returns a temporary, self-contained URL granting GetObject
// access (SEC-003: no broader permission is requested) to bucket/key for
// the profile identified by profileID (docs/02-tech-spec.md section 9.2,
// FR-FM-007/009). expirySeconds is clamped into
// [minPresignExpiry, maxPresignExpiry]; a non-positive value falls back to
// defaultPresignExpiry instead of being clamped up to the minimum, so
// callers that don't care about TTL (e.g. Stage 2's frontend, which never
// sets this parameter explicitly) get the intended 5-minute default rather
// than the 1-minute floor.
//
// Guarded (Этап 4 суб-этап 4.4): resolveClient below decrypts the profile's
// credentials, requiring the current encryption key - unavailable while the
// application is locked. See domain.ErrLocked's own doc comment.
func (f *FileManagerService) GetPresignedURL(profileID int64, bucket, key string, expirySeconds int64) (string, error) {
	encKey, ok := f.keyBox.Get()
	if !ok {
		return "", domain.ErrLocked
	}

	client, err := f.resolveClient(profileID, encKey)
	if err != nil {
		return "", err
	}

	presignClient := s3.NewPresignClient(client)

	// Presigning is a local cryptographic operation (computing a SigV4
	// signature) and never makes a network call to S3, so
	// context.Background() is used here purely for consistency with the
	// rest of the service rather than to bound any I/O.
	result, err := presignClient.PresignGetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(clampPresignExpiry(expirySeconds)))
	if err != nil {
		return "", classifyOperationError("get presigned url", err)
	}

	return result.URL, nil
}

// clampPresignExpiry converts expirySeconds into a time.Duration bounded by
// [minPresignExpiry, maxPresignExpiry], defaulting non-positive input to
// defaultPresignExpiry.
func clampPresignExpiry(expirySeconds int64) time.Duration {
	if expirySeconds <= 0 {
		return defaultPresignExpiry
	}

	requested := time.Duration(expirySeconds) * time.Second

	if requested < minPresignExpiry {
		return minPresignExpiry
	}
	if requested > maxPresignExpiry {
		return maxPresignExpiry
	}

	return requested
}
