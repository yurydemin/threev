package filemanager

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"threev/internal/domain"
)

// CreateFolder creates an empty "folder placeholder" object at
// req.Prefix+req.Name+"/" (docs/02-tech-spec.md section 4.4 constraint 9) -
// the exact same zero-byte, trailing-slash key convention list.go's
// entriesFromPage/fetchAndCachePage already special-case when reading a
// listing (a "folder" some other GUI client/console created explicitly);
// CreateFolder is simply the first place this codebase creates one, rather
// than only ever recognizing one.
//
// Synchronous, like UpdateMetadata: a single PutObject call, no operation
// id/bulk:progress event.
//
// Guarded (Этап 4 суб-этап 4.4): resolveClient below decrypts the profile's
// credentials, requiring the current encryption key - unavailable while the
// application is locked. See domain.ErrLocked's own doc comment.
func (f *FileManagerService) CreateFolder(req domain.CreateFolderRequest) error {
	if req.Name == "" {
		return fmt.Errorf("create folder: name must not be empty")
	}

	if strings.Contains(req.Name, "/") {
		return fmt.Errorf("create folder: name %q must not contain %q", req.Name, "/")
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

	key := req.Prefix + req.Name + "/"

	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(req.Bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(nil),
	})
	if err != nil {
		return classifyOperationError("create folder", err)
	}

	f.emitObjectChangeEvent(req.Bucket, req.Prefix, "create")

	return nil
}
