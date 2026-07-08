// Package migrations implements a minimal, embed-based, forward-only SQL
// schema migration runner for the threev SQLite database.
//
// Migration files live alongside this file as "NNNN_description.sql" and
// are embedded into the binary at build time. Apply tracks which versions
// have already run in a schema_migrations table and applies any remaining
// ones, in ascending numeric order, each inside its own transaction.
package migrations

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed *.sql
var migrationFiles embed.FS

// migration represents a single parsed, embedded migration file.
type migration struct {
	version int64
	name    string
	sql     string
}

// createMigrationsTableSQL creates the bookkeeping table used to track
// which migrations have already been applied.
const createMigrationsTableSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

// Apply creates the schema_migrations bookkeeping table if necessary, then
// applies every embedded migration that has not yet been recorded as
// applied, in ascending version order. Each migration runs inside its own
// transaction; a failure rolls back that migration and Apply returns
// immediately without attempting any later ones.
//
// Apply is idempotent: calling it again after all migrations have been
// applied is a cheap no-op.
func Apply(db *sql.DB) error {
	if _, err := db.Exec(createMigrationsTableSQL); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	all, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("load embedded migrations: %w", err)
	}

	applied, err := appliedVersions(db)
	if err != nil {
		return fmt.Errorf("read applied migrations: %w", err)
	}

	for _, m := range all {
		if applied[m.version] {
			continue
		}

		if err := applyOne(db, m); err != nil {
			return fmt.Errorf("apply migration %s: %w", m.name, err)
		}
	}

	return nil
}

// loadMigrations reads and parses every embedded "*.sql" file, returning
// them sorted in ascending version order.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations directory: %w", err)
	}

	migs := make([]migration, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		version, err := versionFromName(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("parse migration file name %q: %w", entry.Name(), err)
		}

		contents, err := migrationFiles.ReadFile(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration file %q: %w", entry.Name(), err)
		}

		migs = append(migs, migration{
			version: version,
			name:    entry.Name(),
			sql:     string(contents),
		})
	}

	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })

	return migs, nil
}

// versionFromName extracts the leading numeric version prefix from a
// migration file name such as "0001_init.sql" -> 1.
func versionFromName(name string) (int64, error) {
	prefix, _, found := strings.Cut(name, "_")
	if !found {
		return 0, fmt.Errorf("file name %q missing '_' separator between version and description", name)
	}

	version, err := strconv.ParseInt(prefix, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("file name %q has non-numeric version prefix %q: %w", name, prefix, err)
	}

	return version, nil
}

// appliedVersions returns the set of migration versions already recorded
// in schema_migrations.
func appliedVersions(db *sql.DB) (map[int64]bool, error) {
	rows, err := db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int64]bool)

	for rows.Next() {
		var version int64
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan schema_migrations row: %w", err)
		}

		applied[version] = true
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema_migrations rows: %w", err)
	}

	return applied, nil
}

// applyOne runs a single migration's SQL and records it as applied, all
// within one transaction. On any failure the transaction is rolled back.
func applyOne(db *sql.DB, m migration) (err error) {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				err = errors.Join(err, fmt.Errorf("rollback: %w", rbErr))
			}
		}
	}()

	if _, execErr := tx.Exec(m.sql); execErr != nil {
		err = fmt.Errorf("execute migration sql: %w", execErr)
		return err
	}

	if _, execErr := tx.Exec(
		`INSERT INTO schema_migrations (version) VALUES (?)`,
		m.version,
	); execErr != nil {
		err = fmt.Errorf("record migration version: %w", execErr)
		return err
	}

	if commitErr := tx.Commit(); commitErr != nil {
		err = fmt.Errorf("commit transaction: %w", commitErr)
		return err
	}

	return nil
}
