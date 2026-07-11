package filemanager

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"threev/internal/domain"
	"threev/internal/mimetype"
)

// HeadObject returns metadata for a single object (bucket/key) belonging to
// profileID (docs/02-tech-spec.md section 9.2), without downloading its
// body.
//
// Guarded (Этап 4 суб-этап 4.4): resolveClient below decrypts the profile's
// credentials, requiring the current encryption key - unavailable while the
// application is locked. See domain.ErrLocked's own doc comment.
func (f *FileManagerService) HeadObject(profileID int64, bucket, key string) (domain.ObjectMeta, error) {
	encKey, ok := f.keyBox.Get()
	if !ok {
		return domain.ObjectMeta{}, domain.ErrLocked
	}

	client, err := f.resolveClient(profileID, encKey)
	if err != nil {
		return domain.ObjectMeta{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
	defer cancel()

	out, err := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return domain.ObjectMeta{}, classifyOperationError("head object", err)
	}

	return headObjectOutputToMeta(key, out), nil
}

// headObjectOutputToMeta maps a HeadObjectOutput onto domain.ObjectMeta. key
// is threaded through separately because HeadObjectOutput does not echo the
// requested key back.
func headObjectOutputToMeta(key string, out *s3.HeadObjectOutput) domain.ObjectMeta {
	meta := domain.ObjectMeta{
		Key:      key,
		Size:     aws.ToInt64(out.ContentLength),
		ETag:     strings.Trim(aws.ToString(out.ETag), `"`),
		Metadata: out.Metadata,
	}

	// S3 does not always return a correct/present Content-Type for objects
	// uploaded without one explicitly set, so fall back to the same
	// extension-based table ListObjects uses (see internal/mimetype) rather
	// than surfacing an empty/generic value from the server.
	if ct := aws.ToString(out.ContentType); ct != "" {
		meta.ContentType = ct
	} else {
		meta.ContentType = mimetype.ContentTypeForKey(key)
	}

	if out.LastModified != nil {
		meta.LastModified = *out.LastModified
	}

	return meta
}
