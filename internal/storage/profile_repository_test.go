package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"threev/internal/crypto"
	"threev/internal/domain"
)

// newTestProfileRepository opens a fresh migrated SQLite database backed by
// a temporary file and returns a ProfileRepository over it.
func newTestProfileRepository(t *testing.T) *ProfileRepository {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "profiles_test.db")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB(%q) returned error: %v", dbPath, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close() returned error: %v", err)
		}
	})

	return NewProfileRepository(db)
}

// newTestProfileRepositoryWithDB is newTestProfileRepository's counterpart
// for tests that also need direct *sql.DB access (ReencryptSecretsTx's own
// tests, below: they need to open transactions themselves, and one test
// needs to hand-corrupt a row via a raw db.Exec to exercise the rollback
// path).
func newTestProfileRepositoryWithDB(t *testing.T) (*ProfileRepository, *sql.DB) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "profiles_reencrypt_test.db")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB(%q) returned error: %v", dbPath, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close() returned error: %v", err)
		}
	})

	return NewProfileRepository(db), db
}

// reencryptTestKey returns a fixed (test-only) 32-byte key, filled with
// seed in every byte - distinct seeds give distinct, comparable keys for
// ReencryptSecretsTx's old/new key tests.
func reencryptTestKey(seed byte) [32]byte {
	var key [32]byte
	for i := range key {
		key[i] = seed
	}

	return key
}

// createEncryptedTestProfile creates and persists a profile named name
// whose SecretAccessKey/SessionToken are encrypted with key (mirroring what
// ConnectionService.SaveProfile does in production), returning the saved
// (still-encrypted) profile.
func createEncryptedTestProfile(t *testing.T, repo *ProfileRepository, key [32]byte, name string) domain.Profile {
	t.Helper()

	encryptedSecret, err := crypto.Encrypt([]byte("plaintext-secret-"+name), key)
	if err != nil {
		t.Fatalf("crypto.Encrypt(secret) returned error: %v", err)
	}

	encryptedToken, err := crypto.Encrypt([]byte("plaintext-token-"+name), key)
	if err != nil {
		t.Fatalf("crypto.Encrypt(session token) returned error: %v", err)
	}

	saved, err := repo.Create(context.Background(), domain.Profile{
		Name:            name,
		EndpointURL:     "https://s3.example.com",
		Region:          "us-east-1",
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: encryptedSecret,
		SessionToken:    encryptedToken,
		PathStyle:       true,
		VerifySSL:       true,
	})
	if err != nil {
		t.Fatalf("Create(%q) returned error: %v", name, err)
	}

	return saved
}

// TestProfileRepositoryReencryptSecretsTxReencryptsAllProfiles verifies the
// core contract: after a committed ReencryptSecretsTx(oldKey, newKey), every
// profile's SecretAccessKey/SessionToken decrypts correctly under newKey and
// no longer decrypts under oldKey.
func TestProfileRepositoryReencryptSecretsTxReencryptsAllProfiles(t *testing.T) {
	t.Parallel()

	repo, db := newTestProfileRepositoryWithDB(t)
	ctx := context.Background()

	oldKey := reencryptTestKey(0x01)
	newKey := reencryptTestKey(0x02)

	profiles := []domain.Profile{
		createEncryptedTestProfile(t, repo, oldKey, "alpha"),
		createEncryptedTestProfile(t, repo, oldKey, "bravo"),
		createEncryptedTestProfile(t, repo, oldKey, "charlie"),
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx() returned error: %v", err)
	}

	if err := repo.ReencryptSecretsTx(ctx, tx, oldKey, newKey); err != nil {
		t.Fatalf("ReencryptSecretsTx() returned error: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() returned error: %v", err)
	}

	for _, p := range profiles {
		got, err := repo.GetByID(ctx, p.ID)
		if err != nil {
			t.Fatalf("GetByID(%d) returned error: %v", p.ID, err)
		}

		wantSecret := "plaintext-secret-" + p.Name
		wantToken := "plaintext-token-" + p.Name

		decryptedSecret, err := crypto.Decrypt(got.SecretAccessKey, newKey)
		if err != nil {
			t.Fatalf("profile %q: Decrypt(SecretAccessKey, newKey) returned error: %v", p.Name, err)
		}
		if string(decryptedSecret) != wantSecret {
			t.Errorf("profile %q: SecretAccessKey decrypted with newKey = %q, want %q", p.Name, decryptedSecret, wantSecret)
		}

		decryptedToken, err := crypto.Decrypt(got.SessionToken, newKey)
		if err != nil {
			t.Fatalf("profile %q: Decrypt(SessionToken, newKey) returned error: %v", p.Name, err)
		}
		if string(decryptedToken) != wantToken {
			t.Errorf("profile %q: SessionToken decrypted with newKey = %q, want %q", p.Name, decryptedToken, wantToken)
		}

		if _, err := crypto.Decrypt(got.SecretAccessKey, oldKey); err == nil {
			t.Errorf("profile %q: Decrypt(SecretAccessKey, oldKey) succeeded after reencryption, want error", p.Name)
		}
	}
}

