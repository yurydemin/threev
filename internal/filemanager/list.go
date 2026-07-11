package filemanager

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"threev/internal/domain"
	"threev/internal/mimetype"
)

// listTimeout bounds a single ListBuckets/ListObjectsV2 call. Mirrors
// connection.testTimeout: a generous, fixed timeout for an interactive,
// user-initiated navigation action (PERF-003 targets 3s for a full 1000-key
// page at <100ms latency, so 10s leaves ample headroom without letting a
// stalled request hang the UI indefinitely).
const listTimeout = 10 * time.Second

// maxKeysPerPage is the page size passed to ListObjectsV2, matching
// FR-FM-005's documented limit.
const maxKeysPerPage = 1000

// delimiter collapses keys sharing a common prefix up to the next "/" into a
// single CommonPrefix ("folder"), giving FR-FM-002's folder-style
// navigation instead of a flat listing of every key in the bucket.
const delimiter = "/"

// ListBuckets returns every bucket visible to the profile identified by
// profileID (FR-FM-001).
//
// Guarded (Этап 4 суб-этап 4.4): resolveClient below decrypts the profile's
// credentials, requiring the current encryption key - unavailable while the
// application is locked. See domain.ErrLocked's own doc comment.
func (f *FileManagerService) ListBuckets(profileID int64) ([]domain.Bucket, error) {
	key, ok := f.keyBox.Get()
	if !ok {
		return nil, domain.ErrLocked
	}

	client, err := f.resolveClient(profileID, key)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
	defer cancel()

	out, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, classifyOperationError("list buckets", err)
	}

	buckets := make([]domain.Bucket, 0, len(out.Buckets))
	for _, b := range out.Buckets {
		bucket := domain.Bucket{Name: aws.ToString(b.Name)}
		if b.CreationDate != nil {
			bucket.CreationDate = *b.CreationDate
		}

		buckets = append(buckets, bucket)
	}

	return buckets, nil
}

// ListObjects returns a sorted page of entries for req.Bucket/req.Prefix
// (FR-FM-002/004/005), serving from the session cache (listCache) whenever
// possible instead of round-tripping to S3:
//
//   - req.Refresh discards any cached listing for this profile+bucket+prefix
//     before doing anything else, forcing a fresh fetch below.
//   - A request for the first page (req.ContinuationToken == "") of a
//     listing already fully or partially cached is served entirely from the
//     cache: no S3 call is made, only a re-sort (e.g. the user only changed
//     SortBy/SortOrder).
//   - Anything else - no cache entry yet, or a specific next page requested
//     via req.ContinuationToken - performs a real ListObjectsV2 call and
//     merges the result into the cache.
//
// In every case the returned Entries are freshly sorted per
// req.SortBy/req.SortOrder without mutating the cached order.
//
// Guarded (Этап 4 суб-этап 4.4): the guard runs unconditionally at the top,
// even though a fully cache-served request (see above) does not itself
// need the encryption key - this is deliberate, not overly conservative:
// with no Lock()/auto-relock in this application (see crypto.KeyBox's own
// doc comment), the cache can only ever hold entries fetched while
// unlocked, so a locked application can never actually have anything to
// serve from cache anyway. Guarding once, up front, is simpler than
// threading the guard down into fetchAndCachePage for a case that cannot
// occur in practice.
func (f *FileManagerService) ListObjects(req domain.ListObjectsRequest) (domain.ListObjectsResponse, error) {
	encKey, ok := f.keyBox.Get()
	if !ok {
		return domain.ListObjectsResponse{}, domain.ErrLocked
	}

	key := cacheKey{ProfileID: req.ProfileID, Bucket: req.Bucket, Prefix: req.Prefix}

	if req.Refresh {
		f.cache.invalidate(key)
	}

	if req.ContinuationToken == "" && !req.Refresh {
		if cached, ok := f.cache.get(key); ok {
			return domain.ListObjectsResponse{
				Entries:               sortEntries(cached.Entries, req.SortBy, req.SortOrder),
				NextContinuationToken: cached.NextToken,
				IsTruncated:           cached.IsTruncated,
			}, nil
		}
	}

	return f.fetchAndCachePage(req, key, encKey)
}

// fetchAndCachePage performs the actual ListObjectsV2 call for req (using
// encKey, already guarded by ListObjects), merges the resulting page into
// the cache under key (appending if req is a follow-up page, replacing if
// it is a first page), and returns the sorted, accumulated result.
func (f *FileManagerService) fetchAndCachePage(req domain.ListObjectsRequest, key cacheKey, encKey [32]byte) (domain.ListObjectsResponse, error) {
	client, err := f.resolveClient(req.ProfileID, encKey)
	if err != nil {
		return domain.ListObjectsResponse{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
	defer cancel()

	input := &s3.ListObjectsV2Input{
		Bucket:    aws.String(req.Bucket),
		Prefix:    aws.String(req.Prefix),
		Delimiter: aws.String(delimiter),
		MaxKeys:   aws.Int32(maxKeysPerPage),
	}
	if req.ContinuationToken != "" {
		input.ContinuationToken = aws.String(req.ContinuationToken)
	}

	out, err := client.ListObjectsV2(ctx, input)
	if err != nil {
		return domain.ListObjectsResponse{}, classifyOperationError("list objects", err)
	}

	page := entriesFromPage(out, req.Prefix)

	nextToken := aws.ToString(out.NextContinuationToken)
	isTruncated := aws.ToBool(out.IsTruncated)

	if req.ContinuationToken != "" {
		f.cache.appendPage(key, page, nextToken, isTruncated)
	} else {
		f.cache.set(key, cacheEntry{Entries: page, NextToken: nextToken, IsTruncated: isTruncated})
	}

	cached, _ := f.cache.get(key)

	return domain.ListObjectsResponse{
		Entries:               sortEntries(cached.Entries, req.SortBy, req.SortOrder),
		NextContinuationToken: nextToken,
		IsTruncated:           isTruncated,
	}, nil
}

// entriesFromPage maps one ListObjectsV2 response into domain.ObjectEntry
// values: CommonPrefixes become folders, Contents become files.
//
// One edge case is filtered out: some S3-compatible servers include, in
// Contents, an object whose key is exactly equal to prefix itself (a
// zero-byte placeholder object created when a "folder" is made explicitly,
// e.g. by many GUI clients/consoles). That key is not a distinct child of
// prefix - it is the "folder" being listed - so surfacing it as a row would
// duplicate the CommonPrefix entry navigating into the same location and is
// dropped here.
func entriesFromPage(out *s3.ListObjectsV2Output, prefix string) []domain.ObjectEntry {
	entries := make([]domain.ObjectEntry, 0, len(out.CommonPrefixes)+len(out.Contents))

	for _, cp := range out.CommonPrefixes {
		entries = append(entries, domain.ObjectEntry{
			Key:      aws.ToString(cp.Prefix),
			IsFolder: true,
		})
	}

	for _, obj := range out.Contents {
		objKey := aws.ToString(obj.Key)
		if prefix != "" && objKey == prefix {
			continue
		}

		entry := domain.ObjectEntry{
			Key:          objKey,
			IsFolder:     false,
			Size:         aws.ToInt64(obj.Size),
			ContentType:  mimetype.ContentTypeForKey(objKey),
			StorageClass: string(obj.StorageClass),
		}
		if obj.LastModified != nil {
			entry.LastModified = *obj.LastModified
		}

		entries = append(entries, entry)
	}

	return entries
}
