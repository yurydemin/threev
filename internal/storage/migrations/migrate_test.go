package migrations

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// openTestDB opens a fresh, migration-free SQLite database backed by a
// temporary file, for use as the starting point of migration tests.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "migrations_test.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite database %q: %v", dbPath, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close() returned error: %v", err)
		}
	})

	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		t.Fatalf("enable foreign_keys: %v", err)
	}

	return db
}

func TestApply_CreatesAllTablesAndIndexes(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)

	if err := Apply(db); err != nil {
		t.Fatalf("Apply() returned error: %v", err)
	}

	wantTables := []string{
		"profiles",
		"transfer_queue",
		"transfer_history",
		"settings",
		"schema_migrations",
	}
	for _, table := range wantTables {
		var name string
		err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`,
			table,
		).Scan(&name)
		if err != nil {
			t.Errorf("expected table %q to exist: %v", table, err)
		}
	}

	wantIndexes := []string{
		"idx_transfer_queue_status",
		"idx_transfer_queue_profile_id",
		"idx_transfer_history_profile_id",
	}
	for _, index := range wantIndexes {
		var name string
		err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?`,
			index,
		).Scan(&name)
		if err != nil {
			t.Errorf("expected index %q to exist: %v", index, err)
		}
	}
}

func TestApply_RecordsMigrationVersion(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)

	if err := Apply(db); err != nil {
		t.Fatalf("Apply() returned error: %v", err)
	}

	var version int64
	if err := db.QueryRow(`SELECT version FROM schema_migrations WHERE version = 1`).Scan(&version); err != nil {
		t.Fatalf("expected version 1 to be recorded: %v", err)
	}
	if version != 1 {
		t.Errorf("version = %d, want 1", version)
	}
}

func TestApply_IsIdempotent(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)

	if err := Apply(db); err != nil {
		t.Fatalf("first Apply() returned error: %v", err)
	}

	// A second call must not error (e.g. by trying to re-run "CREATE
	// TABLE" without "IF NOT EXISTS" and failing).
	if err := Apply(db); err != nil {
		t.Fatalf("second Apply() returned error: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count schema_migrations rows: %v", err)
	}
	if count != len(mustLoadMigrations(t)) {
		t.Errorf("schema_migrations row count after two Apply() calls = %d, want %d (one row per embedded migration, not doubled)", count, len(mustLoadMigrations(t)))
	}
}

// mustLoadMigrations returns every embedded migration, for tests that need
// to assert against the current total migration count without hardcoding
// it (and so silently going stale as new migration files are added).
func mustLoadMigrations(t *testing.T) []migration {
	t.Helper()

	migs, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations() returned error: %v", err)
	}

	return migs
}

func TestVersionFromName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		want    int64
		wantErr bool
	}{
		{name: "0001_init.sql", want: 1},
		{name: "0042_add_widgets.sql", want: 42},
		{name: "no_version_prefix.sql", wantErr: true},
		{name: "abc_bad_prefix.sql", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := versionFromName(tt.name)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("versionFromName(%q) returned nil error, want error", tt.name)
				}
				return
			}
			if err != nil {
				t.Fatalf("versionFromName(%q) returned error: %v", tt.name, err)
			}
			if got != tt.want {
				t.Errorf("versionFromName(%q) = %d, want %d", tt.name, got, tt.want)
			}
		})
	}
}
