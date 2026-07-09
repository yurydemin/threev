package filemanager

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"threev/internal/connection"
	"threev/internal/s3client"
	"threev/internal/storage"
)

// FileManagerService implements the Wails-bound API described in
// docs/02-tech-spec.md section 9.2: bucket/object listing plus (in later
// stages of this package) metadata, presigned URLs, and text preview, for a
// profile's S3-compatible storage.
//
// It deliberately does not depend on connection.ConnectionService (that
// would create a service->service dependency also needed by
// TransferService, Stage 3). Instead it takes the same
// *storage.ProfileRepository and encryption key already constructed once in
// app.go, and resolves a decrypted domain.Profile itself via
// connection.ResolveProfile - the same helper ConnectionService.GetProfile
// delegates to.
type FileManagerService struct {
	repo          *storage.ProfileRepository
	encryptionKey [32]byte
	cache         *listCache
}

// NewFileManagerService returns a FileManagerService backed by repo, using
// encryptionKey to decrypt profile credentials on demand (see
// connection.ResolveProfile). encryptionKey is expected to be derived once
// at application startup (see crypto.DeriveKey) and passed in already
// computed, mirroring NewConnectionService.
func NewFileManagerService(repo *storage.ProfileRepository, encryptionKey [32]byte) *FileManagerService {
	return &FileManagerService{
		repo:          repo,
		encryptionKey: encryptionKey,
		cache:         newListCache(),
	}
}

// resolveClient loads and decrypts the profile identified by profileID and
// builds an *s3.Client from it.
//
// Note on context: like ConnectionService, every FileManagerService method
// uses context.Background() internally (Wails v2's generated bindings do
// not expose a per-call context.Context), so resolveClient does too;
// callers are responsible for bounding any subsequent S3 call with their
// own context.WithTimeout.
//
// Note on client construction: as in connection/tester.go, a fresh
// *s3.Client is built on every call rather than pooled/reused. That is
// acceptable for this stage's volume of listing calls; a proper Connection
// Manager with pooled clients belongs to the Transfer Engine (Stage 3, see
// docs/02-tech-spec.md section 10.1).
func (f *FileManagerService) resolveClient(profileID int64) (*s3.Client, error) {
	p, err := connection.ResolveProfile(context.Background(), f.repo, f.encryptionKey, profileID)
	if err != nil {
		return nil, fmt.Errorf("resolve profile %d: %w", profileID, err)
	}

	client, err := s3client.NewS3Client(p)
	if err != nil {
		return nil, fmt.Errorf("build S3 client for profile %d: %w", profileID, err)
	}

	return client, nil
}
