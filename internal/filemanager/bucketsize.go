package filemanager

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"threev/internal/domain"
)

// GetBucketSize walks every object under bucket/prefix (recursively - same
// walk shape as ListAllKeysUnderPrefix: no Delimiter on the ListObjectsV2
// calls, so every object nested at any depth under prefix is visited, and
// the same listAllKeysTimeout budget bounds the whole walk rather than being
// reset per page) and returns aggregate totals instead of the flat key
// list: TotalBytes sums aws.ToInt64(obj.Size) over every Contents entry
// seen, ObjectCount counts every one of those entries.
//
// Like ListAllKeysUnderPrefix, a Contents entry whose key is exactly equal
// to prefix (the zero-byte placeholder object some clients create when a
// "folder" is made explicitly) is included in the totals here too: it is a
// real object under prefix, and its bytes/count belong in the aggregate
// just as much as any other object's, even though entriesFromPage drops it
// from browsable listings to avoid duplicating the CommonPrefix row.
//
// It exists to support an on-demand "bucket properties" panel (Блок D,
// "Дашборд размера бакета"): unlike ListAllKeysUnderPrefix, this is not a
// bulk-delete-prep operation, so individual keys are never collected -
// only the running totals are kept as each page is summed.
//
// If the walk's overall listAllKeysTimeout budget is exhausted partway
// through (a very large bucket/prefix taking longer than 60s to fully
// enumerate), that is deliberately not surfaced as a hard error: the
// partial totals accumulated so far are returned with Truncated set to
// true, so the caller can present "~N objects, X GB so far (incomplete)"
// rather than losing the whole computation to a bare failure. Any other
// ListObjectsV2 error (auth, network, NoSuchBucket, ...) is still a real
// returned error via classifyOperationError, exactly as in
// ListAllKeysUnderPrefix.
//
// An empty bucket/prefix is not an error either: a zero-value
// domain.BucketSizeResult (TotalBytes 0, ObjectCount 0, Truncated false)
// and a nil error are returned, leaving it to the caller to present "0
// objects" rather than this method treating it as a failure.
//
// Guarded (Этап 4 суб-этап 4.4): same pattern as every other
// FileManagerService method - resolveClient below decrypts the profile's
// credentials, requiring the current encryption key, unavailable while the
// application is locked. See domain.ErrLocked's own doc comment.
func (f *FileManagerService) GetBucketSize(profileID int64, bucket, prefix string) (domain.BucketSizeResult, error) {
	key, ok := f.keyBox.Get()
	if !ok {
		return domain.BucketSizeResult{}, domain.ErrLocked
	}

	client, err := f.resolveClient(profileID, key)
	if err != nil {
		return domain.BucketSizeResult{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), listAllKeysTimeout)
	defer cancel()

	var (
		result            domain.BucketSizeResult
		continuationToken string
	)

	for {
		input := &s3.ListObjectsV2Input{
			Bucket:  aws.String(bucket),
			Prefix:  aws.String(prefix),
			MaxKeys: aws.Int32(maxKeysPerPage),
		}
		if continuationToken != "" {
			input.ContinuationToken = aws.String(continuationToken)
		}

		out, err := client.ListObjectsV2(ctx, input)
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				result.Truncated = true

				return result, nil
			}

			return domain.BucketSizeResult{}, classifyOperationError("get bucket size", err)
		}

		for _, obj := range out.Contents {
			result.TotalBytes += aws.ToInt64(obj.Size)
			result.ObjectCount++
		}

		if !aws.ToBool(out.IsTruncated) {
			break
		}

		continuationToken = aws.ToString(out.NextContinuationToken)
	}

	return result, nil
}
