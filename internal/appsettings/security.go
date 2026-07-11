package appsettings

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"

	tcrypto "threev/internal/crypto" // aliased: this file also imports the stdlib crypto/hmac and crypto/sha256 packages, so the short name "crypto" is unavailable - tcrypto avoids the collision.
	"threev/internal/domain"
	"threev/internal/storage"
)

// keyMasterPasswordVerifier is the "settings" table key under which a
// base64-encoded HMAC-SHA256 verifier is stored once a master password is
// set - its presence/absence is the sole signal HasMasterPassword and
// app.go's newApp() use to decide whether the application boots locked
// (KeyBox left empty, waiting for Unlock) or unlocked (KeyBox immediately
// filled with the machine-only key, today's exact behavior before this
// Block existed).
const keyMasterPasswordVerifier = "master_password_verifier"

// masterPasswordVerifierMessage is the fixed message HMAC'd with a
// candidate key to produce/check the stored verifier - a shared constant,
// not a secret; its only job is to give Unlock something to compare against
// without ever needing to decrypt a real stored profile (which may not even
// exist yet on a fresh install with zero saved connections).
var masterPasswordVerifierMessage = []byte("threev-master-password-verifier-v1")

// HasMasterPassword reports whether a master password has been configured
// (a verifier row exists), without needing a constructed *SettingsService -
// called directly by app.go's newApp() before SettingsService itself can be
// built (SettingsService's own constructor now needs a *crypto.KeyBox,
// whose initial Set-or-leave-empty decision depends on this very check - a
// chicken-and-egg ordering only a package-level function, not a method, can
// resolve: newApp cannot construct SettingsService until it already knows
// whether to populate the KeyBox first).
func HasMasterPassword(ctx context.Context, db *sql.DB) (bool, error) {
	_, err := storage.GetSetting(ctx, db, keyMasterPasswordVerifier)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}

		return false, fmt.Errorf("check master password: %w", err)
	}

	return true, nil
}

