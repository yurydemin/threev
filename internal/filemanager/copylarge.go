package filemanager

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"golang.org/x/sync/errgroup"

	"threev/internal/s3client"
	"threev/internal/transfer"
)

// maxSingleCopySize is the largest source-object size copyOneObject will
// hand to a single-shot CopyObject call before falling back to
// copyLargeObject's server-side multipart-copy path (UploadPartCopy per
// byte range). S3's actual CopyObject limit is documented as ~5GiB
// (5,368,709,120 bytes) - this constant is deliberately set well below
// that real boundary (roughly 868MB of margin), NOT an attempt to hug the
// exact 5GiB ceiling: the goal is to comfortably avoid ever handing S3 a
// single-shot CopyObject call for an object merely close to (but
// technically still under) its real limit - accounting for size reporting
// differences across S3-compatible providers - not to squeeze every last
// byte out of the single-shot path.
const maxSingleCopySize = 4_500_000_000

// copyLargeObjectConcurrency clamps the multipart-copy worker pool size
// the same way transfer.effectiveConcurrency clamps uploadMultipart's own
// (that helper is unexported, so not directly reusable here):
// transfer.DefaultPartConcurrency workers by default, never more than
// transfer.MaxPartConcurrency, and never more than partCount itself (no
// benefit running idle workers for a small part count). Always returns a
// value >= 1 - see effectiveConcurrency's own doc comment for why that
// floor matters (errgroup.Group.SetLimit(0) would block forever).
func copyLargeObjectConcurrency(partCount int64) int {
	concurrency := transfer.DefaultPartConcurrency

	if concurrency > transfer.MaxPartConcurrency {
		concurrency = transfer.MaxPartConcurrency
	}

	if partCount > 0 && int64(concurrency) > partCount {
		concurrency = int(partCount)
	}

	if concurrency < 1 {
		concurrency = 1
	}

	return concurrency
}

// copyLargeObject copies sourceBucket/sourceKey (size bytes long) to
// destBucket/destKey via a server-side multipart copy
// (CreateMultipartUpload + one UploadPartCopy per byte range +
// CompleteMultipartUpload), used by copyOneObject whenever size exceeds
// maxSingleCopySize - S3's plain CopyObject cannot copy an object anywhere
// near that large in a single call.
//
// host/pooled/fresh follow the exact same pooled-on-first-attempt,
// fresh-on-retry pattern copyOneObject and transfer.uploadMultipart both
// already use.
//
// Unlike transfer.uploadMultipart, this never supports resume: a bulk
// copy/move operation has no persisted queue entry (no transfer_queue row)
// for a later call to rediscover an in-progress UploadId from - see
// runCopyOrMove's own doc comment on cancellation semantics (a canceled
// bulk operation simply stops its worker pool, nothing more). Every call
// to copyLargeObject therefore always starts a brand-new multipart upload,
// and if any part ultimately fails (after s3client.PartRetryPolicy's own
// retries are exhausted), the whole multipart upload is aborted
// best-effort rather than left behind for a resume attempt that could
// never happen.
func (f *FileManagerService) copyLargeObject(ctx context.Context, pooled, fresh *s3.Client, host, sourceBucket, sourceKey, destBucket, destKey string, size int64) error {
	uploadID, err := createLargeObjectCopyUpload(ctx, f.breaker, f.retryPolicies, host, pooled, fresh, destBucket, destKey)
	if err != nil {
		return fmt.Errorf("create multipart upload for %s/%s: %w", destBucket, destKey, err)
	}

	partSize := transfer.PartSize(size)
	partCount := transfer.PartCount(size, partSize)

	partETags, copyErr := f.copyLargeObjectParts(ctx, pooled, fresh, host, sourceBucket, sourceKey, destBucket, destKey, uploadID, size, partSize, partCount)
	if copyErr != nil {
		// Best-effort cleanup only (mirroring completeMultipartUpload's own
		// "best-effort" style elsewhere in this codebase): an abort failure
		// here is logged, never returned - the original copyErr below is
		// what's reported to the caller either way, and this package has no
		// persisted queue entry a later Resume could use to find this
		// UploadId again (see this function's own doc comment), so a failed
		// abort simply leaves an orphaned server-side MPU rather than
		// something recoverable.
		//
		// context.Background(), deliberately NOT ctx: the most common way
		// copyErr is non-nil is the user cancelling the bulk operation,
		// which cancels this exact ctx (see runCopyOrMove/registerBulkOp) -
		// issuing the cleanup abort with an already-canceled context would
		// make it fail immediately every time, silently orphaning the
		// server-side MPU on every cancel. transfer/task.go's own abort
		// call on cancel uses the identical context.Background() pattern
		// for the same reason.
		if abortErr := transfer.AbortMultipartUpload(context.Background(), pooled, destBucket, destKey, uploadID); abortErr != nil {
			log.Printf("filemanager: multipart copy %s/%s -> %s/%s: abort multipart upload %s: %v", sourceBucket, sourceKey, destBucket, destKey, uploadID, abortErr)
		}

		return fmt.Errorf("multipart copy %s/%s -> %s/%s: %w", sourceBucket, sourceKey, destBucket, destKey, copyErr)
	}

	if err := f.completeLargeObjectCopy(ctx, pooled, fresh, host, destBucket, destKey, uploadID, partETags); err != nil {
		return err
	}

	return nil
}

