package storage

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver

	"threev/internal/storage/migrations"
)

// driverName is the database/sql driver name registered by
// modernc.org/sqlite. Unlike the mattn/go-sqlite3 CGO driver (which
// registers "sqlite3"), modernc.org/sqlite registers "sqlite".
const driverName = "sqlite"

// pragmas are applied to every connection opened by OpenDB. They configure
// SQLite for a single-process desktop application:
//   - foreign_keys: enforce declared FOREIGN KEY constraints (off by
//     default in SQLite).
//   - journal_mode=WAL: allows concurrent readers while a write is in
//     progress, and is generally faster for this workload.
//   - busy_timeout: instead of failing immediately with SQLITE_BUSY when
//     the database is locked by another connection/transaction, retry for
//     up to this many milliseconds.
var pragmas = []string{
	"PRAGMA foreign_keys = ON;",
	"PRAGMA journal_mode = WAL;",
	"PRAGMA busy_timeout = 5000;",
}

// OpenDB opens the SQLite database at path (creating the file if it does
// not already exist), applies the required PRAGMA settings, configures a
// connection pool appropriate for SQLite, and applies any pending schema
// migrations.
//
// The returned *sql.DB is safe for concurrent use and should be closed by
// the caller when no longer needed.
func OpenDB(path string) (*sql.DB, error) {
	db, err := sql.Open(driverName, path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database %q: %w", path, err)
	}

	// SQLite allows only one writer at a time, and even in WAL mode
	// concurrent writers still serialize on a single write lock. For a
	// lightly-loaded desktop application there is no benefit to a larger
	// pool, and keeping it small avoids SQLITE_BUSY contention between
	// goroutines racing for a write connection.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	if err := applyPragmas(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	if err := migrations.Apply(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply migrations: %w", err)
	}

	return db, nil
}

// applyPragmas executes every entry in pragmas against db.
func applyPragmas(db *sql.DB) error {
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("apply pragma %q: %w", p, err)
		}
	}

	return nil
}
