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
	value, err := getSetting(ctx, db, key)
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

	if err := setSetting(ctx, db, key, generated); err != nil {
		return "", err
	}

	return generated, nil
}

// getSetting returns the value stored under key, or sql.ErrNoRows if no
// such row exists.
func getSetting(ctx context.Context, db *sql.DB, key string) (string, error) {
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

// setSetting upserts key/value into the "settings" table.
func setSetting(ctx context.Context, db *sql.DB, key, value string) error {
	const query = `
INSERT INTO settings (key, value) VALUES (?, ?)
ON CONFLICT (key) DO UPDATE SET value = excluded.value`

	if _, err := db.ExecContext(ctx, query, key, value); err != nil {
		return fmt.Errorf("set setting %q: %w", key, err)
	}

	return nil
}
