package connection

import (
	"context"
	"fmt"

	"threev/internal/crypto"
	"threev/internal/domain"
	"threev/internal/storage"
)

// ConnectionService implements the Wails-bound API described in
// docs/02-tech-spec.md section 9.1: CRUD for connection profiles plus an
// explicit connection test. Every exported method here is bound directly to
// the frontend by Wails, so its parameter and return types must be JSON
// serializable - domain.Profile, domain.ProfileDTO, and
// domain.ConnectionTestResult all satisfy that already.
//
// ConnectionService owns the encryption boundary for stored credentials: it
// is the only layer that ever handles decrypted SecretAccessKey/
// SessionToken values outside of an in-flight S3 client call.
// storage.ProfileRepository stores whatever strings it is given verbatim
// (it never encrypts/decrypts), so ConnectionService is responsible for
// encrypting those two fields before they reach the repository, and
// decrypting them back out for the one caller that genuinely needs
// plaintext (GetProfile, to populate an edit form).
type ConnectionService struct {
	repo          *storage.ProfileRepository
	encryptionKey [32]byte
}

// NewConnectionService returns a ConnectionService backed by repo, using
// encryptionKey to encrypt/decrypt SecretAccessKey and SessionToken.
// encryptionKey is expected to be derived once at application startup (see
// crypto.DeriveKey) and passed in already computed - ConnectionService
// never derives or persists key material itself.
func NewConnectionService(repo *storage.ProfileRepository, encryptionKey [32]byte) *ConnectionService {
	return &ConnectionService{repo: repo, encryptionKey: encryptionKey}
}

// GetProfiles returns every saved profile as a secret-free ProfileDTO,
// suitable for the connections list screen (docs/03-ux-ui-spec.md section
// 5.2).
//
// Note on context: Wails v2's generated bindings do not give bound methods
// access to a per-call context.Context, so every method on ConnectionService
// uses context.Background() internally. That is acceptable here because
// every underlying operation is either a local SQLite query or an S3 call
// with its own bounded timeout (TestConnection's testTimeout); nothing can
// hang indefinitely for lack of a caller-supplied deadline.
func (c *ConnectionService) GetProfiles() ([]domain.ProfileDTO, error) {
	profiles, err := c.repo.GetAll(context.Background())
	if err != nil {
		return nil, fmt.Errorf("get profiles: %w", err)
	}

	dtos := make([]domain.ProfileDTO, len(profiles))
	for i, p := range profiles {
		dtos[i] = p.ToDTO()
	}

	return dtos, nil
}

// GetProfile returns the full profile identified by id, with
// SecretAccessKey and SessionToken decrypted to plaintext, for populating
// an edit form. AccessKeyID is returned as stored: it is not a secret and
// is never encrypted in the first place.
func (c *ConnectionService) GetProfile(id int64) (domain.Profile, error) {
	p, err := c.repo.GetByID(context.Background(), id)
	if err != nil {
		return domain.Profile{}, fmt.Errorf("get profile %d: %w", id, err)
	}

	secret, err := crypto.Decrypt(p.SecretAccessKey, c.encryptionKey)
	if err != nil {
		return domain.Profile{}, fmt.Errorf("get profile %d: decrypt secret access key: %w", id, err)
	}

	p.SecretAccessKey = string(secret)

	if p.SessionToken != "" {
		token, err := crypto.Decrypt(p.SessionToken, c.encryptionKey)
		if err != nil {
			return domain.Profile{}, fmt.Errorf("get profile %d: decrypt session token: %w", id, err)
		}

		p.SessionToken = string(token)
	}

	return p, nil
}