// computeVerifier returns the base64-encoded HMAC-SHA256 verifier for key -
// the value SetMasterPassword persists and Unlock/RemoveMasterPassword
// compare a candidate key's own verifier against (via verifyCandidate,
// below).
func computeVerifier(key [32]byte) string {
	mac := hmac.New(sha256.New, key[:])
	mac.Write(masterPasswordVerifierMessage)

	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// IsLocked reports whether s's KeyBox currently holds no key - i.e. a
// master password is configured but Unlock has not yet succeeded this
// process lifetime. The frontend calls this exactly once, at startup,
// before rendering anything (Block I, not yet implemented at the time this
// method was written).
func (s *SettingsService) IsLocked() bool {
	_, ok := s.keyBox.Get()
	return !ok
}

// HasMasterPassword reports whether a master password is currently
// configured (a verifier row exists), regardless of whether s is currently
// locked or unlocked - unlike IsLocked (which only ever reports "no key
// installed right now" and cannot distinguish "no password was ever set" from
// "a password is set but this session already unlocked it"), this is what
// the frontend's Security settings section (Этап 4 суб-этап 4.4, Блок I)
// calls to decide which form to show: "Установить пароль" when false,
// "Сменить/удалить пароль" when true. Thin wrapper around the package-level
// HasMasterPassword function app.go's newApp() already uses at startup for
// the same underlying check, before a *SettingsService even exists.
func (s *SettingsService) HasMasterPassword() (bool, error) {
	return HasMasterPassword(context.Background(), s.db)
}

// SetMasterPassword configures password as a new master password (whether
// none was set before, or replacing an existing one): derives a new key via
// crypto.DeriveKey(password, s.salt) (a non-empty passphrase - see
// DeriveKey's own doc comment for why this alone, with no separate
// "combined" derivation function, already produces a key that depends on
// BOTH this machine AND this password), and re-encrypts every stored
// profile's credentials from the currently active key to the new one AND
// writes the new verifier row in a single transaction (see reencryptTx) -
// ONLY after that one transaction commits successfully does it install the
// new key into s.keyBox.
//
// Ordering (commit the re-encryption AND the verifier write TOGETHER, THEN
// - only on success - mutate in-memory state) is deliberate and
// load-bearing: if the transaction fails at any point (a corrupted row, a
// verifier write error, ...), reencryptTx rolls back EVERYTHING it did as
// one unit, s.keyBox is left completely untouched, and the application
// continues working exactly as it did before this call. There is no
// intermediate durable state where the profiles are re-encrypted to newKey
// but the verifier still reflects oldKey (or vice versa) - a prior version
// of this method wrote the verifier in a second, separate transaction
// after the re-encryption transaction had already committed, which a
// security review flagged as an unprotected crash window: a process
// interruption between the two commits could leave the database in a
// state no future process launch could ever correctly recover from
// (HasMasterPassword and the actual re-encrypted key would permanently
// disagree). Committing both together in reencryptTx closes that window
// entirely: either both are durable, or neither is.
//
// Requires the application to already be unlocked (an existing key in
// s.keyBox to re-encrypt FROM) - this covers both "setting a master
// password for the first time" (the existing key is the machine-only one,
// installed automatically at startup, see app.go's newApp) and "changing
// an already-configured master password" (the existing key is the
// previous password-derived one). There is no separate "confirm current
// password" step for the CHANGE case here (unlike RemoveMasterPassword,
// which does require currentPassword) - SEC-001's threat model treats
// "the application is currently unlocked" as sufficient proof of
// authorization to change to a NEW password, since anyone who could open a
// SettingsService method at all already has full access to every
// credential SetMasterPassword itself would re-encrypt.
func (s *SettingsService) SetMasterPassword(password string) error {
	oldKey, ok := s.keyBox.Get()
	if !ok {
		return domain.ErrLocked
	}

	newKey, err := tcrypto.DeriveKey(password, s.salt)
	if err != nil {
		return fmt.Errorf("set master password: derive key: %w", err)
	}

	verifier := computeVerifier(newKey)
	verifierOp := func(ctx context.Context, tx *sql.Tx) error {
		return storage.SetSetting(ctx, tx, keyMasterPasswordVerifier, verifier)
	}

	if err := s.reencryptTx(context.Background(), oldKey, newKey, verifierOp); err != nil {
		return fmt.Errorf("set master password: %w", err)
	}

	s.keyBox.Set(newKey)

	return nil
}

// RemoveMasterPassword requires currentPassword (defense-in-depth: proves
// the caller actually knows the password being removed, not just that the
// application happens to be unlocked right now - see SetMasterPassword's
// doc comment for why THAT method does not require the same proof for a
// same-direction change) and, if it checks out, reverses
// SetMasterPassword: re-encrypts every profile back to the machine-only key
// (crypto.DeriveKey("", s.salt) - the exact derivation app.go's newApp
// already uses when no master password is configured at all) AND deletes
// the verifier row, together in a single transaction (see reencryptTx) -
// ONLY after that transaction commits successfully does it install
// machineKey into s.keyBox. Same commit-both-together-then-mutate-memory
// ordering as SetMasterPassword, for the same reason (see its doc comment):
// there is no durable intermediate state where the profiles are already
// machine-key-encrypted but the verifier row is still present (or vice
// versa) - a prior version of this method deleted the verifier in a
// second, separate transaction after the re-encryption had already
// committed, which left exactly that kind of unprotected crash window; a
// stale verifier surviving a crash there would make a later Unlock attempt
// with the OLD password succeed (it matches the still-present old
// verifier) and install the WRONG key into s.keyBox, since the profiles
// underneath had already moved to machineKey. Committing both together
// closes that window entirely: either both are durable, or neither is.
func (s *SettingsService) RemoveMasterPassword(currentPassword string) error {
	candidateKey, err := tcrypto.DeriveKey(currentPassword, s.salt)
	if err != nil {
		return fmt.Errorf("remove master password: derive key: %w", err)
	}

	if !s.verifyCandidate(candidateKey) {
		return fmt.Errorf("remove master password: incorrect password")
	}

	machineKey, err := tcrypto.DeriveKey("", s.salt)
	if err != nil {
		return fmt.Errorf("remove master password: derive machine key: %w", err)
	}

	verifierOp := func(ctx context.Context, tx *sql.Tx) error {
		return storage.DeleteSetting(ctx, tx, keyMasterPasswordVerifier)
	}

	if err := s.reencryptTx(context.Background(), candidateKey, machineKey, verifierOp); err != nil {
		return fmt.Errorf("remove master password: %w", err)
	}

	s.keyBox.Set(machineKey)

	return nil
}

// Unlock derives a candidate key from password and s.salt and compares its
// verifier against the one stored by SetMasterPassword, using hmac.Equal
// (constant-time comparison - a naive == or bytes.Equal here would leak
// timing information about how many leading bytes of a guessed verifier
// matched, which is exactly the kind of side channel a password check must
// avoid, see verifyCandidate). On a match, installs candidateKey into
// s.keyBox and returns true; on a mismatch, s.keyBox is left completely
// untouched and false is returned.
//
// There is no rate-limiting of failed attempts here - a documented MVP
// limitation (Этап 4 plan's "Known risks": brute-force protection is
// backlog), not an oversight. There is also no Lock() to undo a successful
// Unlock - see crypto.KeyBox's own doc comment for why that is a settled,
// separate decision for this whole Block, not specific to Unlock.
func (s *SettingsService) Unlock(password string) (bool, error) {
	candidateKey, err := tcrypto.DeriveKey(password, s.salt)
	if err != nil {
		return false, fmt.Errorf("unlock: derive key: %w", err)
	}

	if !s.verifyCandidate(candidateKey) {
		return false, nil
	}

	s.keyBox.Set(candidateKey)

	return true, nil
}

// verifyCandidate reports whether candidateKey's verifier matches the one
// stored under keyMasterPasswordVerifier, using hmac.Equal for a
// constant-time comparison (see Unlock's doc comment for why). Returns
// false (never an error) if no verifier row exists at all, or if the
// stored/candidate verifier fails to base64-decode (which SetMasterPassword
// itself never produces, but a hand-edited/corrupted database row
// theoretically could) - Unlock/RemoveMasterPassword's callers are only
// ever expected to call this when a master password IS configured
// (IsLocked()==true implies exactly that), but any of these edge cases is
// handled as "no match" rather than propagating an error or panicking,
// since there is nothing meaningful to unlock against in that case either
// way.
func (s *SettingsService) verifyCandidate(candidateKey [32]byte) bool {
	stored, err := storage.GetSetting(context.Background(), s.db, keyMasterPasswordVerifier)
	if err != nil {
		return false
	}

	storedBytes, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		return false
	}

	candidateBytes, err := base64.StdEncoding.DecodeString(computeVerifier(candidateKey))
	if err != nil {
		return false
	}

	return hmac.Equal(storedBytes, candidateBytes)
}

