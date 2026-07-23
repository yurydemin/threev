//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"threev/internal/connection"
	"threev/internal/crypto"
	"threev/internal/domain"
	"threev/internal/filemanager"
	"threev/internal/s3client"
	"threev/internal/storage"
	"threev/internal/transfer"
)

// s3CallTimeout bounds every direct (non-service, test-scaffolding) S3 call
// this package makes itself (bucket create/cleanup, PutObject seeding) -
// generous enough for a local Docker MinIO under test load, without letting
// a genuinely hung request stall a whole test run.
const s3CallTimeout = 30 * time.Second

// testEncryptionKey returns the same fixed, deterministic 32-byte
// credential-encryption key every other package's test suite uses (see
// e.g. connection/service_test.go's newTestConnectionService) - there is
// nothing profile-secret-worthy about this key; it exists solely so
// storage.ProfileRepository round-trips a profile's SecretAccessKey
// correctly within a single test's temporary SQLite database.
func testEncryptionKey() [32]byte {
	var key [32]byte
	for i := range key {
		key[i] = byte(i)
	}

	return key
}

// profileNameCounter guarantees every newIntegrationServices call gets a
// unique profile Name within a single test binary run - ConnectionService.
// SaveProfile rejects duplicate names, and multiple tests in this package
// may run in parallel, each creating their own profile.
var profileNameCounter atomic.Int64

// integrationServices bundles ConnectionService/FileManagerService/
// TransferService, constructed with the exact same dependency graph app.go's
// newApp assembles (a shared *s3client.ConnectionManager and
// *s3client.CircuitBreaker across FileManagerService/TransferService, a
// single *crypto.KeyBox already Set/unlocked across all three), plus the
// saved profile id/decrypted domain.Profile every test in this package
// needs to talk to MinIO.
type integrationServices struct {
	conn *connection.ConnectionService
	fm   *filemanager.FileManagerService
	tr   *transfer.TransferService

	profileID int64
	// profile holds the full, decrypted profile (including plaintext
	// AccessKeyID/SecretAccessKey) - unlike the ProfileDTO the connections
	// list screen works with, tests here need real credentials to build
	// their own throwaway *s3.Client (s3client.NewS3Client) for bucket
	// setup/teardown that no Service method exposes (see newTestBucket).
	profile domain.Profile
}

// newIntegrationServices builds a fresh, isolated set of services backed by
// a temporary SQLite database (t.TempDir(), closed via t.Cleanup - the same
// pattern internal/connection/service_test.go's newTestConnectionService
// and internal/transfer/service_test.go's newTestTransferService already
// use) and a single connection profile pointed at the MinIO instance
// configured via THREEV_INTEGRATION_S3_* (main_test.go), saved through
// ConnectionService.SaveProfile exactly as the real Connections screen
// would.
//
// Skips t (via requireMinIO) before doing anything else if TestMain's
// startup health check found MinIO unreachable.
func newIntegrationServices(t *testing.T) integrationServices {
	t.Helper()

	requireMinIO(t)

	dbPath := filepath.Join(t.TempDir(), "integration_test.db")

	db, err := storage.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB(%q) returned error: %v", dbPath, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close() returned error: %v", err)
		}
	})

	repo := storage.NewProfileRepository(db)
	queueRepo := storage.NewTransferQueueRepository(db)
	historyRepo := storage.NewTransferHistoryRepository(db)

	keyBox := crypto.NewKeyBox()
	keyBox.Set(testEncryptionKey())

	// Same *s3client.ConnectionManager/*s3client.CircuitBreaker/
	// *s3client.RetryPolicyStore instance shared between fileManagerService
	// and transferService - see app.go's newApp, which this mirrors exactly.
	connMgr := s3client.NewConnectionManager(repo, keyBox)
	breaker := s3client.NewCircuitBreaker()
	retryPolicies := s3client.NewRetryPolicyStore()

	connSvc := connection.NewConnectionService(repo, keyBox)
	fmSvc := filemanager.NewFileManagerService(repo, keyBox, connMgr, breaker, retryPolicies)
	trSvc := transfer.NewTransferService(repo, keyBox, queueRepo, historyRepo, connMgr, breaker, retryPolicies)

	saved, err := connSvc.SaveProfile(domain.Profile{
		Name:            fmt.Sprintf("integration-test-%d", profileNameCounter.Add(1)),
		EndpointURL:     s3Endpoint(),
		Region:          "us-east-1",
		AccessKeyID:     s3AccessKeyID(),
		SecretAccessKey: s3SecretAccessKey(),
		// PathStyle is required against MinIO: it does not support
		// virtual-hosted-style addressing (bucket.host/key) out of the box
		// the way real AWS S3 does.
		PathStyle: true,
		// A local/CI MinIO instance normally has no TLS in front of it at
		// all (plain http:// endpoint); VerifySSL is meaningless in that
		// case but set to false regardless, matching what a real user
		// pointing threev at a self-hosted MinIO over https with a
		// self-signed certificate would also need.
		VerifySSL: false,
	})
	if err != nil {
		t.Fatalf("SaveProfile() returned error: %v", err)
	}

	full, err := connSvc.GetProfile(saved.ID)
	if err != nil {
		t.Fatalf("GetProfile(%d) returned error: %v", saved.ID, err)
	}

	return integrationServices{
		conn:      connSvc,
		fm:        fmSvc,
		tr:        trSvc,
		profileID: saved.ID,
		profile:   full,
	}
}

