// Package appsettings implements the Wails-bound backend for the Settings
// screen (FR-SET-001, Этап 4 суб-этап 4.3): reading/writing a
// domain.AppSettings, persisted one "settings" table row per field via
// internal/storage's generic GetSetting/SetSetting helpers, and pushing the
// Transfers-section fields live into a *transfer.TransferService.
//
// It also implements the master-password backend (SEC-001, Этап 4
// суб-этап 4.4, security.go): IsLocked/Unlock/SetMasterPassword/
// RemoveMasterPassword. See security.go's own doc comments for the full
// design; SettingsService.db/salt/keyBox/profileRepo below are shared
// between both halves of this package (settings-persistence and
// master-password) rather than split into two separate structs, since both
// halves are Wails-bound on the exact same SettingsService instance.
package appsettings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"threev/internal/crypto"
	"threev/internal/domain"
	"threev/internal/storage"
	"threev/internal/transfer"
)

// Settings table keys - one per domain.AppSettings field (see that type's
// doc comment for why one row each, not a single JSON blob).
const (
	keyTheme                     = "theme"
	keyUIScalePercent            = "ui_scale_percent"
	keyCloseBehavior             = "close_behavior"
	keyAutoResumeQueue           = "auto_resume_queue"
	keyMaxConcurrentTransfers    = "max_concurrent_transfers"
	keyPartSizeOverrideMB        = "part_size_override_mb"
	keyBandwidthLimitUploadBPS   = "bandwidth_limit_upload_bps"
	keyBandwidthLimitDownloadBPS = "bandwidth_limit_download_bps"
)

// Per-field defaults, applied by GetSettings whenever a given key's row does
// not exist yet (or, defensively, holds a value that fails to parse - see
// the getXxxSetting helpers below). These MUST match the application's
// behavior before this Block existed exactly: a user who has never opened
// the Settings screen (and therefore has no "settings" table rows for any
// of these keys at all) must observe zero difference in behavior from
// before this package existed.
const (
	defaultTheme            = "system"
	defaultUIScalePercent   = 100
	defaultCloseBehavior    = "exit"
	defaultAutoResumeQueue  = false
	defaultPartSizeOverride = 0
	// defaultBandwidthLimitBPS applies to both directions - 0 means
	// "unlimited", matching transfer.NewBandwidthLimiter's own convention
	// (see domain.AppSettings.BandwidthLimitUploadBytesPerSec's doc
	// comment).
	defaultBandwidthLimitBPS int64 = 0
)

// Validation/clamp bounds SaveSettings enforces (UX-спека 5.7) before ever
// persisting or applying a settings value - see SaveSettings' own doc
// comment for the "silent clamp, never reject" philosophy this follows,
// matching internal/filemanager's clampPresignExpiry precedent.
const (
	minUIScalePercent = 90
	maxUIScalePercent = 125

	minMaxConcurrentTransfers = 1
	maxMaxConcurrentTransfers = 10

	minPartSizeOverrideMB = 5
	maxPartSizeOverrideMB = 128
)

// validThemes/validCloseBehaviors are the only values SaveSettings ever
// persists for their respective fields - anything else falls back to that
// field's default (see SaveSettings).
var (
	validThemes         = map[string]bool{"system": true, "light": true, "dark": true}
	validCloseBehaviors = map[string]bool{"exit": true, "confirm": true}
)

