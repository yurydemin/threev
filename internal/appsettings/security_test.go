package appsettings

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	tcrypto "threev/internal/crypto"
	"threev/internal/domain"
)

// TestIsLockedReflectsKeyBoxState verifies IsLocked's direct KeyBox.Get
// mirroring: true before any key is installed, false once one is (via
// Set, standing in for a real Unlock success in these unit tests that
// don't need the full password round trip).
func TestIsLockedReflectsKeyBoxState(t *testing.T) {
	t.Parallel()

	deps := newTestSettingsService(t)

	if deps.settingsSvc.IsLocked() {
		t.Fatal("IsLocked() = true immediately after newTestSettingsService (which already Set a key), want false")
	}

	deps.keyBox.Clear()

	if !deps.settingsSvc.IsLocked() {
		t.Fatal("IsLocked() = false after keyBox.Clear(), want true")
	}
}

// TestHasMasterPasswordMethodReflectsVerifierRowRegardlessOfLockState
// verifies the (*SettingsService).HasMasterPassword method - unlike
// IsLocked, which only reports the current KeyBox state - genuinely tracks
// whether a verifier row exists: false on a freshly constructed service (no
// password ever set), true once SetMasterPassword has run.
func TestHasMasterPasswordMethodReflectsVerifierRowRegardlessOfLockState(t *testing.T) {
	t.Parallel()

	deps := newTestSettingsService(t)

	hasPassword, err := deps.settingsSvc.HasMasterPassword()
	if err != nil {
		t.Fatalf("HasMasterPassword() returned error: %v", err)
	}
	if hasPassword {
		t.Error("HasMasterPassword() on a fresh service = true, want false")
	}

	if err := deps.settingsSvc.SetMasterPassword("correct horse battery staple"); err != nil {
		t.Fatalf("SetMasterPassword() returned error: %v", err)
	}

	hasPassword, err = deps.settingsSvc.HasMasterPassword()
	if err != nil {
		t.Fatalf("HasMasterPassword() returned error: %v", err)
	}
	if !hasPassword {
		t.Error("HasMasterPassword() after SetMasterPassword() = false, want true")
	}
}

// TestSetMasterPasswordReencryptsProfilesAndInstallsNewKey is the core
// SetMasterPassword contract test: after a successful call, every stored
// profile's SecretAccessKey/SessionToken decrypts under the NEW
// (password-derived) key - not the old one - the verifier row is
// persisted, and s.keyBox holds the new key.
func TestSetMasterPasswordReencryptsProfilesAndInstallsNewKey(t *testing.T) {
	t.Parallel()

	deps := newTestSettingsService(t)
	ctx := context.Background()

	profileID := createTestProfile(t, deps.profileRepo, deps.key, "https://s3.example.com")

	if err := deps.settingsSvc.SetMasterPassword("correct horse battery staple"); err != nil {
		t.Fatalf("SetMasterPassword() returned error: %v", err)
	}

	newKey, ok := deps.keyBox.Get()
	if !ok {
		t.Fatal("keyBox.Get() after SetMasterPassword() returned ok=false, want true")
	}
	if newKey == deps.key {
		t.Fatal("keyBox holds the same key after SetMasterPassword(), want a different, password-derived key")
	}

	wantKey, err := tcrypto.DeriveKey("correct horse battery staple", deps.salt)
	if err != nil {
		t.Fatalf("DeriveKey() returned error: %v", err)
	}
	if newKey != wantKey {
		t.Errorf("keyBox key = %v, want DeriveKey(password, salt) = %v", newKey, wantKey)
	}

	stored, err := deps.profileRepo.GetByID(ctx, profileID)
	if err != nil {
		t.Fatalf("GetByID(%d) returned error: %v", profileID, err)
	}

	decrypted, err := tcrypto.Decrypt(stored.SecretAccessKey, newKey)
	if err != nil {
		t.Fatalf("Decrypt(SecretAccessKey, newKey) returned error: %v", err)
	}
	if string(decrypted) != "supersecret" {
		t.Errorf("SecretAccessKey decrypted with newKey = %q, want %q", decrypted, "supersecret")
	}

	if _, err := tcrypto.Decrypt(stored.SecretAccessKey, deps.key); err == nil {
		t.Error("Decrypt(SecretAccessKey, oldKey) succeeded after SetMasterPassword(), want error")
	}

	hasPassword, err := HasMasterPassword(ctx, deps.settingsSvc.db)
	if err != nil {
		t.Fatalf("HasMasterPassword() returned error: %v", err)
	}
	if !hasPassword {
		t.Error("HasMasterPassword() after SetMasterPassword() = false, want true")
	}
}

