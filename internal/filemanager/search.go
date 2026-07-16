package filemanager

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"threev/internal/domain"
)

// maxSearchResults caps the walk performed by SearchObjects: as soon as
// this many matches have been collected, pagination stops immediately and
// the response is returned with Truncated set to true, rather than
// continuing to exhaust the rest of the bucket/prefix. This is a
// RESULT-count budget, distinct from listAllKeysTimeout's TIME budget -
// whichever of the two is hit first ends the walk the same way: whatever
// was collected so far is returned, not treated as an error.
const maxSearchResults = 500

// SearchObjects walks every object under req.Bucket/req.Prefix
// (recursively - same walk shape as ListAllKeysUnderPrefix/GetBucketSize: no
// Delimiter on the ListObjectsV2 calls, so every object nested at any depth
// is visited, bounded by the same listAllKeysTimeout budget covering the
// whole walk) and returns every one whose basename (the last non-empty
// "/"-separated segment of its key) contains req.Query as a
// case-insensitive substring.
//
// It exists to support "Искать везде" (docs backlog "Блок F — Поиск по
// всему бакету"): the existing client-side filename filter only searches
// entries already loaded into the current listing page, so a match nested
// several folders deep is invisible to it. SearchObjects performs the real,
// opt-in, server-side walk instead.
//
// An empty req.Query is treated as matching nothing: an empty, non-nil
// Entries slice is returned immediately, with no S3 calls made at all -
// walking a potentially huge bucket for a blank query would be a needless
// footgun, and the frontend never calls this with an empty query per the
// "appears from 3 characters" UI rule regardless.
//
// Like GetBucketSize, if the walk's overall listAllKeysTimeout budget is
// exhausted partway through, that is not surfaced as a hard error: the
// matches collected so far are returned with Truncated set to true. The
// same is true when maxSearchResults matches have been collected before the
// walk would otherwise have finished. Any other ListObjectsV2 error (auth,
// network, NoSuchBucket, ...) is still a real returned error via
// classifyOperationError, exactly as in every other method in this package.
//
// An empty bucket/prefix, or zero matches found within a walk that neither
// timed out nor hit maxSearchResults, is not an error either: an empty,
// non-nil Entries slice and Truncated: false are returned.
//
// Guarded (Этап 4 суб-этап 4.4): same pattern as every other
// FileManagerService method - resolveClient below decrypts the profile's
// credentials, requiring the current encryption key, unavailable while the
// application is locked. See domain.ErrLocked's own doc comment.
func (f *FileManagerService) SearchObjects(req domain.SearchObjectsRequest) (domain.SearchObjectsResponse, error) {
	key, ok := f.keyBox.Get()
	if !ok {
		return domain.SearchObjectsResponse{}, domain.ErrLocked
	}

	if req.Query == "" {
		return domain.SearchObjectsResponse{Entries: make([]domain.ObjectEntry, 0)}, nil
	}

	client, err := f.resolveClient(req.ProfileID, key)
	if err != nil {
		return domain.SearchObjectsResponse{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), listAllKeysTimeout)
	defer cancel()

	queryLower := strings.ToLower(req.Query)

	var (
		result            domain.SearchObjectsResponse
		continuationToken string
	)
	result.Entries = make([]domain.ObjectEntry, 0)

	for {
		input := &s3.ListObjectsV2Input{
			Bucket:  aws.String(req.Bucket),
			Prefix:  aws.String(req.Prefix),
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

			return domain.SearchObjectsResponse{}, classifyOperationError("search objects", err)
		}

		for _, obj := range out.Contents {
			objKey := aws.ToString(obj.Key)
			if !strings.Contains(strings.ToLower(basename(objKey)), queryLower) {
				continue
			}

			result.Entries = append(result.Entries, objectEntryFromS3Object(obj))

			if len(result.Entries) >= maxSearchResults {
				result.Truncated = true

				return result, nil
			}
		}

		if !aws.ToBool(out.IsTruncated) {
			break
		}

		continuationToken = aws.ToString(out.NextContinuationToken)
	}

	return result, nil
}

// basename returns the last non-empty "/"-separated segment of key, mirroring
// how the frontend's getEntryDisplayName already treats a key for display:
// "2024/reports/invoice.pdf" -> "invoice.pdf", and a trailing-slash
// "folder" key like "2024/reports/" -> "reports".
func basename(key string) string {
	trimmed := strings.TrimSuffix(key, "/")

	idx := strings.LastIndex(trimmed, "/")
	if idx == -1 {
		return trimmed
	}

	return trimmed[idx+1:]
}