// reencryptTx runs ProfileRepository.ReencryptSecretsTx AND the given
// verifierOp inside a single transaction owned entirely by this method
// (Begin here, Commit/Rollback here) - SetMasterPassword/
// RemoveMasterPassword both delegate to this shared helper rather than
// duplicating the Begin/defer-Rollback/Commit boilerplate twice. verifierOp
// is called with the same *sql.Tx, after the profile re-encryption
// succeeds but before Commit, so that "every profile is re-encrypted to
// newKey" AND "the verifier row reflects newKey (or is gone, for
// RemoveMasterPassword)" commit or roll back TOGETHER, atomically - closing
// the crash-recovery gap a security review caught: writing/deleting the
// verifier as a SEPARATE, later transaction left a window where a crash
// (or a verifier-write failure) between the two commits could leave the
// durable database permanently inconsistent with what any process launch
// could ever re-derive (see SetMasterPassword/RemoveMasterPassword's own
// doc comments for the two concrete failure scenarios this closes).
//
// See ProfileRepository.ReencryptSecretsTx's own doc comment for what
// happens to already-processed rows if a later row fails (nothing: the
// whole transaction, this method's own Rollback below, undoes every row
// together) - the same guarantee now also covers verifierOp: if it fails,
// every profile row ReencryptSecretsTx already touched is rolled back too.
//
// The defer/Rollback-on-error pattern here mirrors
// storage.TransferQueueRepository.MoveToHistory's identical convention
// (internal/storage/transfer_queue_repository.go): a Rollback failure is
// joined onto the original error via errors.Join rather than silently
// discarded, since - unlike a routine best-effort cleanup - the caller
// (SetMasterPassword/RemoveMasterPassword) explicitly relies on Rollback
// having actually reverted every row before it is safe to report "nothing
// changed" back to its own caller.
func (s *SettingsService) reencryptTx(ctx context.Context, oldKey, newKey [32]byte, verifierOp func(ctx context.Context, tx *sql.Tx) error) (err error) {
	tx, beginErr := s.db.BeginTx(ctx, nil)
	if beginErr != nil {
		return fmt.Errorf("begin transaction: %w", beginErr)
	}

	defer func() {
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				err = errors.Join(err, fmt.Errorf("rollback: %w", rbErr))
			}
		}
	}()

	if err = s.profileRepo.ReencryptSecretsTx(ctx, tx, oldKey, newKey); err != nil {
		return err
	}

	if err = verifierOp(ctx, tx); err != nil {
		return fmt.Errorf("update verifier: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}