// TestSetMasterPasswordWhenLockedReturnsErrLocked verifies SetMasterPassword
// requires an already-unlocked application (an existing key to re-encrypt
// FROM) - see its own doc comment for why.
func TestSetMasterPasswordWhenLockedReturnsErrLocked(t *testing.T) {
	t.Parallel()

	deps := newTestSettingsService(t)

	deps.keyBox.Clear()

	err := deps.settingsSvc.SetMasterPassword("some password")
	if !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("SetMasterPassword() on a locked service error = %v, want errors.Is(_, domain.ErrLocked)", err)
	}
}

// TestUnlockWithCorrectPasswordInstallsSameKeySetMasterPasswordDid verifies
// the Unlock/SetMasterPassword round trip: after SetMasterPassword(pw), a
// FRESH KeyBox (simulating a new process launch) Unlock(pw)'d against the
// SAME database ends up holding the exact same key SetMasterPassword
// installed.
func TestUnlockWithCorrectPasswordInstallsSameKeySetMasterPasswordDid(t *testing.T) {
	t.Parallel()

	deps := newTestSettingsService(t)

	if err := deps.settingsSvc.SetMasterPassword("correct horse battery staple"); err != nil {
		t.Fatalf("SetMasterPassword() returned error: %v", err)
	}

	wantKey, ok := deps.keyBox.Get()
	if !ok {
		t.Fatal("keyBox.Get() after SetMasterPassword() returned ok=false, want true")
	}

	// Simulate a fresh process launch: a brand new, empty KeyBox, but the
	// same underlying database/salt.
	freshKeyBox := tcrypto.NewKeyBox()
	freshSvc := NewSettingsService(deps.settingsSvc.db, deps.transferSvc, deps.profileRepo, freshKeyBox, deps.salt)

	ok, err := freshSvc.Unlock("correct horse battery staple")
	if err != nil {
		t.Fatalf("Unlock() returned error: %v", err)
	}
	if !ok {
		t.Fatal("Unlock() with the correct password returned false, want true")
	}

	gotKey, isSet := freshKeyBox.Get()
	if !isSet {
		t.Fatal("keyBox.Get() after Unlock() returned ok=false, want true")
	}
	if gotKey != wantKey {
		t.Errorf("Unlock() installed key %v, want the same key SetMasterPassword installed (%v)", gotKey, wantKey)
	}
}

// TestUnlockWithIncorrectPasswordLeavesKeyBoxUntouched verifies Unlock's
// failure path: a wrong password returns (false, nil) and must not
// install anything into the KeyBox.
func TestUnlockWithIncorrectPasswordLeavesKeyBoxUntouched(t *testing.T) {
	t.Parallel()

	deps := newTestSettingsService(t)

	if err := deps.settingsSvc.SetMasterPassword("correct horse battery staple"); err != nil {
		t.Fatalf("SetMasterPassword() returned error: %v", err)
	}

	freshKeyBox := tcrypto.NewKeyBox()
	freshSvc := NewSettingsService(deps.settingsSvc.db, deps.transferSvc, deps.profileRepo, freshKeyBox, deps.salt)

	ok, err := freshSvc.Unlock("wrong password")
	if err != nil {
		t.Fatalf("Unlock() returned error: %v", err)
	}
	if ok {
		t.Fatal("Unlock() with an incorrect password returned true, want false")
	}

	if _, isSet := freshKeyBox.Get(); isSet {
		t.Error("keyBox.Get() after a failed Unlock() returned ok=true, want false (KeyBox must be left untouched)")
	}
}

