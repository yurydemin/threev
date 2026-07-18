package s3client

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"threev/internal/crypto"
	"threev/internal/domain"
	"threev/internal/storage"
)

// testEncryptionKey is a fixed (test-only) 32-byte encryption key, matching
// the pattern used by connection/service_test.go.
func testEncryptionKey() [32]byte {
	var key [32]byte
	for i := range key {
		key[i] = byte(i)
	}

	return key
}

// newTestConnectionManager opens a fresh migrated SQLite database backed by
// a temporary file and returns a ConnectionManager over it, with a fresh
// *crypto.KeyBox already Set to testEncryptionKey() (mirroring the state
// app.go's newApp leaves it in when no master password is configured - see
// TestConnectionManagerGetReturnsErrLockedWhenLocked for the dedicated
// locked-state test, which builds its own KeyBox instead).
func newTestConnectionManager(t *testing.T) (*ConnectionManager, *storage.ProfileRepository) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "connection_manager_test.db")

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

	keyBox := crypto.NewKeyBox()
	keyBox.Set(testEncryptionKey())

	return NewConnectionManager(repo, keyBox), repo
}

// createTestProfile encrypts secret/session token with the manager's test
// key (mirroring what ConnectionService.SaveProfile does) and persists the
// profile directly through repo, returning the saved (still-encrypted)
// profile.
func createTestProfile(t *testing.T, repo *storage.ProfileRepository, name string) domain.Profile {
	t.Helper()

	key := testEncryptionKey()

	encryptedSecret, err := crypto.Encrypt([]byte("plaintext-secret"), key)
	if err != nil {
		t.Fatalf("crypto.Encrypt(secret) returned error: %v", err)
	}

	encryptedToken, err := crypto.Encrypt([]byte("plaintext-session-token"), key)
	if err != nil {
		t.Fatalf("crypto.Encrypt(session token) returned error: %v", err)
	}

	p := domain.Profile{
		Name:            name,
		EndpointURL:     "https://s3.example.com",
		Region:          "us-east-1",
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: encryptedSecret,
		SessionToken:    encryptedToken,
		PathStyle:       true,
		VerifySSL:       true,
		CustomHeaders:   map[string]string{"X-Custom-Header": "value"},
	}

	saved, err := repo.Create(context.Background(), p)
	if err != nil {
		t.Fatalf("repo.Create() returned error: %v", err)
	}

	return saved
}

// TestProfileHashDiffersOnProxyURLChange is the regression test for
// cache-invalidation-on-proxy-edit: profileHash must produce a different
// fingerprint when only ProxyURL differs between two otherwise-identical
// profiles, so ConnectionManager.Get correctly rebuilds a profile's cached
// clients after its proxy is added, changed, or removed.
func TestProfileHashDiffersOnProxyURLChange(t *testing.T) {
	t.Parallel()

	base := domain.Profile{
		EndpointURL:     "https://s3.example.com",
		Region:          "us-east-1",
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "secret",
		SessionToken:    "token",
		PathStyle:       true,
		VerifySSL:       true,
	}

	withoutProxy := base
	withoutProxy.ProxyURL = ""

	withProxy := base
	withProxy.ProxyURL = "socks5://user:pass@proxy.example.com:1080"

	hashWithoutProxy := profileHash(withoutProxy)
	hashWithProxy := profileHash(withProxy)

	if hashWithoutProxy == hashWithProxy {
		t.Error("profileHash() unchanged when only ProxyURL differs, want distinct hashes")
	}
}

func TestConnectionManagerGetReturnsClients(t *testing.T) {
	t.Parallel()

	mgr, repo := newTestConnectionManager(t)
	profile := createTestProfile(t, repo, "get-clients")

	pooled, fresh, err := mgr.Get(profile.ID)
	if err != nil {
		t.Fatalf("Get(%d) returned error: %v", profile.ID, err)
	}

	if pooled == nil {
		t.Error("Get() pooled client is nil")
	}
	if fresh == nil {
		t.Error("Get() fresh client is nil")
	}
	if pooled == fresh {
		t.Error("Get() pooled and fresh clients are the same instance, want distinct clients/transports")
	}
}