// TestProfileRepositoryReencryptSecretsTxSkipsEmptySessionToken verifies
// that a profile with no SessionToken at all (the optional-field case
// ConnectionService.SaveProfile already documents) round-trips through
// ReencryptSecretsTx without error and stays empty - never encrypting an
// empty string into something crypto.Decrypt would then fail to parse back
// out as "".
func TestProfileRepositoryReencryptSecretsTxSkipsEmptySessionToken(t *testing.T) {
	t.Parallel()

	repo, db := newTestProfileRepositoryWithDB(t)
	ctx := context.Background()

	oldKey := reencryptTestKey(0x03)
	newKey := reencryptTestKey(0x04)

	encryptedSecret, err := crypto.Encrypt([]byte("plaintext-secret"), oldKey)
	if err != nil {
		t.Fatalf("crypto.Encrypt(secret) returned error: %v", err)
	}

	saved, err := repo.Create(ctx, domain.Profile{
		Name:            "no-token",
		EndpointURL:     "https://s3.example.com",
		Region:          "us-east-1",
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: encryptedSecret,
		SessionToken:    "",
		PathStyle:       true,
		VerifySSL:       true,
	})
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx() returned error: %v", err)
	}

	if err := repo.ReencryptSecretsTx(ctx, tx, oldKey, newKey); err != nil {
		t.Fatalf("ReencryptSecretsTx() returned error: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() returned error: %v", err)
	}

	got, err := repo.GetByID(ctx, saved.ID)
	if err != nil {
		t.Fatalf("GetByID(%d) returned error: %v", saved.ID, err)
	}

	if got.SessionToken != "" {
		t.Errorf("SessionToken = %q, want empty", got.SessionToken)
	}

	decryptedSecret, err := crypto.Decrypt(got.SecretAccessKey, newKey)
	if err != nil {
		t.Fatalf("Decrypt(SecretAccessKey, newKey) returned error: %v", err)
	}
	if string(decryptedSecret) != "plaintext-secret" {
		t.Errorf("SecretAccessKey decrypted with newKey = %q, want %q", decryptedSecret, "plaintext-secret")
	}
}