// createLargeObjectCopyUpload issues CreateMultipartUpload for
// destBucket/destKey and returns the assigned UploadId, under
// retryPolicies.Metadata() - the same policy/pooled-fresh pattern
// transfer.ensureMultipartUploadID uses for an ordinary upload's own
// CreateMultipartUpload call.
func createLargeObjectCopyUpload(ctx context.Context, breaker *s3client.CircuitBreaker, retryPolicies *s3client.RetryPolicyStore, host string, pooled, fresh *s3.Client, destBucket, destKey string) (string, error) {
	var uploadID string

	err := s3client.WithRetry(ctx, breaker, retryPolicies.Metadata(), host, func(attemptCtx context.Context, isRetry bool) error {
		client := pooled
		if isRetry {
			client = fresh
		}

		out, createErr := client.CreateMultipartUpload(attemptCtx, &s3.CreateMultipartUploadInput{
			Bucket: aws.String(destBucket),
			Key:    aws.String(destKey),
		})
		if createErr != nil {
			return createErr
		}

		if out.UploadId == nil {
			return fmt.Errorf("S3 did not return an UploadId from CreateMultipartUpload")
		}

		uploadID = *out.UploadId

		return nil
	})

	return uploadID, err
}

// copyLargeObjectParts runs the errgroup-bounded UploadPartCopy worker pool
// for uploadID's partCount parts (each partSize bytes, the last one getting
// whatever remainder is left), mirroring transfer.uploadMultipart's own
// part loop: offsets are computed the same way, and every part's resulting
// ETag (quotes stripped) is collected into a map[int32]string guarded by a
// mutex, since a group.Go closure can run concurrently with the loop
// scheduling its next iteration - see uploadMultipart's identical
// partETags/mu doc comment for exactly why the mutex is required even
// though nothing else writes into the map concurrently with this loop.
func (f *FileManagerService) copyLargeObjectParts(ctx context.Context, pooled, fresh *s3.Client, host, sourceBucket, sourceKey, destBucket, destKey, uploadID string, size, partSize, partCount int64) (map[int32]string, error) {
	partETags := make(map[int32]string, partCount)

	var mu sync.Mutex

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(copyLargeObjectConcurrency(partCount))

	for n := int64(1); n <= partCount; n++ {
		// partCount is bounded by transfer.PartSize's 10000-part clamp
		// (maxPartsPerUpload), so n always fits comfortably in an int32 -
		// S3's PartNumber field type, and what UploadPartCopyInput.
		// PartNumber requires.
		partNumber := int32(n) //nolint:gosec // see comment above

		offset := (n - 1) * partSize
		partLen := partSize
		if remaining := size - offset; remaining < partLen {
			partLen = remaining
		}

		group.Go(func() error {
			partETag, partErr := f.uploadPartCopy(groupCtx, pooled, fresh, host, sourceBucket, sourceKey, destBucket, destKey, uploadID, partNumber, offset, partLen)
			if partErr != nil {
				return fmt.Errorf("part %d: %w", partNumber, partErr)
			}

			mu.Lock()
			partETags[partNumber] = partETag
			mu.Unlock()

			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return nil, err
	}

	return partETags, nil
}

// uploadPartCopy issues a single UploadPartCopy call for byte range
// [offset, offset+partLen) of sourceBucket/sourceKey as PartNumber
// partNumber of uploadID, under s3client.PartRetryPolicy - the same policy
// transfer.uploadPart uses for an ordinary upload's own per-part calls,
// since a part is likewise the unit of work most exposed to a flaky link
// here. Unlike uploadPart there is no local file/reader to reopen fresh on
// each retry attempt - CopySourceRange is a stateless header value, so a
// retried attempt simply resends the identical range with no risk of the
// "partially-read reader" bug uploadPart's own doc comment describes.
//
// Returns the part's ETag with surrounding quotes stripped, matching every
// other ETag this codebase collects (transfer.uploadPart,
// transfer.listCompletedParts).
func (f *FileManagerService) uploadPartCopy(ctx context.Context, pooled, fresh *s3.Client, host, sourceBucket, sourceKey, destBucket, destKey, uploadID string, partNumber int32, offset, partLen int64) (string, error) {
	rangeHeader := fmt.Sprintf("bytes=%d-%d", offset, offset+partLen-1)

	var partETag string

	err := s3client.WithRetry(ctx, f.breaker, f.retryPolicies.Part(), host, func(attemptCtx context.Context, isRetry bool) error {
		client := pooled
		if isRetry {
			client = fresh
		}

		out, copyErr := client.UploadPartCopy(attemptCtx, &s3.UploadPartCopyInput{
			Bucket:          aws.String(destBucket),
			Key:             aws.String(destKey),
			UploadId:        aws.String(uploadID),
			PartNumber:      aws.Int32(partNumber),
			CopySource:      aws.String(copySourceFor(sourceBucket, sourceKey)),
			CopySourceRange: aws.String(rangeHeader),
		})
		if copyErr != nil {
			return copyErr
		}

		if out.CopyPartResult == nil || out.CopyPartResult.ETag == nil {
			return fmt.Errorf("S3 did not return an ETag for part %d", partNumber)
		}

		partETag = strings.Trim(*out.CopyPartResult.ETag, `"`)

		return nil
	})

	return partETag, err
}

// completeLargeObjectCopy sorts partETags into ascending PartNumber order
// and calls CompleteMultipartUpload with the full part list, mirroring
// transfer.completeMultipartUpload's own sort-then-complete step (minus
// its composite-ETag verification, which is upload-specific FR-TR-004
// machinery with no equivalent need here - a bucket-to-bucket copy has no
// local file to verify a computed checksum against).
func (f *FileManagerService) completeLargeObjectCopy(ctx context.Context, pooled, fresh *s3.Client, host, destBucket, destKey, uploadID string, partETags map[int32]string) error {
	partNumbers := make([]int32, 0, len(partETags))
	for n := range partETags {
		partNumbers = append(partNumbers, n)
	}

	sort.Slice(partNumbers, func(i, j int) bool { return partNumbers[i] < partNumbers[j] })

	parts := make([]types.CompletedPart, 0, len(partNumbers))
	for _, n := range partNumbers {
		parts = append(parts, types.CompletedPart{
			ETag:       aws.String(partETags[n]),
			PartNumber: aws.Int32(n),
		})
	}

	err := s3client.WithRetry(ctx, f.breaker, f.retryPolicies.Metadata(), host, func(attemptCtx context.Context, isRetry bool) error {
		client := pooled
		if isRetry {
			client = fresh
		}

		_, completeErr := client.CompleteMultipartUpload(attemptCtx, &s3.CompleteMultipartUploadInput{
			Bucket:          aws.String(destBucket),
			Key:             aws.String(destKey),
			UploadId:        aws.String(uploadID),
			MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
		})

		return completeErr
	})
	if err != nil {
		return fmt.Errorf("complete multipart upload %s for %s/%s: %w", uploadID, destBucket, destKey, err)
	}

	return nil
}
