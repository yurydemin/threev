package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// GetOrCreateSetting reads the value stored under key in the "settings"
// table. If no row exists for key yet, it calls generate to produce a new
// value, persists it, and returns it. Subsequent calls with the same key
// then return the persisted value unchanged.
//
// This is a minimal, single-purpose helper - not a general SettingsService
// (that is a later stage, per docs/02-tech-spec.md section 9.4). It exists
// so callers such as the application's startup sequence (deriving the
// encryption key) can lazily create and persist a single value, e.g. the
// KDF salt under the "crypto_salt" key, without depending on a full
// settings layer.
func GetOrCreateSetting(ctx context.Context, db *sql.DB, key string, generate func() (string, error)) (string, error) {
	value, err := GetSetting(ctx, db, key)
	if err == nil {
		return value, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	generated, err := generate()
	if err != nil {
		return "", fmt.Errorf("generate setting %q: %w", key, err)
	}

	if err := SetSetting(ctx, db, key, generated); err != nil {
		return "", err
	}

	return generated, nil
}

// GetSetting returns the value stored under key, or sql.ErrNoRows if no
// such row exists.
//
// Exported (rather than the package-private getSetting it started out as)
// so callers outside this package - namely internal/appsettings.
// SettingsService.GetSettings, which reads each of its own settings keys
// individually and applies a per-field default on sql.ErrNoRows rather than
// GetOrCreateSetting's eager persist-on-first-read behavior - can read a
// single key without going through a generate callback.
func GetSetting(ctx context.Context, db *sql.DB, key string) (string, error) {
	var value sql.NullString

	err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", err
		}

		return "", fmt.Errorf("get setting %q: %w", key, err)
	}

	return value.String, nil
}

// sqlExecutor is satisfied by both *sql.DB and *sql.Tx (both expose an
// identical ExecContext signature) - SetSetting/DeleteSetting accept this
// instead of a concrete *sql.DB so a caller that already owns a
// transaction (internal/appsettings.SettingsService.reencryptTx, Этап 4
// суб-этап 4.4) can write/delete a settings row as part of that SAME
// transaction, rather than as a separate, non-atomic follow-up write after
// the transaction has already committed - see reencryptTx's own doc
// comment for why that atomicity is load-bearing (SetMasterPassword/
// RemoveMasterPassword's master-password verifier row must never become
// durably inconsistent with which key the stored profiles were actually
// re-encrypted to).
type sqlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// SetSetting upserts key/value into the "settings" table. Exported for the
// same reason as GetSetting - internal/appsettings.SettingsService.
// SaveSettings writes each of its settings keys individually. Accepts
// sqlExecutor (not a concrete *sql.DB) so it can also be called with a
// *sql.Tx - see sqlExecutor's own doc comment.
func SetSetting(ctx context.Context, db sqlExecutor, key, value string) error {
	const query = `
INSERT INTO settings (key, value) VALUES (?, ?)
ON CONFLICT (key) DO UPDATE SET value = excluded.value`

	if _, err := db.ExecContext(ctx, query, key, value); err != nil {
		return fmt.Errorf("set setting %q: %w", key, err)
	}

	return nil
}

// DeleteSetting removes the row stored under key, if any. Idempotent - it
// is not an error to delete a key that does not exist (RemoveMasterPassword,
// internal/appsettings/security.go, calls this to remove the master-password
// verifier row, and the removal must succeed even if called twice or against
// a database that somehow never had the row). Accepts sqlExecutor (not a
// concrete *sql.DB) so it can also be called with a *sql.Tx - see
// sqlExecutor's own doc comment.
func DeleteSetting(ctx context.Context, db sqlExecutor, key string) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key); err != nil {
		return fmt.Errorf("delete setting %q: %w", key, err)
	}

	return nil
}
