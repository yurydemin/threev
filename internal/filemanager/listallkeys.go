package filemanager

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"threev/internal/domain"
)

// listAllKeysTimeout bounds the ENTIRE walk performed by
// ListAllKeysUnderPrefix - every page it takes to exhaust a prefix, not just
// one. Unlike listTimeout (10s, sized for a single interactive
// ListBuckets/ListObjectsV2 call), a "folder" being prepared for deletion
// may nest thousands of objects across many maxKeysPerPage-sized pages, so a
// single, longer deadline covers the whole recursive walk instead of being
// reset per page (which would let a pathologically large folder run
// unbounded).
const listAllKeysTimeout = 60 * time.Second

// ListAllKeysUnderPrefix walks every object under bucket/prefix
// (recursively) and returns the full, flat list of real object keys found
// there. It exists to support "delete folder": prefixes like
// "photos/vacation/" are not real containers in S3, only a naming
// convention, so deleting one requires first discovering every actual
// object key nested under it, then handing that full list, unchanged, to
// the existing DeleteObjects/runDeleteObjects (delete.go) - this method
// performs only the discovery half of that operation.
//
// Unlike ListObjects/fetchAndCachePage, no Delimiter is set on the
// ListObjectsV2 calls made here: fetchAndCachePage deliberately sets
// Delimiter so a single page groups results one folder-level at a time for
// FR-FM-002's browsing UI, but ListAllKeysUnderPrefix wants the opposite -
// every object nested at any depth under prefix, flattened into one list,
// since every one of those keys must be deleted regardless of how deeply it
// is nested.
//
// This is a one-shot bulk-delete-prep operation, not a browsable listing:
// unlike fetchAndCachePage, nothing here touches f.cache (no f.cache.set,
// f.cache.appendPage, or f.cache.get) - the result is handed straight back
// to the caller and never needs to be paged through again.
//
// Also unlike entriesFromPage (which drops a Contents entry whose key is
// exactly equal to prefix - a zero-byte placeholder object some clients
// create when a "folder" is made explicitly - because surfacing it as a row
// would duplicate the CommonPrefix entry navigating into the same
// location), that placeholder key IS included here if S3 returns one: the
// whole point of this method is to find everything that must be deleted,
// and that placeholder object is a real object under prefix that DeleteObjects
// needs to know about just as much as any other.
//
// If prefix genuinely has nothing under it (e.g. it was already emptied by
// something else between the user opening the context menu and this call
// running), that is not an error: an empty, non-nil []string and a nil
// error are returned, leaving it to the caller to decide how to present "0
// objects found" rather than this method treating it as a failure.
//
// Guarded (Этап 4 суб-этап 4.4): same pattern as every other
// FileManagerService method - resolveClient below decrypts the profile's
// credentials, requiring the current encryption key, unavailable while the
// application is locked. See domain.ErrLocked's own doc comment.
func (f *FileManagerService) ListAllKeysUnderPrefix(profileID int64, bucket, prefix string) ([]string, error) {
	key, ok := f.keyBox.Get()
	if !ok {
		return nil, domain.ErrLocked
	}

	client, err := f.resolveClient(profileID, key)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), listAllKeysTimeout)
	defer cancel()

	keys := make([]string, 0)

	var continuationToken string

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
			return nil, classifyOperationError("list all keys", err)
		}

		for _, obj := range out.Contents {
			keys = append(keys, aws.ToString(obj.Key))
		}

		if !aws.ToBool(out.IsTruncated) {
			break
		}

		continuationToken = aws.ToString(out.NextContinuationToken)
	}

	return keys, nil
}