// SettingsService implements the Wails-bound API for the Settings screen
// (FR-SET-001), backed directly by db (for its own settings-row
// persistence) and transferService (to push its Transfers-section fields
// into live effect - see ApplySettings).
//
// Unlike every other *Service in this codebase (ConnectionService,
// FileManagerService, TransferService), which all deliberately avoid
// depending on one another as a service-to-service coupling - see
// TransferService's own doc comment: it takes the same
// *storage.ProfileRepository/encryption key app.go already constructed and
// resolves a profile itself via connection.ResolveProfile, rather than
// taking a *connection.ConnectionService, precisely to avoid this kind of
// coupling - SettingsService intentionally DOES depend directly on
// *transfer.TransferService. This is not an accidental violation of that
// established principle: the pattern being avoided elsewhere is one service
// reaching into another to duplicate or reuse THAT SERVICE'S OWN domain
// logic (e.g. resolving/decrypting a profile a second, redundant way).
// SettingsService's reason to exist is categorically different - its whole
// job is to configure another service's LIVE, in-memory runtime state
// (TransferService's concurrency limit, bandwidth limiter, part-size
// override) in direct response to a user's Settings changes, which is
// simply impossible without holding a reference to the very service being
// configured. Should a future settings consumer beyond TransferService ever
// exist, it should follow this same direct-dependency pattern, not the
// avoided ResolveProfile one.
//
// profileRepo/keyBox/salt (Этап 4 суб-этап 4.4, security.go) extend this
// same reasoning rather than violating it further: SetMasterPassword/
// RemoveMasterPassword need to (a) run a re-encryption transaction directly
// against every stored profile's ciphertext and (b) mutate the live,
// shared *crypto.KeyBox every other service reads its encryption key from.
// Neither of those is "resolve a profile a second, redundant way" (the
// pattern TransferService's own doc comment describes every *Service in
// this codebase as avoiding) - SettingsService does not build its own S3
// client or duplicate connection.ResolveProfile's decrypt logic anywhere;
// it owns the re-encryption transaction itself (via
// storage.ProfileRepository.ReencryptSecretsTx) precisely because no other
// existing layer has a reason to ever touch every profile's ciphertext at
// once. keyBox is the SAME *crypto.KeyBox instance app.go's newApp shares
// with ConnectionService/FileManagerService/TransferService/
// s3client.ConnectionManager - SettingsService.Unlock/SetMasterPassword/
// RemoveMasterPassword are the only places in the entire codebase that ever
// call KeyBox.Set, which is exactly why they live here rather than on any
// other *Service.
type SettingsService struct {
	db              *sql.DB
	transferService *transfer.TransferService

	// profileRepo/keyBox/salt back the master-password backend
	// (security.go) - see this struct's own doc comment above for why they
	// live here rather than on a separate type.
	profileRepo *storage.ProfileRepository
	keyBox      *crypto.KeyBox
	salt        []byte
}

// NewSettingsService returns a SettingsService backed by db (settings-row
// persistence), transferService (ApplySettings' target), profileRepo
// (SetMasterPassword/RemoveMasterPassword's re-encryption transaction,
// security.go), keyBox (the shared *crypto.KeyBox every service reads its
// encryption key from - Unlock/SetMasterPassword/RemoveMasterPassword are
// this codebase's only KeyBox.Set call sites), and salt (the same
// crypto_salt app.go's newApp already resolves once at startup via
// resolveCryptoSalt, passed in already decoded - see crypto.DeriveKey's own
// doc comment for why the SAME salt is reused for both the machine-only and
// password-derived key, rather than a second, dedicated "master password
// salt").
func NewSettingsService(db *sql.DB, transferService *transfer.TransferService, profileRepo *storage.ProfileRepository, keyBox *crypto.KeyBox, salt []byte) *SettingsService {
	return &SettingsService{
		db:              db,
		transferService: transferService,
		profileRepo:     profileRepo,
		keyBox:          keyBox,
		salt:            salt,
	}
}

// GetSettings reads every domain.AppSettings field from its own "settings"
// table row, defaulting any field whose row does not exist yet to that
// field's specific default (see the defaultXxx constants above) - a
// per-field lazy default, NOT storage.GetOrCreateSetting's eager
// persist-on-first-read pattern: the very first GetSettings() call on a
// brand-new database must not, as a side effect, write eight rows that were
// never asked for. Only a genuine database error (anything other than
// sql.ErrNoRows) is propagated.
func (s *SettingsService) GetSettings() (domain.AppSettings, error) {
	ctx := context.Background()

	theme, err := getStringSetting(ctx, s.db, keyTheme, defaultTheme)
	if err != nil {
		return domain.AppSettings{}, err
	}

	uiScalePercent, err := getIntSetting(ctx, s.db, keyUIScalePercent, defaultUIScalePercent)
	if err != nil {
		return domain.AppSettings{}, err
	}

	closeBehavior, err := getStringSetting(ctx, s.db, keyCloseBehavior, defaultCloseBehavior)
	if err != nil {
		return domain.AppSettings{}, err
	}

	autoResumeQueue, err := getBoolSetting(ctx, s.db, keyAutoResumeQueue, defaultAutoResumeQueue)
	if err != nil {
		return domain.AppSettings{}, err
	}

	maxConcurrentTransfers, err := getIntSetting(ctx, s.db, keyMaxConcurrentTransfers, transfer.DefaultMaxConcurrentTasks)
	if err != nil {
		return domain.AppSettings{}, err
	}

	partSizeOverrideMB, err := getIntSetting(ctx, s.db, keyPartSizeOverrideMB, defaultPartSizeOverride)
	if err != nil {
		return domain.AppSettings{}, err
	}

	bandwidthLimitUploadBPS, err := getInt64Setting(ctx, s.db, keyBandwidthLimitUploadBPS, defaultBandwidthLimitBPS)
	if err != nil {
		return domain.AppSettings{}, err
	}

	bandwidthLimitDownloadBPS, err := getInt64Setting(ctx, s.db, keyBandwidthLimitDownloadBPS, defaultBandwidthLimitBPS)
	if err != nil {
		return domain.AppSettings{}, err
	}

	return domain.AppSettings{
		Theme:                             theme,
		UIScalePercent:                    uiScalePercent,
		CloseBehavior:                     closeBehavior,
		AutoResumeQueue:                   autoResumeQueue,
		MaxConcurrentTransfers:            maxConcurrentTransfers,
		PartSizeOverrideMB:                partSizeOverrideMB,
		BandwidthLimitUploadBytesPerSec:   bandwidthLimitUploadBPS,
		BandwidthLimitDownloadBytesPerSec: bandwidthLimitDownloadBPS,
	}, nil
}

