package filemanager

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"threev/internal/domain"
)

// RenameObject renames req.OldKey to req.NewKey within req.Bucket
// (FR-BULK-003's "Rename (F2)" case - see domain.RenameObjectRequest's doc
// comment for why this cannot simply reuse CopyObjects/MoveObjects).
// Synchronous, like UpdateMetadata/CreateFolder: renaming a single object is
// two quick calls (a self-bucket CopyObject to the new key, then a
// DeleteObject of the old key), not a batch worthy of an operation id/
// bulk:progress events.
//
// Ordering: exactly copyOneObject's copy-then-delete-that-same-key rationale
// (see copymove.go), applied to one object instead of a worker pool - if
// DeleteObject fails after a successful CopyObject, RenameObject returns the
// DeleteObject error and, deliberately, does NOT attempt to undo the copy:
// req.OldKey and req.NewKey both exist (a safe, recoverable duplicate) rather
// than risking deleting req.OldKey before a copy at req.NewKey was ever
// confirmed.
//
// Guarded (Этап 4 суб-этап 4.4): resolveClient below decrypts the profile's
// credentials, requiring the current encryption key - unavailable while the
// application is locked. See domain.ErrLocked's own doc comment.
func (f *FileManagerService) RenameObject(req domain.RenameObjectRequest) error {
	if req.NewKey == "" {
		return fmt.Errorf("rename object: new key must not be empty")
	}

	if req.NewKey == req.OldKey {
		return fmt.Errorf("rename object: new key %q is the same as the current key", req.NewKey)
	}

	encKey, ok := f.keyBox.Get()
	if !ok {
		return domain.ErrLocked
	}

	client, err := f.resolveClient(req.ProfileID, encKey)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
	defer cancel()

	_, err = client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(req.Bucket),
		Key:        aws.String(req.NewKey),
		CopySource: aws.String(copySourceFor(req.Bucket, req.OldKey)),
	})
	if err != nil {
		return classifyOperationError("rename object", err)
	}

	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(req.Bucket),
		Key:    aws.String(req.OldKey),
	})
	if err != nil {
		return classifyOperationError("rename object", err)
	}

	// Two separate events (never a single one covering both prefixes) since
	// OldKey and NewKey can, in the general case, resolve to different
	// "folder" prefixes even though Stage 4 Block D's UI only ever renames
	// within the same prefix - see this file's own doc comment.
	f.emitObjectChangeEvent(req.Bucket, objectPrefixOf(req.OldKey), "delete")
	f.emitObjectChangeEvent(req.Bucket, objectPrefixOf(req.NewKey), "create")

	return nil
}