// TestUnlockWithIncorrectPasswordDoesNotDisturbAlreadyUnlockedKeyBox is
// TestUnlockWithIncorrectPasswordLeavesKeyBoxUntouched's counterpart for a
// KeyBox that already held a (different) key before the failed Unlock call
// - e.g. a stray/defensive Unlock call against an already-unlocked
// application must not clobber the key already in effect.
func TestUnlockWithIncorrectPasswordDoesNotDisturbAlreadyUnlockedKeyBox(t *testing.T) {
	t.Parallel()

	deps := newTestSettingsService(t)

	if err := deps.settingsSvc.SetMasterPassword("correct horse battery staple"); err != nil {
		t.Fatalf("SetMasterPassword() returned error: %v", err)
	}

	beforeKey, ok := deps.keyBox.Get()
	if !ok {
		t.Fatal("keyBox.Get() before the failed Unlock() call returned ok=false, want true")
	}

	unlocked, err := deps.settingsSvc.Unlock("wrong password")
	if err != nil {
		t.Fatalf("Unlock() returned error: %v", err)
	}
	if unlocked {
		t.Fatal("Unlock() with an incorrect password returned true, want false")
	}

	afterKey, ok := deps.keyBox.Get()
	if !ok {
		t.Fatal("keyBox.Get() after the failed Unlock() call returned ok=false, want true")
	}
	if afterKey != beforeKey {
		t.Errorf("keyBox key changed after a failed Unlock() call: before=%v after=%v, want unchanged", beforeKey, afterKey)
	}
}

// TestRemoveMasterPasswordWithCorrectPasswordRevertsToMachineKey verifies
// RemoveMasterPassword's core contract: profiles are re-encrypted back to
// the machine-only key (crypto.DeriveKey("", salt)), the verifier row is
// deleted (HasMasterPassword becomes false), and s.keyBox ends up holding
// the machine-only key.
func TestRemoveMasterPasswordWithCorrectPasswordRevertsToMachineKey(t *testing.T) {
	t.Parallel()

	deps := newTestSettingsService(t)
	ctx := context.Background()

	profileID := createTestProfile(t, deps.profileRepo, deps.key, "https://s3.example.com")

	if err := deps.settingsSvc.SetMasterPassword("correct horse battery staple"); err != nil {
		t.Fatalf("SetMasterPassword() returned error: %v", err)
	}

	if err := deps.settingsSvc.RemoveMasterPassword("correct horse battery staple"); err != nil {
		t.Fatalf("RemoveMasterPassword() returned error: %v", err)
	}

	machineKey, err := tcrypto.DeriveKey("", deps.salt)
	if err != nil {
		t.Fatalf("DeriveKey() returned error: %v", err)
	}

	gotKey, ok := deps.keyBox.Get()
	if !ok {
		t.Fatal("keyBox.Get() after RemoveMasterPassword() returned ok=false, want true")
	}
	if gotKey != machineKey {
		t.Errorf("keyBox key after RemoveMasterPassword() = %v, want machine-only key %v", gotKey, machineKey)
	}

	stored, err := deps.profileRepo.GetByID(ctx, profileID)
	if err != nil {
		t.Fatalf("GetByID(%d) returned error: %v", profileID, err)
	}

	decrypted, err := tcrypto.Decrypt(stored.SecretAccessKey, machineKey)
	if err != nil {
		t.Fatalf("Decrypt(SecretAccessKey, machineKey) returned error: %v", err)
	}
	if string(decrypted) != "supersecret" {
		t.Errorf("SecretAccessKey decrypted with machineKey = %q, want %q", decrypted, "supersecret")
	}

	hasPassword, err := HasMasterPassword(ctx, deps.settingsSvc.db)
	if err != nil {
		t.Fatalf("HasMasterPassword() returned error: %v", err)
	}
	if hasPassword {
		t.Error("HasMasterPassword() after RemoveMasterPassword() = true, want false")
	}
}

