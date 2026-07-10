package filemanager

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"threev/internal/connection"
	"threev/internal/s3client"
	"threev/internal/storage"
)

// FileManagerService implements the Wails-bound API described in
// docs/02-tech-spec.md section 9.2: bucket/object listing, metadata,
// presigned URLs, text preview, and (Этап 4, суб-этап 4.1) bulk
// delete/copy/move/create-folder operations for a profile's S3-compatible
// storage.
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

	// connMgr/breaker back the bulk delete/copy/move operations (bulkops.go,
	// delete.go, copymove.go): unlike resolveClient (used by the pre-Stage-4
	// listing/metadata/preview methods above, which build a fresh, one-off
	// *s3.Client per call), a bulk operation over potentially hundreds or
	// thousands of keys reuses the same pooled/fresh clients and per-host
	// circuit breaker the Transfer Engine (Stage 3) already relies on -
	// breaker is, deliberately, the exact same *s3client.CircuitBreaker
	// instance app.go's newApp() constructs for TransferService (not a new
	// one), so retry/failure bookkeeping for a given host is shared across
	// both transfers and bulk operations rather than tracked twice.
	connMgr *s3client.ConnectionManager
	breaker *s3client.CircuitBreaker

	// wailsCtx holds ctxHolder (see its own doc comment in bulkops.go),
	// installed once via SetContext from App.startup, enabling
	// emitBulkProgressEvent/emitObjectChangeEvent to actually publish Wails
	// events. Until then (including every test in this package, which never
	// calls SetContext), both are no-ops - the exact same contract
	// transfer.TransferService.wailsCtx documents.
	wailsCtx atomic.Value

	// mu guards running (the set of in-flight bulk operations) and is never
	// held for the duration of a bulk operation's own network I/O - only
	// while running is read/written (see bulkops.go).
	mu      sync.Mutex
	running map[int64]*runningBulkOp

	// nextOpID hands out unique domain.BulkOperationProgressEvent.OperationID
	// values, starting at 1 (atomic.Int64's zero value is 0, so the first
	// Add(1) below yields 1 - see bulkops.go's nextOperationID).
	nextOpID atomic.Int64
}

// NewFileManagerService returns a FileManagerService backed by repo, using
// encryptionKey to decrypt profile credentials on demand (see
// connection.ResolveProfile). encryptionKey is expected to be derived once
// at application startup (see crypto.DeriveKey) and passed in already
// computed, mirroring NewConnectionService. connMgr/breaker back the bulk
// operations added in Этап 4 (see the connMgr/breaker fields' doc comment) -
// breaker is expected to be the same instance passed to
// transfer.NewTransferService.
func NewFileManagerService(repo *storage.ProfileRepository, encryptionKey [32]byte, connMgr *s3client.ConnectionManager, breaker *s3client.CircuitBreaker) *FileManagerService {
	return &FileManagerService{
		repo:          repo,
		encryptionKey: encryptionKey,
		cache:         newListCache(),
		connMgr:       connMgr,
		breaker:       breaker,
		running:       make(map[int64]*runningBulkOp),
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