// TestProfileRepositoryReencryptSecretsTxRollsBackOnCorruptedRow is the
// key "commit only after every row succeeds" regression test: one profile's
// secret_access_key is hand-corrupted (invalid base64, which crypto.Decrypt
// itself would reject) directly via db.Exec (bypassing the repository/
// crypto layer entirely - simulating a corrupted database row rather than
// anything ReencryptSecretsTx's own caller could produce), so
// ReencryptSecretsTx must fail on that row - and, critically, the caller
// rolling back the transaction on that error must leave EVERY profile's
// ciphertext (including the two healthy ones) completely untouched, still
// decryptable under oldKey.
func TestProfileRepositoryReencryptSecretsTxRollsBackOnCorruptedRow(t *testing.T) {
	t.Parallel()

	repo, db := newTestProfileRepositoryWithDB(t)
	ctx := context.Background()

	oldKey := reencryptTestKey(0x05)
	newKey := reencryptTestKey(0x06)

	healthy1 := createEncryptedTestProfile(t, repo, oldKey, "healthy-one")
	corrupted := createEncryptedTestProfile(t, repo, oldKey, "corrupted")
	healthy2 := createEncryptedTestProfile(t, repo, oldKey, "healthy-two")

	if _, err := db.ExecContext(ctx, `UPDATE profiles SET secret_access_key = ? WHERE id = ?`, "not-valid-base64!!!", corrupted.ID); err != nil {
		t.Fatalf("corrupt profile %d: %v", corrupted.ID, err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx() returned error: %v", err)
	}

	err = repo.ReencryptSecretsTx(ctx, tx, oldKey, newKey)
	if err == nil {
		t.Fatal("ReencryptSecretsTx() returned nil error, want an error for the corrupted row")
	}

	if rbErr := tx.Rollback(); rbErr != nil {
		t.Fatalf("Rollback() returned error: %v", rbErr)
	}

	// Every profile - including the two healthy ones - must still decrypt
	// under oldKey, exactly as before the failed attempt: the corrupted
	// row's failure must not have left any partial writes committed.
	for _, p := range []domain.Profile{healthy1, healthy2} {
		got, err := repo.GetByID(ctx, p.ID)
		if err != nil {
			t.Fatalf("GetByID(%d) returned error: %v", p.ID, err)
		}

		decrypted, err := crypto.Decrypt(got.SecretAccessKey, oldKey)
		if err != nil {
			t.Fatalf("profile %q: Decrypt(SecretAccessKey, oldKey) returned error after rollback: %v", p.Name, err)
		}

		want := "plaintext-secret-" + p.Name
		if string(decrypted) != want {
			t.Errorf("profile %q: SecretAccessKey decrypted with oldKey after rollback = %q, want %q", p.Name, decrypted, want)
		}
	}

	// The corrupted row itself must also be untouched (still the
	// hand-corrupted value, not silently rewritten).
	gotCorrupted, err := repo.GetByID(ctx, corrupted.ID)
	if err != nil {
		t.Fatalf("GetByID(%d) returned error: %v", corrupted.ID, err)
	}
	if gotCorrupted.SecretAccessKey != "not-valid-base64!!!" {
		t.Errorf("corrupted profile SecretAccessKey after rollback = %q, want unchanged %q", gotCorrupted.SecretAccessKey, "not-valid-base64!!!")
	}
}

func sampleProfile(name string) domain.Profile {
	return domain.Profile{
		Name:            name,
		EndpointURL:     "https://s3.example.com",
		Region:          "us-east-1",
		AccessKeyID:     "encrypted-access-key",
		SecretAccessKey: "encrypted-secret-key",
		SessionToken:    "encrypted-session-token",
		PathStyle:       true,
		VerifySSL:       false,
		CustomHeaders:   map[string]string{"X-Custom-Header": "value"},
	}
}

func TestProfileRepositoryCreateAndGetByID(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, sampleProfile("prod"))
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	if created.ID == 0 {
		t.Fatal("Create() did not populate ID")
	}
	if created.CreatedAt.IsZero() {
		t.Error("Create() did not populate CreatedAt")
	}
	if created.UpdatedAt.IsZero() {
		t.Error("Create() did not populate UpdatedAt")
	}

	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID(%d) returned error: %v", created.ID, err)
	}

	if got.Name != "prod" {
		t.Errorf("Name = %q, want %q", got.Name, "prod")
	}
	if got.EndpointURL != created.EndpointURL {
		t.Errorf("EndpointURL = %q, want %q", got.EndpointURL, created.EndpointURL)
	}
	if got.AccessKeyID != created.AccessKeyID {
		t.Errorf("AccessKeyID = %q, want %q", got.AccessKeyID, created.AccessKeyID)
	}
	if got.SecretAccessKey != created.SecretAccessKey {
		t.Errorf("SecretAccessKey = %q, want %q", got.SecretAccessKey, created.SecretAccessKey)
	}
	if got.SessionToken != created.SessionToken {
		t.Errorf("SessionToken = %q, want %q", got.SessionToken, created.SessionToken)
	}
	if got.PathStyle != true {
		t.Errorf("PathStyle = %v, want true", got.PathStyle)
	}
	if got.VerifySSL != false {
		t.Errorf("VerifySSL = %v, want false", got.VerifySSL)
	}
	if !reflect.DeepEqual(got.CustomHeaders, map[string]string{"X-Custom-Header": "value"}) {
		t.Errorf("CustomHeaders = %#v, want %#v", got.CustomHeaders, map[string]string{"X-Custom-Header": "value"})
	}
}

func TestProfileRepositoryCreateWithoutOptionalFields(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	p := sampleProfile("minimal")
	p.SessionToken = ""
	p.CustomHeaders = nil

	created, err := repo.Create(ctx, p)
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID(%d) returned error: %v", created.ID, err)
	}

	if got.SessionToken != "" {
		t.Errorf("SessionToken = %q, want empty", got.SessionToken)
	}
	if got.CustomHeaders != nil {
		t.Errorf("CustomHeaders = %#v, want nil", got.CustomHeaders)
	}
}

func TestProfileRepositoryGetByIDNotFound(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, 999)
	if !errors.Is(err, domain.ErrProfileNotFound) {
		t.Fatalf("GetByID() error = %v, want errors.Is(_, domain.ErrProfileNotFound)", err)
	}
}