// TestRemoveMasterPasswordWithIncorrectPasswordChangesNothing verifies
// RemoveMasterPassword's defense-in-depth password check: an incorrect
// currentPassword returns an error and leaves the verifier row, the
// KeyBox, and every stored profile's ciphertext completely untouched.
func TestRemoveMasterPasswordWithIncorrectPasswordChangesNothing(t *testing.T) {
	t.Parallel()

	deps := newTestSettingsService(t)
	ctx := context.Background()

	profileID := createTestProfile(t, deps.profileRepo, deps.key, "https://s3.example.com")

	if err := deps.settingsSvc.SetMasterPassword("correct horse battery staple"); err != nil {
		t.Fatalf("SetMasterPassword() returned error: %v", err)
	}

	passwordDerivedKey, ok := deps.keyBox.Get()
	if !ok {
		t.Fatal("keyBox.Get() after SetMasterPassword() returned ok=false, want true")
	}

	err := deps.settingsSvc.RemoveMasterPassword("wrong password")
	if err == nil {
		t.Fatal("RemoveMasterPassword() with an incorrect password returned nil error, want an error")
	}

	gotKey, ok := deps.keyBox.Get()
	if !ok {
		t.Fatal("keyBox.Get() after a failed RemoveMasterPassword() call returned ok=false, want true")
	}
	if gotKey != passwordDerivedKey {
		t.Errorf("keyBox key changed after a failed RemoveMasterPassword() call: before=%v after=%v, want unchanged", passwordDerivedKey, gotKey)
	}

	hasPassword, err := HasMasterPassword(ctx, deps.settingsSvc.db)
	if err != nil {
		t.Fatalf("HasMasterPassword() returned error: %v", err)
	}
	if !hasPassword {
		t.Error("HasMasterPassword() after a failed RemoveMasterPassword() call = false, want true (verifier must be left in place)")
	}

	stored, err := deps.profileRepo.GetByID(ctx, profileID)
	if err != nil {
		t.Fatalf("GetByID(%d) returned error: %v", profileID, err)
	}

	decrypted, err := tcrypto.Decrypt(stored.SecretAccessKey, passwordDerivedKey)
	if err != nil {
		t.Fatalf("Decrypt(SecretAccessKey, passwordDerivedKey) returned error after a failed RemoveMasterPassword() call: %v", err)
	}
	if string(decrypted) != "supersecret" {
		t.Errorf("SecretAccessKey decrypted with passwordDerivedKey = %q, want %q", decrypted, "supersecret")
	}
}

// TestSetMasterPasswordRollsBackOnCorruptedProfileRow is the key
// "commit only after every row succeeds" regression test at the
// SettingsService layer (see SetMasterPassword's own doc comment and
// storage.ProfileRepository.ReencryptSecretsTx's identical test at the
// storage layer): one profile's secret_access_key is hand-corrupted
// directly via db.Exec (bypassing the repository/crypto layer entirely),
// so SetMasterPassword must fail - and, critically, must leave s.keyBox
// and the verifier row EXACTLY as they were before the call (no new key
// installed, no verifier written), not just roll back the database rows.
func TestSetMasterPasswordRollsBackOnCorruptedProfileRow(t *testing.T) {
	t.Parallel()

	deps := newTestSettingsService(t)
	ctx := context.Background()

	profileID := createTestProfile(t, deps.profileRepo, deps.key, "https://s3.example.com")

	if _, err := deps.settingsSvc.db.ExecContext(ctx, `UPDATE profiles SET secret_access_key = ? WHERE id = ?`, "not-valid-base64!!!", profileID); err != nil {
		t.Fatalf("corrupt profile %d: %v", profileID, err)
	}

	beforeKey, ok := deps.keyBox.Get()
	if !ok {
		t.Fatal("keyBox.Get() before SetMasterPassword() returned ok=false, want true")
	}

	err := deps.settingsSvc.SetMasterPassword("correct horse battery staple")
	if err == nil {
		t.Fatal("SetMasterPassword() over a corrupted profile row returned nil error, want an error")
	}

	afterKey, ok := deps.keyBox.Get()
	if !ok {
		t.Fatal("keyBox.Get() after a failed SetMasterPassword() call returned ok=false, want true")
	}
	if afterKey != beforeKey {
		t.Errorf("keyBox key changed after a failed SetMasterPassword() call: before=%v after=%v, want unchanged", beforeKey, afterKey)
	}

	hasPassword, err := HasMasterPassword(ctx, deps.settingsSvc.db)
	if err != nil {
		t.Fatalf("HasMasterPassword() returned error: %v", err)
	}
	if hasPassword {
		t.Error("HasMasterPassword() after a failed SetMasterPassword() call = true, want false (no verifier should have been persisted)")
	}

	stored, err := deps.profileRepo.GetByID(ctx, profileID)
	if err != nil {
		t.Fatalf("GetByID(%d) returned error: %v", profileID, err)
	}
	if stored.SecretAccessKey != "not-valid-base64!!!" {
		t.Errorf("corrupted profile SecretAccessKey after failed SetMasterPassword() = %q, want unchanged %q", stored.SecretAccessKey, "not-valid-base64!!!")
	}
}

