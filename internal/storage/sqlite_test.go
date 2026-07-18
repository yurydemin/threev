package storage

import (
	"path/filepath"
	"testing"
)

func TestOpenDB_AppliesPragmas(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB(%q) returned error: %v", dbPath, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close() returned error: %v", err)
		}
	})

	var foreignKeys int
	if err := db.QueryRow("PRAGMA foreign_keys;").Scan(&foreignKeys); err != nil {
		t.Fatalf("query foreign_keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("foreign_keys = %d, want 1", foreignKeys)
	}

	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode;").Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode pragma: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("journal_mode = %q, want %q", journalMode, "wal")
	}

	var busyTimeout int
	if err := db.QueryRow("PRAGMA busy_timeout;").Scan(&busyTimeout); err != nil {
		t.Fatalf("query busy_timeout pragma: %v", err)
	}
	if busyTimeout != 5000 {
		t.Errorf("busy_timeout = %d, want 5000", busyTimeout)
	}
}

func TestOpenDB_RunsMigrations(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB(%q) returned error: %v", dbPath, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close() returned error: %v", err)
		}
	})

	for _, table := range []string{"profiles", "transfer_queue", "transfer_history", "settings", "schema_migrations"} {
		var name string
		err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`,
			table,
		).Scan(&name)
		if err != nil {
			t.Errorf("expected table %q to exist: %v", table, err)
		}
	}
}

func TestOpenDB_IdempotentReopen(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")

	db1, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("first OpenDB(%q) returned error: %v", dbPath, err)
	}
	if err := db1.Close(); err != nil {
		t.Fatalf("close first db handle: %v", err)
	}

	db2, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("second OpenDB(%q) returned error: %v", dbPath, err)
	}
	t.Cleanup(func() {
		if err := db2.Close(); err != nil {
			t.Errorf("db.Close() returned error: %v", err)
		}
	})

	// Reopening must not re-run (or double-record) any migration: exactly
	// one schema_migrations row per embedded "migrations/*.sql" file
	// (currently 0001_init.sql, 0002_favorites.sql,
	// 0003_transfer_queue_zip_type.sql, and 0004_profiles_proxy_url.sql),
	// not one per OpenDB call.
	const wantMigrationCount = 4

	var count int
	if err := db2.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count schema_migrations rows: %v", err)
	}
	if count != wantMigrationCount {
		t.Errorf("schema_migrations row count = %d, want %d", count, wantMigrationCount)
	}
}