// SaveProfile creates a new profile (p.ID == 0) or overwrites an existing
// one (p.ID != 0). It performs format validation and enforces name
// uniqueness, but deliberately does NOT call TestConnection itself: saving
// is a soft, data-entry-level operation, and verifying reachability/
// credentials is a separate, explicit action the user triggers with its own
// button (see TestConnection below) - this is a settled decision, not an
// oversight.
//
// Credential handling on save:
//
//   - AccessKeyID is not a secret and is stored/returned exactly as given.
//
//   - SecretAccessKey is required by ValidateProfile, but on update
//     (p.ID != 0) a blank SecretAccessKey is interpreted as "leave the
//     existing secret unchanged" rather than "clear it": before validating,
//     the existing encrypted value is loaded from storage and substituted
//     in, unchanged (not re-encrypted). Any non-blank SecretAccessKey is
//     treated as a new plaintext secret entered by the user and is
//     encrypted before storage. This backfill deliberately happens *before*
//     ValidateProfile runs (ahead of where docs/02-tech-spec.md section 9.1
//     might suggest validation as strictly step one): ValidateProfile is a
//     pure, update-agnostic format checker that has no notion of "keep
//     existing" and unconditionally rejects an empty secret, so resolving
//     the effective secret value first lets an edit form omit the field
//     without ever tripping that check or persisting an empty secret.
//
//   - SessionToken is optional - ValidateProfile does not require it - so a
//     blank value simply means "no session token" and is stored empty
//     (never encrypted). A non-blank value is treated as a new plaintext
//     token and is encrypted. Unlike SecretAccessKey, there is no "keep
//     existing" special case for SessionToken: clearing it is a valid,
//     explicit user action (e.g. switching a profile from STS temporary
//     credentials to permanent ones), and since it is never required,
//     leaving it blank cannot accidentally fail validation the way an
//     empty SecretAccessKey would.
//
//   - The returned Profile has SecretAccessKey and SessionToken cleared to
//     "" rather than echoed back, in either plaintext or ciphertext form.
//     The frontend already holds whatever plaintext it just submitted in
//     its own form state, so sending it back adds no value and only
//     widens the secret's exposure (an extra trip across the Wails IPC
//     boundary). Every other field - including the persisted ID and
//     timestamps - is returned normally, so the caller can e.g. update its
//     local list state from the response.
func (c *ConnectionService) SaveProfile(p domain.Profile) (domain.Profile, error) {
	ctx := context.Background()

	keepExistingSecret := p.ID != 0 && p.SecretAccessKey == ""
	if keepExistingSecret {
		existing, err := c.repo.GetByID(ctx, p.ID)
		if err != nil {
			return domain.Profile{}, fmt.Errorf("save profile: load existing secret: %w", err)
		}

		p.SecretAccessKey = existing.SecretAccessKey
	}

	if err := ValidateProfile(p); err != nil {
		return domain.Profile{}, err
	}

	exists, err := c.repo.ExistsByName(ctx, p.Name, p.ID)
	if err != nil {
		return domain.Profile{}, fmt.Errorf("save profile: check name uniqueness: %w", err)
	}

	if exists {
		return domain.Profile{}, domain.ErrDuplicateProfileName
	}

	if !keepExistingSecret {
		encrypted, err := crypto.Encrypt([]byte(p.SecretAccessKey), c.encryptionKey)
		if err != nil {
			return domain.Profile{}, fmt.Errorf("save profile: encrypt secret access key: %w", err)
		}

		p.SecretAccessKey = encrypted
	}

	if p.SessionToken != "" {
		encrypted, err := crypto.Encrypt([]byte(p.SessionToken), c.encryptionKey)
		if err != nil {
			return domain.Profile{}, fmt.Errorf("save profile: encrypt session token: %w", err)
		}

		p.SessionToken = encrypted
	}

	var saved domain.Profile
	if p.ID == 0 {
		saved, err = c.repo.Create(ctx, p)
	} else {
		saved, err = c.repo.Update(ctx, p)
	}

	if err != nil {
		return domain.Profile{}, fmt.Errorf("save profile: %w", err)
	}

	saved.SecretAccessKey = ""
	saved.SessionToken = ""

	return saved, nil
}

// DeleteProfile removes the profile identified by id.
func (c *ConnectionService) DeleteProfile(id int64) error {
	if err := c.repo.Delete(context.Background(), id); err != nil {
		return fmt.Errorf("delete profile %d: %w", id, err)
	}

	return nil
}

// TestConnection verifies that p's endpoint is reachable and its
// credentials are valid, by delegating directly to the package-level
// TestConnection function (tester.go).
//
// p is used exactly as given by the caller and is never looked up in, or
// merged with, a stored profile. "Test connection" is meant to validate a
// profile's in-progress edit-form state, which is typically tested before
// it is ever saved (and so may not even have an ID yet, or may hold edits
// not yet persisted) - going through the repository/decryption path here
// would be both unnecessary and wrong. The frontend is responsible for
// sending real plaintext credentials in p, exactly as it would for
// SaveProfile.
//
// The package-level TestConnection this delegates to never itself returns
// a Go error: every outcome, success or failure, is reported through the
// returned domain.ConnectionTestResult. The error return on this method
// exists purely to match the Wails-bound signature from
// docs/02-tech-spec.md section 9.1, and is always nil.
func (c *ConnectionService) TestConnection(p domain.Profile) (domain.ConnectionTestResult, error) {
	return TestConnection(context.Background(), p), nil
}
