package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

// newTestDB opens a fresh migrated SQLite database backed by a temporary
// file.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "settings_test.db")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB(%q) returned error: %v", dbPath, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close() returned error: %v", err)
		}
	})

	return db
}

func TestGetOrCreateSetting_GeneratesOnFirstCall(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	ctx := context.Background()

	calls := 0
	generate := func() (string, error) {
		calls++
		return "generated-value", nil
	}

	got, err := GetOrCreateSetting(ctx, db, "my-key", generate)
	if err != nil {
		t.Fatalf("GetOrCreateSetting() returned error: %v", err)
	}
	if got != "generated-value" {
		t.Errorf("GetOrCreateSetting() = %q, want %q", got, "generated-value")
	}
	if calls != 1 {
		t.Errorf("generate called %d times, want 1", calls)
	}
}

func TestGetOrCreateSetting_ReusesPersistedValue(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	ctx := context.Background()

	first, err := GetOrCreateSetting(ctx, db, "my-key", func() (string, error) {
		return "generated-value", nil
	})
	if err != nil {
		t.Fatalf("first GetOrCreateSetting() returned error: %v", err)
	}

	calls := 0
	second, err := GetOrCreateSetting(ctx, db, "my-key", func() (string, error) {
		calls++
		return "should-not-be-used", nil
	})
	if err != nil {
		t.Fatalf("second GetOrCreateSetting() returned error: %v", err)
	}

	if second != first {
		t.Errorf("second GetOrCreateSetting() = %q, want %q (persisted value)", second, first)
	}
	if calls != 0 {
		t.Errorf("generate called %d times on second call, want 0", calls)
	}
}

func TestGetOrCreateSetting_GenerateError(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	ctx := context.Background()

	wantErr := errors.New("boom")

	_, err := GetOrCreateSetting(ctx, db, "my-key", func() (string, error) {
		return "", wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("GetOrCreateSetting() error = %v, want errors.Is(_, wantErr)", err)
	}

	// Confirm nothing was persisted: a later call still invokes generate.
	calls := 0
	_, err = GetOrCreateSetting(ctx, db, "my-key", func() (string, error) {
		calls++
		return "generated-value", nil
	})
	if err != nil {
		t.Fatalf("GetOrCreateSetting() after failed generate returned error: %v", err)
	}
	if calls != 1 {
		t.Errorf("generate called %d times, want 1", calls)
	}
}

func TestGetOrCreateSetting_DifferentKeysIndependent(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	ctx := context.Background()

	a, err := GetOrCreateSetting(ctx, db, "key-a", func() (string, error) { return "value-a", nil })
	if err != nil {
		t.Fatalf("GetOrCreateSetting(key-a) returned error: %v", err)
	}

	b, err := GetOrCreateSetting(ctx, db, "key-b", func() (string, error) { return "value-b", nil })
	if err != nil {
		t.Fatalf("GetOrCreateSetting(key-b) returned error: %v", err)
	}

	if a != "value-a" || b != "value-b" {
		t.Errorf("got a=%q b=%q, want a=%q b=%q", a, b, "value-a", "value-b")
	}
}
