package filemanager

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"threev/internal/domain"
)

// UpdateMetadata replaces req.Key's Content-Type/Cache-Control/user-metadata
// (x-amz-meta-*) via a synchronous self-copy CopyObject call with
// MetadataDirective=REPLACE (FR-BULK-004, docs/02-tech-spec.md section 4.4
// constraint 1) - unlike DeleteObjects/CopyObjects/MoveObjects, this is a
// single-object, synchronous operation: no operation id, no bulk:progress
// events (FR-BULK-004 does not call for a bulk metadata edit in this MVP,
// see domain.UpdateMetadataRequest's doc comment). It uses f.resolveClient
// (a fresh, one-off *s3.Client, the same helper ListBuckets/ListObjects/
// HeadObject/GetPresignedURL/GetTextPreview already use) rather than
// f.connMgr/s3client.WithRetry: a single synchronous call has no batch/
// worker-pool retry story to share, so the extra machinery buys nothing
// here.
func (f *FileManagerService) UpdateMetadata(req domain.UpdateMetadataRequest) error {
	client, err := f.resolveClient(req.ProfileID)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
	defer cancel()

	_, err = client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:            aws.String(req.Bucket),
		Key:               aws.String(req.Key),
		CopySource:        aws.String(copySourceFor(req.Bucket, req.Key)),
		MetadataDirective: types.MetadataDirectiveReplace,
		ContentType:       aws.String(req.ContentType),
		CacheControl:      aws.String(req.CacheControl),
		Metadata:          req.UserMetadata,
	})
	if err != nil {
		return classifyOperationError("update metadata", err)
	}

	// "create" - the same semantic type transfer.TransferService already
	// uses for "an object under this bucket/prefix changed" (see
	// bulkops.go's wailsObjectChangeEvent doc comment); a dedicated "update"
	// type is deliberately not introduced for this one extra case, keeping
	// the frontend's "object:change" handler (useTransferEvents.ts) with a
	// single, already-correct code path.
	f.emitObjectChangeEvent(req.Bucket, objectPrefixOf(req.Key), "create")

	return nil
}
