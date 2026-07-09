package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"threev/internal/domain"
)

// newTestTransferHistoryRepository opens a fresh migrated SQLite database
// backed by a temporary file and returns a TransferHistoryRepository over
// it.
func newTestTransferHistoryRepository(t *testing.T) *TransferHistoryRepository {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "transfer_history_test.db")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB(%q) returned error: %v", dbPath, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close() returned error: %v", err)
		}
	})

	return NewTransferHistoryRepository(db)
}

func sampleTransferHistoryEntry(profileID int64, completedAt time.Time) domain.TransferHistoryEntry {
	return domain.TransferHistoryEntry{
		ProfileID:       profileID,
		Type:            "download",
		SourcePath:      "objects/report.pdf",
		DestinationPath: "/local/downloads/report.pdf",
		TotalBytes:      4096,
		Status:          "completed",
		CompletedAt:     completedAt,
	}
}

func TestTransferHistoryRepositoryCreate(t *testing.T) {
	t.Parallel()

	repo := newTestTransferHistoryRepository(t)
	ctx := context.Background()

	completedAt := time.Now().UTC().Truncate(time.Second)
	entry := sampleTransferHistoryEntry(7, completedAt)
	entry.QueueID = 99
	entry.ErrorMessage = "partial failure, retried"

	created, err := repo.Create(ctx, entry)
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	if created.ID == 0 {
		t.Fatal("Create() did not populate ID")
	}

	all, err := repo.GetAll(ctx, 10)
	if err != nil {
		t.Fatalf("GetAll() returned error: %v", err)
	}

	if len(all) != 1 {
		t.Fatalf("GetAll() returned %d entries, want 1", len(all))
	}

	got := all[0]
	if got.ID != created.ID {
		t.Errorf("ID = %d, want %d", got.ID, created.ID)
	}
	if got.QueueID != 99 {
		t.Errorf("QueueID = %d, want 99", got.QueueID)
	}
	if got.ProfileID != 7 {
		t.Errorf("ProfileID = %d, want 7", got.ProfileID)
	}
	if got.Type != "download" {
		t.Errorf("Type = %q, want %q", got.Type, "download")
	}
	if got.SourcePath != entry.SourcePath {
		t.Errorf("SourcePath = %q, want %q", got.SourcePath, entry.SourcePath)
	}
	if got.DestinationPath != entry.DestinationPath {
		t.Errorf("DestinationPath = %q, want %q", got.DestinationPath, entry.DestinationPath)
	}
	if got.TotalBytes != 4096 {
		t.Errorf("TotalBytes = %d, want 4096", got.TotalBytes)
	}
	if got.Status != "completed" {
		t.Errorf("Status = %q, want %q", got.Status, "completed")
	}
	if !got.CompletedAt.Equal(completedAt) {
		t.Errorf("CompletedAt = %v, want %v", got.CompletedAt, completedAt)
	}
	if got.ErrorMessage != "partial failure, retried" {
		t.Errorf("ErrorMessage = %q, want %q", got.ErrorMessage, "partial failure, retried")
	}
}

func TestTransferHistoryRepositoryCreateWithoutOptionalFields(t *testing.T) {
	t.Parallel()

	repo := newTestTransferHistoryRepository(t)
	ctx := context.Background()

	entry := sampleTransferHistoryEntry(1, time.Now())

	created, err := repo.Create(ctx, entry)
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	all, err := repo.GetAll(ctx, 10)
	if err != nil {
		t.Fatalf("GetAll() returned error: %v", err)
	}

	if len(all) != 1 || all[0].ID != created.ID {
		t.Fatalf("GetAll() = %#v, want single entry with ID %d", all, created.ID)
	}

	if all[0].ErrorMessage != "" {
		t.Errorf("ErrorMessage = %q, want empty", all[0].ErrorMessage)
	}
	if all[0].QueueID != 0 {
		t.Errorf("QueueID = %d, want 0", all[0].QueueID)
	}
}

func TestTransferHistoryRepositoryGetAllOrderedByCompletedAtDesc(t *testing.T) {
	t.Parallel()

	repo := newTestTransferHistoryRepository(t)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Second)

	oldest := sampleTransferHistoryEntry(1, base.Add(-2*time.Hour))
	oldest.SourcePath = "oldest"
	if _, err := repo.Create(ctx, oldest); err != nil {
		t.Fatalf("Create(oldest) returned error: %v", err)
	}

	newest := sampleTransferHistoryEntry(1, base)
	newest.SourcePath = "newest"
	if _, err := repo.Create(ctx, newest); err != nil {
		t.Fatalf("Create(newest) returned error: %v", err)
	}

	middle := sampleTransferHistoryEntry(1, base.Add(-1*time.Hour))
	middle.SourcePath = "middle"
	if _, err := repo.Create(ctx, middle); err != nil {
		t.Fatalf("Create(middle) returned error: %v", err)
	}

	all, err := repo.GetAll(ctx, 10)
	if err != nil {
		t.Fatalf("GetAll() returned error: %v", err)
	}

	if len(all) != 3 {
		t.Fatalf("GetAll() returned %d entries, want 3", len(all))
	}

	wantOrder := []string{"newest", "middle", "oldest"}
	for i, want := range wantOrder {
		if all[i].SourcePath != want {
			t.Errorf("GetAll()[%d].SourcePath = %q, want %q", i, all[i].SourcePath, want)
		}
	}
}

func TestTransferHistoryRepositoryGetAllRespectsLimit(t *testing.T) {
	t.Parallel()

	repo := newTestTransferHistoryRepository(t)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Second)

	for i := 0; i < 5; i++ {
		entry := sampleTransferHistoryEntry(1, base.Add(time.Duration(i)*time.Minute))
		if _, err := repo.Create(ctx, entry); err != nil {
			t.Fatalf("Create() returned error: %v", err)
		}
	}

	all, err := repo.GetAll(ctx, 2)
	if err != nil {
		t.Fatalf("GetAll() returned error: %v", err)
	}

	if len(all) != 2 {
		t.Fatalf("GetAll(limit=2) returned %d entries, want 2", len(all))
	}
}

func TestTransferHistoryRepositoryGetAllEmpty(t *testing.T) {
	t.Parallel()

	repo := newTestTransferHistoryRepository(t)
	ctx := context.Background()

	all, err := repo.GetAll(ctx, 10)
	if err != nil {
		t.Fatalf("GetAll() returned error: %v", err)
	}

	if len(all) != 0 {
		t.Fatalf("GetAll() returned %d entries, want 0", len(all))
	}
}

func TestTransferHistoryRepositoryClear(t *testing.T) {
	t.Parallel()

	repo := newTestTransferHistoryRepository(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := repo.Create(ctx, sampleTransferHistoryEntry(1, time.Now())); err != nil {
			t.Fatalf("Create() returned error: %v", err)
		}
	}

	if err := repo.Clear(ctx); err != nil {
		t.Fatalf("Clear() returned error: %v", err)
	}

	all, err := repo.GetAll(ctx, 10)
	if err != nil {
		t.Fatalf("GetAll() returned error: %v", err)
	}

	if len(all) != 0 {
		t.Fatalf("GetAll() after Clear() returned %d entries, want 0", len(all))
	}
}

func TestTransferHistoryRepositoryClearEmpty(t *testing.T) {
	t.Parallel()

	repo := newTestTransferHistoryRepository(t)
	ctx := context.Background()

	if err := repo.Clear(ctx); err != nil {
		t.Fatalf("Clear() on empty history returned error: %v", err)
	}
}
