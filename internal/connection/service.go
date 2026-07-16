package connection

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

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
	repo   *storage.ProfileRepository
	keyBox *crypto.KeyBox

	// wailsCtx holds ctxHolder (never a bare context.Context - see its own
	// doc comment for why), set once via SetContext from App.startup once
	// the real Wails runtime context is available. Until then (including
	// for every test in this package, which never calls SetContext),
	// ExportProfiles/ImportProfiles's requireWailsContext call fails with
	// errWailsContextNotSet - unlike transfer.TransferService.wailsCtx/
	// filemanager.FileManagerService.wailsCtx (both best-effort, no-op-until-
	// set progress event plumbing), ExportProfiles/ImportProfiles genuinely
	// cannot show a system file dialog without a real Wails runtime context,
	// so an unset wailsCtx is treated as an error here, mirroring
	// transfer.TransferService's Pick* dialog methods (internal/transfer/
	// dialogs.go) rather than its emitProgressEvent.
	wailsCtx atomic.Value
}

// ctxHolder wraps a context.Context so it can be stored in an atomic.Value:
// atomic.Value.Store panics if called with values of two different concrete
// types across calls, which a bare context.Context interface value cannot
// safely guarantee (different context implementations satisfy it) -
// wrapping it in a single, fixed struct type sidesteps that entirely, at the
// cost of one extra field access on load. Identical to (but a distinct type
// from, deliberately not imported) transfer.ctxHolder/filemanager.ctxHolder -
// see filemanager.runningBulkOp's doc comment for why each package that
// needs this small pattern gets its own copy rather than a shared
// cross-package dependency.
type ctxHolder struct {
	ctx context.Context //nolint:containedctx // held only so requireWailsContext can hand the real Wails runtime context to ExportProfiles/ImportProfiles's dialog calls; see ConnectionService.wailsCtx's doc comment.
}

// SetContext installs the real Wails runtime context (from App.startup),
// enabling ExportProfiles/ImportProfiles to actually show their file
// dialogs from this point on. Safe to call at most once in production
// (App.startup runs once), but idempotent/safe to call repeatedly
// regardless (e.g. from a test) - identical contract to
// transfer.TransferService.SetContext/filemanager.FileManagerService.
// SetContext.
func (c *ConnectionService) SetContext(ctx context.Context) {
	c.wailsCtx.Store(ctxHolder{ctx: ctx})
}

// errWailsContextNotSet is returned by requireWailsContext before
// SetContext has ever been called - see ConnectionService.wailsCtx's doc
// comment for why an unset context is a real error here rather than a
// silent no-op, matching transfer.errWailsContextNotSet's identical
// rationale for the same class of "must show a real OS dialog" method.
var errWailsContextNotSet = errors.New("wails runtime context is not set yet")

// requireWailsContext returns the real Wails runtime context installed by
// SetContext (App.startup), or errWailsContextNotSet if that has not
// happened yet - mirrors transfer.TransferService.requireWailsContext.
func (c *ConnectionService) requireWailsContext() (context.Context, error) {
	holder, ok := c.wailsCtx.Load().(ctxHolder)
	if !ok || holder.ctx == nil {
		return nil, errWailsContextNotSet
	}

	return holder.ctx, nil
}

// NewConnectionService returns a ConnectionService backed by repo, reading
// the current encryption key from keyBox on every call that needs one
// rather than taking a fixed [32]byte at construction time (Этап 4 суб-этап
// 4.4, KeyBox: see crypto.KeyBox's own doc comment for why a master
// password means the key can no longer be a constructor-time constant).
// keyBox is expected to be shared with every other service app.go's newApp
// constructs (FileManagerService, TransferService, s3client.
// ConnectionManager) and, when a master password is configured, is filled
// in later by appsettings.SettingsService.Unlock rather than at
// construction time.
func NewConnectionService(repo *storage.ProfileRepository, keyBox *crypto.KeyBox) *ConnectionService {
	return &ConnectionService{repo: repo, keyBox: keyBox}
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
//
// Guarded (Этап 4 суб-этап 4.4): decrypting SecretAccessKey/SessionToken
// requires the current encryption key, which is unavailable while the
// application is locked (a master password is configured but Unlock has
// not yet succeeded this process lifetime) - see domain.ErrLocked's own
// doc comment.
func (c *ConnectionService) GetProfile(id int64) (domain.Profile, error) {
	key, ok := c.keyBox.Get()
	if !ok {
		return domain.Profile{}, domain.ErrLocked
	}

	return ResolveProfile(context.Background(), c.repo, key, id)
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
//
// Guarded (Этап 4 суб-этап 4.4): encrypting a new/changed SecretAccessKey or
// SessionToken requires the current encryption key, unavailable while the
// application is locked - see GetProfile's identical guard/domain.ErrLocked
// doc comment. The guard runs before any repository access at all (even the
// keepExistingSecret path's read-only GetByID below), so a locked
// application never partially validates/looks up a profile only to fail on
// the encryption step.
func (c *ConnectionService) SaveProfile(p domain.Profile) (domain.Profile, error) {
	key, ok := c.keyBox.Get()
	if !ok {
		return domain.Profile{}, domain.ErrLocked
	}

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
		encrypted, err := crypto.Encrypt([]byte(p.SecretAccessKey), key)
		if err != nil {
			return domain.Profile{}, fmt.Errorf("save profile: encrypt secret access key: %w", err)
		}

		p.SecretAccessKey = encrypted
	}

	if p.SessionToken != "" {
		encrypted, err := crypto.Encrypt([]byte(p.SessionToken), key)
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