// bucketNameCounter guarantees every newTestBucket call gets a unique
// bucket name even when several calls happen within the same nanosecond
// (unlikely but not impossible) or across parallel subtests of the same
// t.Name().
var bucketNameCounter atomic.Int64

// invalidBucketNameChars matches every character not allowed in an S3
// bucket name (only lowercase letters, digits, and hyphens are used here -
// S3 bucket naming rules also permit dots, but those are never produced by
// sanitizeBucketName's inputs and are needlessly stricter to reason about
// with path-style addressing, so they are simply never introduced).
var invalidBucketNameChars = regexp.MustCompile(`[^a-z0-9-]+`)

// maxBucketNameLength is S3's own hard limit on bucket name length.
const maxBucketNameLength = 63

// sanitizeBucketName lowercases raw and replaces every run of characters
// not valid in an S3 bucket name with a single "-", trims any leading/
// trailing "-" left behind, and truncates to maxBucketNameLength - turning
// an arbitrary t.Name() (which may contain "/" from subtests, or upper-case
// letters) into a valid bucket name.
func sanitizeBucketName(raw string) string {
	name := strings.ToLower(raw)
	name = invalidBucketNameChars.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")

	if len(name) > maxBucketNameLength {
		name = strings.Trim(name[:maxBucketNameLength], "-")
	}

	return name
}

// newTestBucket creates a uniquely-named bucket against profile's endpoint
// (built directly via s3client.NewS3Client - no Service in this codebase
// exposes bucket creation; FileManagerService only has CreateFolder, which
// creates an object inside an already-existing bucket, see this package's
// task description) and registers a t.Cleanup that empties (deletes every
// object, paginating through as many ListObjectsV2 pages as needed) and
// then deletes the bucket itself - no test bucket in this package survives
// its own test.
func newTestBucket(t *testing.T, profile domain.Profile) string {
	t.Helper()

	raw := fmt.Sprintf("threev-test-%s-%d-%d", t.Name(), time.Now().UnixNano(), bucketNameCounter.Add(1))
	name := sanitizeBucketName(raw)

	client, err := s3client.NewS3Client(profile)
	if err != nil {
		t.Fatalf("NewS3Client() returned error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), s3CallTimeout)
	defer cancel()

	if _, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(name)}); err != nil {
		t.Fatalf("CreateBucket(%q) returned error: %v", name, err)
	}

	t.Cleanup(func() {
		cleanupBucket(t, client, name)
	})

	return name
}

