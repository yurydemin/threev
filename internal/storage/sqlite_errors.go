package storage

import (
	"errors"
	"strings"

	sqlitelib "modernc.org/sqlite"
)

// sqliteConstraintUnique is the SQLite extended result code
// SQLITE_CONSTRAINT_UNIQUE (https://www.sqlite.org/rescode.html#constraint_unique).
const sqliteConstraintUnique = 2067

// isUniqueConstraintError reports whether err was caused by a SQLite UNIQUE
// constraint violation (e.g. a duplicate profiles.name).
func isUniqueConstraintError(err error) bool {
	var sqliteErr *sqlitelib.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.Code() == sqliteConstraintUnique
	}

	// Fall back to substring matching in case the error was wrapped in a
	// way errors.As cannot unwrap (defensive; the modernc.org/sqlite driver
	// currently always returns *sqlite.Error directly).
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