// TestReencryptTxRunsVerifierOpInsideSameTransactionAsReencryption is the
// regression test for the atomicity gap a security review found: a prior
// version of SetMasterPassword/RemoveMasterPassword wrote/deleted the
// master-password verifier row in a SEPARATE transaction, committed AFTER
// the profile re-encryption transaction had already committed on its own -
// leaving an unprotected crash window between the two commits (see
// reencryptTx's, SetMasterPassword's and RemoveMasterPassword's doc
// comments for the two concrete failure scenarios that gap allowed).
//
// This test exercises reencryptTx directly (rather than going through
// SetMasterPassword/RemoveMasterPassword) so it can inject a verifierOp
// that deliberately fails AFTER the profile row has already been
// re-encrypted within the transaction, and assert two things that only
// hold if verifierOp genuinely runs inside the SAME *sql.Tx, before
// Commit, rather than as a separate follow-up transaction:
//
//  1. While verifierOp itself runs, it can see the profile row's
//     ALREADY-re-encrypted ciphertext through the very same *sql.Tx it was
//     handed (proving ReencryptSecretsTx ran first, in the same
//     not-yet-committed transaction, not a prior separate one).
//  2. Once reencryptTx returns the error verifierOp produced, the profile
//     row as read back from a fresh query against the real *sql.DB (i.e.
//     what is actually durable) is completely unchanged - still decryptable
//     under oldKey, not newKey - proving the profile re-encryption was
//     rolled back together with the failed verifier write as a single
//     atomic unit, rather than the re-encryption having already durably
//     committed on its own before verifierOp ever ran.
func TestReencryptTxRunsVerifierOpInsideSameTransactionAsReencryption(t *testing.T) {
	t.Parallel()

	deps := newTestSettingsService(t)
	ctx := context.Background()

	profileID := createTestProfile(t, deps.profileRepo, deps.key, "https://s3.example.com")

	beforeStored, err := deps.profileRepo.GetByID(ctx, profileID)
	if err != nil {
		t.Fatalf("GetByID(%d) returned error: %v", profileID, err)
	}

	newKey, err := tcrypto.DeriveKey("correct horse battery staple", deps.salt)
	if err != nil {
		t.Fatalf("DeriveKey() returned error: %v", err)
	}

	verifierOpErr := errors.New("boom: verifier write deliberately fails")

	var verifierOpSawReencryptedRow bool

	verifierOp := func(ctx context.Context, tx *sql.Tx) error {
		// Read the profile row through the SAME transaction reencryptTx
		// handed us - if ReencryptSecretsTx really ran first inside this
		// not-yet-committed transaction, this read must already see the
		// NEW ciphertext, even though nothing has been committed to the
		// database yet.
		var secretAccessKey string
		if err := tx.QueryRowContext(ctx, `SELECT secret_access_key FROM profiles WHERE id = ?`, profileID).Scan(&secretAccessKey); err != nil {
			t.Fatalf("query profile %d inside verifierOp's tx: %v", profileID, err)
		}

		if _, err := tcrypto.Decrypt(secretAccessKey, newKey); err == nil {
			verifierOpSawReencryptedRow = true
		}

		return verifierOpErr
	}

	err = deps.settingsSvc.reencryptTx(ctx, deps.key, newKey, verifierOp)
	if !errors.Is(err, verifierOpErr) {
		t.Fatalf("reencryptTx() error = %v, want errors.Is(_, verifierOpErr)", err)
	}

	if !verifierOpSawReencryptedRow {
		t.Error("verifierOp did not see the already-re-encrypted profile row through its own tx - verifierOp is not running inside the same transaction as ReencryptSecretsTx")
	}

	afterStored, err := deps.profileRepo.GetByID(ctx, profileID)
	if err != nil {
		t.Fatalf("GetByID(%d) returned error: %v", profileID, err)
	}

	if afterStored.SecretAccessKey != beforeStored.SecretAccessKey {
		t.Errorf("profile SecretAccessKey after reencryptTx()'s verifierOp failure = %q, want unchanged %q (re-encryption must have rolled back together with the failed verifier write)", afterStored.SecretAccessKey, beforeStored.SecretAccessKey)
	}

	decrypted, err := tcrypto.Decrypt(afterStored.SecretAccessKey, deps.key)
	if err != nil {
		t.Fatalf("Decrypt(SecretAccessKey, oldKey) after rolled-back reencryptTx() returned error: %v", err)
	}
	if string(decrypted) != "supersecret" {
		t.Errorf("SecretAccessKey decrypted with oldKey after rollback = %q, want %q", decrypted, "supersecret")
	}

	if _, err := tcrypto.Decrypt(afterStored.SecretAccessKey, newKey); err == nil {
		t.Error("Decrypt(SecretAccessKey, newKey) succeeded after reencryptTx() rolled back, want error (row must not be durably re-encrypted)")
	}
}