// cleanupBucket empties bucket (paginating ListObjectsV2/DeleteObjects
// until nothing is left) and then deletes it. Every failure here is
// reported via t.Errorf (not Fatalf): cleanup runs from a deferred
// t.Cleanup, where the test's own assertions have already run - failing
// loudly-but-not-abruptly still surfaces a leaked bucket as a test failure
// without a panic-like abort mid-cleanup that could leave other pending
// t.Cleanup callbacks unrun.
func cleanupBucket(t *testing.T, client *s3.Client, bucket string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), s3CallTimeout)
	defer cancel()

	var continuationToken *string

	for {
		out, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(bucket),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			t.Errorf("cleanup: ListObjectsV2(%q) returned error: %v", bucket, err)
			break
		}

		if len(out.Contents) > 0 {
			objects := make([]types.ObjectIdentifier, len(out.Contents))
			for i, obj := range out.Contents {
				objects[i] = types.ObjectIdentifier{Key: obj.Key}
			}

			if _, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: aws.String(bucket),
				Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)},
			}); err != nil {
				t.Errorf("cleanup: DeleteObjects(%q) returned error: %v", bucket, err)
			}
		}

		if !aws.ToBool(out.IsTruncated) {
			break
		}

		continuationToken = out.NextContinuationToken
	}

	if _, err := client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(bucket)}); err != nil {
		t.Errorf("cleanup: DeleteBucket(%q) returned error: %v", bucket, err)
	}
}

// putTestObject uploads a single small object (body) directly to
// bucket/key against profile's endpoint via a throwaway *s3.Client
// (s3client.NewS3Client) - test-data seeding for filemanager_test.go/
// bulk_test.go, which need existing objects in place before exercising
// FileManagerService's listing/head/bulk methods. Not used by
// transfer_test.go, which deliberately goes through
// TransferService.QueueUpload instead (see that file's own doc comment for
// why: it is what actually exercises the multipart upload path under
// test).
func putTestObject(t *testing.T, profile domain.Profile, bucket, key string, body []byte, contentType string) {
	t.Helper()

	client, err := s3client.NewS3Client(profile)
	if err != nil {
		t.Fatalf("NewS3Client() returned error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), s3CallTimeout)
	defer cancel()

	input := &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(body),
	}
	if contentType != "" {
		input.ContentType = aws.String(contentType)
	}

	if _, err := client.PutObject(ctx, input); err != nil {
		t.Fatalf("PutObject(%q, %q) returned error: %v", bucket, key, err)
	}
}

// waitForCondition polls cond (returning (done, err)) every 100ms until it
// reports done, returns a non-nil error (which fails the test immediately,
// via t.Fatalf), or timeout elapses (which also fails the test) - the
// shared polling primitive bulk_test.go's async delete/copy/move
// assertions use in place of a direct completion signal: neither
// FileManagerService's bulk operations nor this package's tests ever call
// SetContext, so the "bulk:progress" Wails event they would otherwise
// observe is never actually emitted (see FileManagerService.
// emitBulkProgressEvent's own doc comment: a no-op without a real Wails
// runtime context) - polling the observable, S3-visible end state (a
// listing) is the only option available to a black-box test like this one.
func waitForCondition(t *testing.T, timeout time.Duration, desc string, cond func() (bool, error)) {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for {
		done, err := cond()
		if err != nil {
			t.Fatalf("%s: %v", desc, err)
		}
		if done {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("%s: condition not met within %s", desc, timeout)
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// waitForTransferCompletion polls TransferService.GetHistory until taskID
// appears (a transfer task only ever leaves transfer_queue for
// transfer_history once it reaches a terminal state - see
// domain.TransferTask.Status's own doc comment), failing the test
// immediately if it appears with any status other than "completed", or if
// it does not appear at all within timeout.
func waitForTransferCompletion(t *testing.T, tr *transfer.TransferService, taskID int64, timeout time.Duration) domain.TransferHistoryEntry {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for {
		entries, err := tr.GetHistory(200)
		if err != nil {
			t.Fatalf("GetHistory() returned error: %v", err)
		}

		for _, entry := range entries {
			if entry.QueueID != taskID {
				continue
			}

			if entry.Status != "completed" {
				t.Fatalf("transfer task %d finished with status %q, want %q (error: %s)", taskID, entry.Status, "completed", entry.ErrorMessage)
			}

			return entry
		}

		if time.Now().After(deadline) {
			queue, _ := tr.GetQueue()
			t.Fatalf("transfer task %d did not reach transfer_history within %s (queue: %+v)", taskID, timeout, queue)
		}

		time.Sleep(50 * time.Millisecond)
	}
}