// SaveSettings clamps/validates settings (see the bounds/valid-value
// constants above), persists every field as its own "settings" row, and
// then calls ApplySettings with the CLAMPED settings - never the raw input
// - so a live TransferService setter can never receive an out-of-range
// value SaveSettings itself just rejected.
//
// Validation never fails/returns an error for an out-of-range or
// unrecognized value - every field is silently clamped (numeric fields) or
// reset to its default (Theme/CloseBehavior) instead, the same
// "silent clamp, never reject" philosophy internal/filemanager's
// clampPresignExpiry already established for this codebase: a Settings
// screen is not the place to reject a slider value the UI itself should
// already be constraining, and a defensive clamp here is simply cheap
// insurance against a frontend bug or a hand-crafted Wails call bypassing
// the UI entirely.
func (s *SettingsService) SaveSettings(settings domain.AppSettings) error {
	clamped := clampSettings(settings)

	ctx := context.Background()

	if err := storage.SetSetting(ctx, s.db, keyTheme, clamped.Theme); err != nil {
		return err
	}
	if err := storage.SetSetting(ctx, s.db, keyUIScalePercent, strconv.Itoa(clamped.UIScalePercent)); err != nil {
		return err
	}
	if err := storage.SetSetting(ctx, s.db, keyCloseBehavior, clamped.CloseBehavior); err != nil {
		return err
	}
	if err := storage.SetSetting(ctx, s.db, keyAutoResumeQueue, strconv.FormatBool(clamped.AutoResumeQueue)); err != nil {
		return err
	}
	if err := storage.SetSetting(ctx, s.db, keyMaxConcurrentTransfers, strconv.Itoa(clamped.MaxConcurrentTransfers)); err != nil {
		return err
	}
	if err := storage.SetSetting(ctx, s.db, keyPartSizeOverrideMB, strconv.Itoa(clamped.PartSizeOverrideMB)); err != nil {
		return err
	}
	if err := storage.SetSetting(ctx, s.db, keyBandwidthLimitUploadBPS, strconv.FormatInt(clamped.BandwidthLimitUploadBytesPerSec, 10)); err != nil {
		return err
	}
	if err := storage.SetSetting(ctx, s.db, keyBandwidthLimitDownloadBPS, strconv.FormatInt(clamped.BandwidthLimitDownloadBytesPerSec, 10)); err != nil {
		return err
	}

	s.ApplySettings(clamped)

	return nil
}