func TestProfileRepositoryGetAllOrderedByName(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	for _, name := range []string{"charlie", "alpha", "bravo"} {
		if _, err := repo.Create(ctx, sampleProfile(name)); err != nil {
			t.Fatalf("Create(%q) returned error: %v", name, err)
		}
	}

	all, err := repo.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll() returned error: %v", err)
	}

	if len(all) != 3 {
		t.Fatalf("GetAll() returned %d profiles, want 3", len(all))
	}

	wantOrder := []string{"alpha", "bravo", "charlie"}
	for i, want := range wantOrder {
		if all[i].Name != want {
			t.Errorf("GetAll()[%d].Name = %q, want %q", i, all[i].Name, want)
		}
	}
}

func TestProfileRepositoryGetAllEmpty(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	all, err := repo.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll() returned error: %v", err)
	}

	if len(all) != 0 {
		t.Fatalf("GetAll() returned %d profiles, want 0", len(all))
	}
}

func TestProfileRepositoryUpdate(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, sampleProfile("to-update"))
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	created.Name = "updated-name"
	created.Region = "eu-west-1"
	created.PathStyle = false
	created.VerifySSL = true

	updated, err := repo.Update(ctx, created)
	if err != nil {
		t.Fatalf("Update() returned error: %v", err)
	}

	if updated.Name != "updated-name" {
		t.Errorf("Name = %q, want %q", updated.Name, "updated-name")
	}
	if updated.Region != "eu-west-1" {
		t.Errorf("Region = %q, want %q", updated.Region, "eu-west-1")
	}
	if updated.PathStyle != false {
		t.Errorf("PathStyle = %v, want false", updated.PathStyle)
	}
	if updated.VerifySSL != true {
		t.Errorf("VerifySSL = %v, want true", updated.VerifySSL)
	}
	if updated.UpdatedAt.IsZero() {
		t.Error("Update() did not populate UpdatedAt")
	}

	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID() returned error: %v", err)
	}
	if got.Name != "updated-name" {
		t.Errorf("persisted Name = %q, want %q", got.Name, "updated-name")
	}
}

func TestProfileRepositoryUpdateNotFound(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	p := sampleProfile("ghost")
	p.ID = 999

	_, err := repo.Update(ctx, p)
	if !errors.Is(err, domain.ErrProfileNotFound) {
		t.Fatalf("Update() error = %v, want errors.Is(_, domain.ErrProfileNotFound)", err)
	}
}

func TestProfileRepositoryDelete(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, sampleProfile("to-delete"))
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete() returned error: %v", err)
	}

	_, err = repo.GetByID(ctx, created.ID)
	if !errors.Is(err, domain.ErrProfileNotFound) {
		t.Fatalf("GetByID() after Delete() error = %v, want errors.Is(_, domain.ErrProfileNotFound)", err)
	}
}

func TestProfileRepositoryDeleteNotFound(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	err := repo.Delete(ctx, 999)
	if !errors.Is(err, domain.ErrProfileNotFound) {
		t.Fatalf("Delete() error = %v, want errors.Is(_, domain.ErrProfileNotFound)", err)
	}
}

func TestProfileRepositoryCreateDuplicateName(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, sampleProfile("dup")); err != nil {
		t.Fatalf("first Create() returned error: %v", err)
	}

	_, err := repo.Create(ctx, sampleProfile("dup"))
	if !errors.Is(err, domain.ErrDuplicateProfileName) {
		t.Fatalf("second Create() error = %v, want errors.Is(_, domain.ErrDuplicateProfileName)", err)
	}
}

func TestProfileRepositoryUpdateDuplicateName(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, sampleProfile("first")); err != nil {
		t.Fatalf("Create(first) returned error: %v", err)
	}

	second, err := repo.Create(ctx, sampleProfile("second"))
	if err != nil {
		t.Fatalf("Create(second) returned error: %v", err)
	}

	second.Name = "first"

	_, err = repo.Update(ctx, second)
	if !errors.Is(err, domain.ErrDuplicateProfileName) {
		t.Fatalf("Update() error = %v, want errors.Is(_, domain.ErrDuplicateProfileName)", err)
	}
}

func TestProfileRepositoryExistsByName(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, sampleProfile("existing"))
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	tests := []struct {
		name      string
		checkName string
		excludeID int64
		want      bool
	}{
		{name: "new name does not exist", checkName: "brand-new", excludeID: 0, want: false},
		{name: "existing name without exclusion", checkName: "existing", excludeID: 0, want: true},
		{name: "existing name excluding itself", checkName: "existing", excludeID: created.ID, want: false},
		{name: "existing name excluding a different id", checkName: "existing", excludeID: created.ID + 1, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := repo.ExistsByName(ctx, tt.checkName, tt.excludeID)
			if err != nil {
				t.Fatalf("ExistsByName() returned error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ExistsByName(%q, %d) = %v, want %v", tt.checkName, tt.excludeID, got, tt.want)
			}
		})
	}
}
