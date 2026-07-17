package filemanager

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"threev/internal/domain"
)

// CreateBucket creates a new bucket named name in the profile's S3 account.
//
// Synchronous, like CreateFolder/ListBuckets: a single CreateBucket call, no
// operation id/bulk:progress event.
//
// name is not validated here (Блок B decision 5, docs/... plan): the
// frontend performs a soft mirror of S3's DNS-compatible naming rules for
// immediate feedback, but this method is not the source of truth for that
// spec - it forwards name straight to S3 and relies on classifyOperationError
// to translate the resulting InvalidBucketName API error into a friendly
// message, rather than duplicating S3's full naming-rule spec in Go.
//
// No f.emitObjectChangeEvent call: that event type is scoped to
// object/prefix changes inside an already-selected bucket, not to changes in
// the bucket list itself. The frontend re-fetches the bucket list after a
// successful call instead (ListBuckets has no cache layer to invalidate).
//
// Guarded (Этап 4 суб-этап 4.4): resolveClient below decrypts the profile's
// credentials, requiring the current encryption key - unavailable while the
// application is locked. See domain.ErrLocked's own doc comment.
func (f *FileManagerService) CreateBucket(profileID int64, name string) error {
	encKey, ok := f.keyBox.Get()
	if !ok {
		return domain.ErrLocked
	}

	client, err := f.resolveClient(profileID, encKey)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
	defer cancel()

	_, err = client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(name)})
	if err != nil {
		return classifyOperationError("create bucket", err)
	}

	return nil
}

// DeleteBucket deletes the bucket named name from the profile's S3 account.
//
// Requires the bucket to already be empty (Блок B decision 2): this method
// deliberately does not implement any "list and delete everything first"
// convenience, since a single confirm click silently wiping an entire
// bucket's contents is disproportionately risky for a medium-priority
// feature. S3 itself already fails cleanly with BucketNotEmpty on a
// non-empty bucket; classifyOperationError translates that into a message
// pointing the user at the existing recursive folder/object deletion flow
// to empty the bucket first.
//
// Synchronous, like CreateBucket above: a single DeleteBucket call, no
// operation id/bulk:progress event, no f.emitObjectChangeEvent call (see
// CreateBucket's doc comment for why).
//
// Guarded (Этап 4 суб-этап 4.4): resolveClient below decrypts the profile's
// credentials, requiring the current encryption key - unavailable while the
// application is locked. See domain.ErrLocked's own doc comment.
func (f *FileManagerService) DeleteBucket(profileID int64, name string) error {
	encKey, ok := f.keyBox.Get()
	if !ok {
		return domain.ErrLocked
	}

	client, err := f.resolveClient(profileID, encKey)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
	defer cancel()

	_, err = client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(name)})
	if err != nil {
		return classifyOperationError("delete bucket", err)
	}

	return nil
}