func TestConnectionManagerGetCachesClientsForUnchangedProfile(t *testing.T) {
	t.Parallel()

	mgr, repo := newTestConnectionManager(t)
	profile := createTestProfile(t, repo, "cached")

	pooled1, fresh1, err := mgr.Get(profile.ID)
	if err != nil {
		t.Fatalf("first Get(%d) returned error: %v", profile.ID, err)
	}

	pooled2, fresh2, err := mgr.Get(profile.ID)
	if err != nil {
		t.Fatalf("second Get(%d) returned error: %v", profile.ID, err)
	}

	if pooled1 != pooled2 {
		t.Error("pooled client changed across Get() calls for an unchanged profile, want cached instance reused")
	}
	if fresh1 != fresh2 {
		t.Error("fresh client changed across Get() calls for an unchanged profile, want cached instance reused")
	}
}

func TestConnectionManagerGetRebuildsClientsOnProfileChange(t *testing.T) {
	t.Parallel()

	mgr, repo := newTestConnectionManager(t)
	profile := createTestProfile(t, repo, "changed")

	pooled1, fresh1, err := mgr.Get(profile.ID)
	if err != nil {
		t.Fatalf("first Get(%d) returned error: %v", profile.ID, err)
	}

	// Change a transport-affecting field (region) and persist it, simulating
	// the profile being edited via ConnectionService.SaveProfile between two
	// Get calls.
	profile.Region = "eu-west-1"
	if _, err := repo.Update(context.Background(), profile); err != nil {
		t.Fatalf("repo.Update() returned error: %v", err)
	}

	pooled2, fresh2, err := mgr.Get(profile.ID)
	if err != nil {
		t.Fatalf("second Get(%d) returned error: %v", profile.ID, err)
	}

	if pooled1 == pooled2 {
		t.Error("pooled client unchanged after profile edit, want rebuilt instance")
	}
	if fresh1 == fresh2 {
		t.Error("fresh client unchanged after profile edit, want rebuilt instance")
	}
}

func TestConnectionManagerGetUnknownProfile(t *testing.T) {
	t.Parallel()

	mgr, _ := newTestConnectionManager(t)

	if _, _, err := mgr.Get(999); err == nil {
		t.Fatal("Get() for a nonexistent profile returned nil error, want error")
	}
}

func TestConnectionManagerInvalidateForcesRebuild(t *testing.T) {
	t.Parallel()

	mgr, repo := newTestConnectionManager(t)
	profile := createTestProfile(t, repo, "invalidate")

	pooled1, _, err := mgr.Get(profile.ID)
	if err != nil {
		t.Fatalf("first Get(%d) returned error: %v", profile.ID, err)
	}

	mgr.Invalidate(profile.ID)

	pooled2, _, err := mgr.Get(profile.ID)
	if err != nil {
		t.Fatalf("second Get(%d) returned error: %v", profile.ID, err)
	}

	if pooled1 == pooled2 {
		t.Error("pooled client unchanged after Invalidate(), want rebuilt instance")
	}
}

func TestConnectionManagerInvalidateUnknownProfileIsNoop(t *testing.T) {
	t.Parallel()

	mgr, _ := newTestConnectionManager(t)

	// Must not panic or error for a profile that was never cached.
	mgr.Invalidate(999)
}

// TestConnectionManagerGetReturnsErrLockedWhenLocked verifies Get's Этап 4
// суб-этап 4.4 guard: a ConnectionManager backed by an empty *crypto.KeyBox
// (never Set) returns domain.ErrLocked rather than attempting to decrypt
// anything, even for a profile that would otherwise resolve successfully.
func TestConnectionManagerGetReturnsErrLockedWhenLocked(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "connection_manager_locked_test.db")

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
	mgr := NewConnectionManager(repo, crypto.NewKeyBox())

	profile := createTestProfile(t, repo, "locked")

	_, _, err = mgr.Get(profile.ID)
	if !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("Get() on a locked manager error = %v, want errors.Is(_, domain.ErrLocked)", err)
	}
}