// ApplySettings pushes settings' Transfers-section fields into the live
// s.transferService, without touching persistence at all - called by
// SaveSettings (persist-then-apply, with the already-clamped settings) AND
// by app.go's newApp() at boot (apply-only, immediately after reading
// already-persisted settings via GetSettings - which must NOT be
// re-persisted right back, since GetSettings' own lazy-default behavior
// deliberately avoids writing anything for a field that was never actually
// configured).
//
// Only three of domain.AppSettings' fields have a live, in-memory
// TransferService counterpart to push; Theme/UIScalePercent/CloseBehavior/
// AutoResumeQueue have no backend runtime state of their own to configure
// (Theme/UIScalePercent/CloseBehavior are pure frontend presentation state;
// AutoResumeQueue is only ever consulted once, at startup, by app.go's own
// call to TransferService.AutoResumeIfEnabled - not something ApplySettings
// itself pushes anywhere).
func (s *SettingsService) ApplySettings(settings domain.AppSettings) {
	s.transferService.SetMaxConcurrentTasks(settings.MaxConcurrentTransfers)
	s.transferService.SetBandwidthLimits(settings.BandwidthLimitUploadBytesPerSec, settings.BandwidthLimitDownloadBytesPerSec)
	s.transferService.SetPartSizeOverrideMB(settings.PartSizeOverrideMB)
}

// clampSettings returns settings with every field validated/clamped per
// SaveSettings' documented bounds.
func clampSettings(settings domain.AppSettings) domain.AppSettings {
	if !validThemes[settings.Theme] {
		settings.Theme = defaultTheme
	}

	settings.UIScalePercent = clampInt(settings.UIScalePercent, minUIScalePercent, maxUIScalePercent)

	if !validCloseBehaviors[settings.CloseBehavior] {
		settings.CloseBehavior = defaultCloseBehavior
	}

	settings.MaxConcurrentTransfers = clampInt(settings.MaxConcurrentTransfers, minMaxConcurrentTransfers, maxMaxConcurrentTransfers)

	if settings.PartSizeOverrideMB > 0 {
		settings.PartSizeOverrideMB = clampInt(settings.PartSizeOverrideMB, minPartSizeOverrideMB, maxPartSizeOverrideMB)
	} else {
		settings.PartSizeOverrideMB = 0
	}

	if settings.BandwidthLimitUploadBytesPerSec < 0 {
		settings.BandwidthLimitUploadBytesPerSec = 0
	}
	if settings.BandwidthLimitDownloadBytesPerSec < 0 {
		settings.BandwidthLimitDownloadBytesPerSec = 0
	}

	return settings
}

// clampInt clamps v to [minVal, maxVal].
func clampInt(v, minVal, maxVal int) int {
	if v < minVal {
		return minVal
	}
	if v > maxVal {
		return maxVal
	}

	return v
}

// getStringSetting returns the string stored under key, or def if no such
// row exists (sql.ErrNoRows) - see GetSettings' doc comment for why this is
// a per-field lazy default, not storage.GetOrCreateSetting's eager,
// persist-on-first-read pattern.
func getStringSetting(ctx context.Context, db *sql.DB, key, def string) (string, error) {
	value, err := storage.GetSetting(ctx, db, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return def, nil
		}

		return "", fmt.Errorf("get setting %q: %w", key, err)
	}

	return value, nil
}

// getIntSetting is getStringSetting's int-parsing counterpart. A row that
// exists but fails to parse as an int (which SaveSettings itself never
// produces, but a hand-edited database row theoretically could) falls back
// to def as well, rather than propagating a parse error - the same
// defensive, never-reject spirit as SaveSettings' own clamping.
func getIntSetting(ctx context.Context, db *sql.DB, key string, def int) (int, error) {
	value, err := storage.GetSetting(ctx, db, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return def, nil
		}

		return 0, fmt.Errorf("get setting %q: %w", key, err)
	}

	parsed, parseErr := strconv.Atoi(value)
	if parseErr != nil {
		return def, nil
	}

	return parsed, nil
}

// getInt64Setting is getIntSetting's int64 counterpart, for the bandwidth
// limit fields.
func getInt64Setting(ctx context.Context, db *sql.DB, key string, def int64) (int64, error) {
	value, err := storage.GetSetting(ctx, db, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return def, nil
		}

		return 0, fmt.Errorf("get setting %q: %w", key, err)
	}

	parsed, parseErr := strconv.ParseInt(value, 10, 64)
	if parseErr != nil {
		return def, nil
	}

	return parsed, nil
}

// getBoolSetting is getStringSetting's bool counterpart, for
// AutoResumeQueue.
func getBoolSetting(ctx context.Context, db *sql.DB, key string, def bool) (bool, error) {
	value, err := storage.GetSetting(ctx, db, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return def, nil
		}

		return false, fmt.Errorf("get setting %q: %w", key, err)
	}

	parsed, parseErr := strconv.ParseBool(value)
	if parseErr != nil {
		return def, nil
	}

	return parsed, nil
}
