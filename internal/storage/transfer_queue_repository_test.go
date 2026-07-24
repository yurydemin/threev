package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"threev/internal/domain"
)

// newTestTransferQueueRepository opens a fresh migrated SQLite database
// backed by a temporary file and returns a TransferQueueRepository over it,
// alongside the raw *sql.DB (needed by callers that also need a
// ProfileRepository to satisfy transfer_queue's profile_id foreign key).
func newTestTransferQueueRepository(t *testing.T) (*TransferQueueRepository, *sql.DB) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "transfer_queue_test.db")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB(%q) returned error: %v", dbPath, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close() returned error: %v", err)
		}
	})

	return NewTransferQueueRepository(db), db
}

// createTestProfile inserts a minimal profile and returns its ID, so tests
// can satisfy transfer_queue.profile_id's foreign key constraint.
func createTestProfile(t *testing.T, db *sql.DB) int64 {
	t.Helper()

	profile, err := NewProfileRepository(db).Create(context.Background(), domain.Profile{
		Name:            "profile-1",
		EndpointURL:     "https://s3.example.com",
		Region:          "us-east-1",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		VerifySSL:       true,
	})
	if err != nil {
		t.Fatalf("create test profile: %v", err)
	}

	return profile.ID
}

func sampleTransferTask(profileID int64) domain.TransferTask {
	return domain.TransferTask{
		ProfileID:       profileID,
		Type:            "upload",
		SourcePath:      "/local/path/file.bin",
		DestinationPath: "objects/file.bin",
		Status:          "pending",
		TotalBytes:      1024,
		Priority:        5,
	}
}

func TestTransferQueueRepositoryCreateAndGetByID(t *testing.T) {
	t.Parallel()

	repo, db := newTestTransferQueueRepository(t)
	ctx := context.Background()
	profileID := createTestProfile(t, db)

	task := sampleTransferTask(profileID)
	task.ErrorMessage = "previous attempt failed"
	task.MultipartUploadID = "upload-id-123"
	task.PartsCompleted = `[1,2,3]`
	task.FileOffset = 42

	created, err := repo.Create(ctx, task)
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	if created.ID == 0 {
		t.Fatal("Create() did not populate ID")
	}
	if created.CreatedAt.IsZero() {
		t.Error("Create() did not populate CreatedAt")
	}
	if created.UpdatedAt.IsZero() {
		t.Error("Create() did not populate UpdatedAt")
	}

	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID(%d) returned error: %v", created.ID, err)
	}

	if got.ProfileID != profileID {
		t.Errorf("ProfileID = %d, want %d", got.ProfileID, profileID)
	}
	if got.Type != "upload" {
		t.Errorf("Type = %q, want %q", got.Type, "upload")
	}
	if got.SourcePath != task.SourcePath {
		t.Errorf("SourcePath = %q, want %q", got.SourcePath, task.SourcePath)
	}
	if got.DestinationPath != task.DestinationPath {
		t.Errorf("DestinationPath = %q, want %q", got.DestinationPath, task.DestinationPath)
	}
	if got.Status != "pending" {
		t.Errorf("Status = %q, want %q", got.Status, "pending")
	}
	if got.TotalBytes != 1024 {
		t.Errorf("TotalBytes = %d, want 1024", got.TotalBytes)
	}
	if got.ErrorMessage != task.ErrorMessage {
		t.Errorf("ErrorMessage = %q, want %q", got.ErrorMessage, task.ErrorMessage)
	}
	if got.MultipartUploadID != task.MultipartUploadID {
		t.Errorf("MultipartUploadID = %q, want %q", got.MultipartUploadID, task.MultipartUploadID)
	}
	if got.PartsCompleted != task.PartsCompleted {
		t.Errorf("PartsCompleted = %q, want %q", got.PartsCompleted, task.PartsCompleted)
	}
	if got.FileOffset != 42 {
		t.Errorf("FileOffset = %d, want 42", got.FileOffset)
	}
	if got.Priority != 5 {
		t.Errorf("Priority = %d, want 5", got.Priority)
	}
}

// TestTransferQueueRepositoryCreateAndGetByIDCrossConnection verifies the
// "copy_cross" task columns added by 0005_transfer_queue_cross_connection.sql
// (dest_profile_id, is_move) round-trip through Create/GetByID exactly like
// every other domain.TransferTask field - dest_profile_id via the same
// nullable-column convention multipart_upload_id already uses (see
// nullableDestProfileID's own doc comment).
func TestTransferQueueRepositoryCreateAndGetByIDCrossConnection(t *testing.T) {
	t.Parallel()

	repo, db := newTestTransferQueueRepository(t)
	ctx := context.Background()
	sourceProfileID := createTestProfile(t, db)

	destProfile, err := NewProfileRepository(db).Create(ctx, domain.Profile{
		Name:            "profile-2",
		EndpointURL:     "https://s3-2.example.com",
		Region:          "us-east-1",
		AccessKeyID:     "access-key-2",
		SecretAccessKey: "secret-key-2",
		VerifySSL:       true,
	})
	if err != nil {
		t.Fatalf("create second test profile: %v", err)
	}

	task := domain.TransferTask{
		ProfileID:       sourceProfileID,
		DestProfileID:   destProfile.ID,
		Type:            "copy_cross",
		SourcePath:      "source-bucket/key.bin",
		DestinationPath: "dest-bucket/key.bin",
		Status:          "pending",
		TotalBytes:      2048,
		IsMove:          true,
	}

	created, err := repo.Create(ctx, task)
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID(%d) returned error: %v", created.ID, err)
	}

	if got.ProfileID != sourceProfileID {
		t.Errorf("ProfileID = %d, want %d", got.ProfileID, sourceProfileID)
	}
	if got.DestProfileID != destProfile.ID {
		t.Errorf("DestProfileID = %d, want %d", got.DestProfileID, destProfile.ID)
	}
	if got.Type != "copy_cross" {
		t.Errorf("Type = %q, want %q", got.Type, "copy_cross")
	}
	if !got.IsMove {
		t.Error("IsMove = false, want true")
	}
}

// TestTransferQueueRepositoryCreateWithoutDestProfileIDReadsBackZero
// verifies that a non-copy_cross task (DestProfileID left at its zero
// value, is_move left at its zero value) reads back exactly that - the
// nullable dest_profile_id column round-trips through NULL, not 0 stored
// literally, but domain.TransferTask.DestProfileID still reads back as the
// same 0 either way (see nullableDestProfileID's own doc comment for why 0
// is a safe "unset" sentinel: it is never a valid profiles.id).
func TestTransferQueueRepositoryCreateWithoutDestProfileIDReadsBackZero(t *testing.T) {
	t.Parallel()

	repo, db := newTestTransferQueueRepository(t)
	ctx := context.Background()
	profileID := createTestProfile(t, db)

	created, err := repo.Create(ctx, sampleTransferTask(profileID))
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID(%d) returned error: %v", created.ID, err)
	}

	if got.DestProfileID != 0 {
		t.Errorf("DestProfileID = %d, want 0", got.DestProfileID)
	}
	if got.IsMove {
		t.Error("IsMove = true, want false")
	}
}

func TestTransferQueueRepositoryCreateWithoutOptionalFields(t *testing.T) {
	t.Parallel()

	repo, db := newTestTransferQueueRepository(t)
	ctx := context.Background()
	profileID := createTestProfile(t, db)

	created, err := repo.Create(ctx, sampleTransferTask(profileID))
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID(%d) returned error: %v", created.ID, err)
	}

	if got.ErrorMessage != "" {
		t.Errorf("ErrorMessage = %q, want empty", got.ErrorMessage)
	}
	if got.MultipartUploadID != "" {
		t.Errorf("MultipartUploadID = %q, want empty", got.MultipartUploadID)
	}
	if got.PartsCompleted != "" {
		t.Errorf("PartsCompleted = %q, want empty", got.PartsCompleted)
	}
}

func TestTransferQueueRepositoryGetByIDNotFound(t *testing.T) {
	t.Parallel()

	repo, _ := newTestTransferQueueRepository(t)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, 999)
	if !errors.Is(err, domain.ErrTransferTaskNotFound) {
		t.Fatalf("GetByID() error = %v, want errors.Is(_, domain.ErrTransferTaskNotFound)", err)
	}
}

func TestTransferQueueRepositoryGetAllOrderedByPriorityThenCreatedAt(t *testing.T) {
	t.Parallel()

	repo, db := newTestTransferQueueRepository(t)
	ctx := context.Background()
	profileID := createTestProfile(t, db)

	// Two tasks share priority 1 (creation order should break the tie); one
	// task has a lower (higher-precedence) priority and should sort first
	// despite being created last.
	first := sampleTransferTask(profileID)
	first.Priority = 1
	first.SourcePath = "first.bin"
	if _, err := repo.Create(ctx, first); err != nil {
		t.Fatalf("Create(first) returned error: %v", err)
	}

	second := sampleTransferTask(profileID)
	second.Priority = 1
	second.SourcePath = "second.bin"
	if _, err := repo.Create(ctx, second); err != nil {
		t.Fatalf("Create(second) returned error: %v", err)
	}

	highPriority := sampleTransferTask(profileID)
	highPriority.Priority = 0
	highPriority.SourcePath = "high-priority.bin"
	if _, err := repo.Create(ctx, highPriority); err != nil {
		t.Fatalf("Create(highPriority) returned error: %v", err)
	}

	all, err := repo.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll() returned error: %v", err)
	}

	if len(all) != 3 {
		t.Fatalf("GetAll() returned %d tasks, want 3", len(all))
	}

	wantOrder := []string{"high-priority.bin", "first.bin", "second.bin"}
	for i, want := range wantOrder {
		if all[i].SourcePath != want {
			t.Errorf("GetAll()[%d].SourcePath = %q, want %q", i, all[i].SourcePath, want)
		}
	}
}

func TestTransferQueueRepositoryGetAllEmpty(t *testing.T) {
	t.Parallel()

	repo, _ := newTestTransferQueueRepository(t)
	ctx := context.Background()

	all, err := repo.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll() returned error: %v", err)
	}

	if len(all) != 0 {
		t.Fatalf("GetAll() returned %d tasks, want 0", len(all))
	}
}

func TestTransferQueueRepositoryUpdateStatus(t *testing.T) {
	t.Parallel()

	repo, db := newTestTransferQueueRepository(t)
	ctx := context.Background()
	profileID := createTestProfile(t, db)

	created, err := repo.Create(ctx, sampleTransferTask(profileID))
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	if err := repo.UpdateStatus(ctx, created.ID, "failed", "network timeout"); err != nil {
		t.Fatalf("UpdateStatus() returned error: %v", err)
	}

	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID() returned error: %v", err)
	}

	if got.Status != "failed" {
		t.Errorf("Status = %q, want %q", got.Status, "failed")
	}
	if got.ErrorMessage != "network timeout" {
		t.Errorf("ErrorMessage = %q, want %q", got.ErrorMessage, "network timeout")
	}
	if !got.UpdatedAt.After(created.UpdatedAt) && !got.UpdatedAt.Equal(created.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want >= %v", got.UpdatedAt, created.UpdatedAt)
	}
}

func TestTransferQueueRepositoryUpdateStatusNotFound(t *testing.T) {
	t.Parallel()

	repo, _ := newTestTransferQueueRepository(t)
	ctx := context.Background()

	err := repo.UpdateStatus(ctx, 999, "failed", "boom")
	if !errors.Is(err, domain.ErrTransferTaskNotFound) {
		t.Fatalf("UpdateStatus() error = %v, want errors.Is(_, domain.ErrTransferTaskNotFound)", err)
	}
}

func TestTransferQueueRepositoryUpdateProgress(t *testing.T) {
	t.Parallel()

	repo, db := newTestTransferQueueRepository(t)
	ctx := context.Background()
	profileID := createTestProfile(t, db)

	created, err := repo.Create(ctx, sampleTransferTask(profileID))
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	if err := repo.UpdateProgress(ctx, created.ID, 512, 2048); err != nil {
		t.Fatalf("UpdateProgress() returned error: %v", err)
	}

	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID() returned error: %v", err)
	}

	if got.TransferredBytes != 512 {
		t.Errorf("TransferredBytes = %d, want 512", got.TransferredBytes)
	}
	if got.TotalBytes != 2048 {
		t.Errorf("TotalBytes = %d, want 2048", got.TotalBytes)
	}
}

func TestTransferQueueRepositoryUpdateProgressNotFound(t *testing.T) {
	t.Parallel()

	repo, _ := newTestTransferQueueRepository(t)
	ctx := context.Background()

	err := repo.UpdateProgress(ctx, 999, 1, 2)
	if !errors.Is(err, domain.ErrTransferTaskNotFound) {
		t.Fatalf("UpdateProgress() error = %v, want errors.Is(_, domain.ErrTransferTaskNotFound)", err)
	}
}

func TestTransferQueueRepositoryUpdateMultipartUploadID(t *testing.T) {
	t.Parallel()

	repo, db := newTestTransferQueueRepository(t)
	ctx := context.Background()
	profileID := createTestProfile(t, db)

	created, err := repo.Create(ctx, sampleTransferTask(profileID))
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	if err := repo.UpdateMultipartUploadID(ctx, created.ID, "new-upload-id"); err != nil {
		t.Fatalf("UpdateMultipartUploadID() returned error: %v", err)
	}

	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID() returned error: %v", err)
	}

	if got.MultipartUploadID != "new-upload-id" {
		t.Errorf("MultipartUploadID = %q, want %q", got.MultipartUploadID, "new-upload-id")
	}
}

func TestTransferQueueRepositoryUpdateMultipartUploadIDNotFound(t *testing.T) {
	t.Parallel()

	repo, _ := newTestTransferQueueRepository(t)
	ctx := context.Background()

	err := repo.UpdateMultipartUploadID(ctx, 999, "id")
	if !errors.Is(err, domain.ErrTransferTaskNotFound) {
		t.Fatalf("UpdateMultipartUploadID() error = %v, want errors.Is(_, domain.ErrTransferTaskNotFound)", err)
	}
}

func TestTransferQueueRepositoryUpdatePriority(t *testing.T) {
	t.Parallel()

	repo, db := newTestTransferQueueRepository(t)
	ctx := context.Background()
	profileID := createTestProfile(t, db)

	created, err := repo.Create(ctx, sampleTransferTask(profileID))
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	if err := repo.UpdatePriority(ctx, created.ID, 42); err != nil {
		t.Fatalf("UpdatePriority() returned error: %v", err)
	}

	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID() returned error: %v", err)
	}

	if got.Priority != 42 {
		t.Errorf("Priority = %d, want 42", got.Priority)
	}
}

func TestTransferQueueRepositoryUpdatePriorityNotFound(t *testing.T) {
	t.Parallel()

	repo, _ := newTestTransferQueueRepository(t)
	ctx := context.Background()

	err := repo.UpdatePriority(ctx, 999, 1)
	if !errors.Is(err, domain.ErrTransferTaskNotFound) {
		t.Fatalf("UpdatePriority() error = %v, want errors.Is(_, domain.ErrTransferTaskNotFound)", err)
	}
}

func TestTransferQueueRepositoryDelete(t *testing.T) {
	t.Parallel()

	repo, db := newTestTransferQueueRepository(t)
	ctx := context.Background()
	profileID := createTestProfile(t, db)

	created, err := repo.Create(ctx, sampleTransferTask(profileID))
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete() returned error: %v", err)
	}

	_, err = repo.GetByID(ctx, created.ID)
	if !errors.Is(err, domain.ErrTransferTaskNotFound) {
		t.Fatalf("GetByID() after Delete() error = %v, want errors.Is(_, domain.ErrTransferTaskNotFound)", err)
	}
}

func TestTransferQueueRepositoryDeleteNotFound(t *testing.T) {
	t.Parallel()

	repo, _ := newTestTransferQueueRepository(t)
	ctx := context.Background()

	err := repo.Delete(ctx, 999)
	if !errors.Is(err, domain.ErrTransferTaskNotFound) {
		t.Fatalf("Delete() error = %v, want errors.Is(_, domain.ErrTransferTaskNotFound)", err)
	}
}

func TestTransferQueueRepositoryMoveToHistory(t *testing.T) {
	t.Parallel()

	repo, db := newTestTransferQueueRepository(t)
	historyRepo := NewTransferHistoryRepository(db)
	ctx := context.Background()
	profileID := createTestProfile(t, db)

	task := sampleTransferTask(profileID)
	task.Status = "completed"
	task.TotalBytes = 2048
	task.TransferredBytes = 2048

	created, err := repo.Create(ctx, task)
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	completedAt := time.Now().UTC().Truncate(time.Second)
	historyEntry := domain.TransferHistoryEntry{
		QueueID:         created.ID,
		ProfileID:       profileID,
		Type:            created.Type,
		SourcePath:      created.SourcePath,
		DestinationPath: created.DestinationPath,
		TotalBytes:      created.TotalBytes,
		Status:          "completed",
		CompletedAt:     completedAt,
	}

	if err := repo.MoveToHistory(ctx, created.ID, historyEntry); err != nil {
		t.Fatalf("MoveToHistory() returned error: %v", err)
	}

	// No longer in the queue.
	if _, err := repo.GetByID(ctx, created.ID); !errors.Is(err, domain.ErrTransferTaskNotFound) {
		t.Fatalf("GetByID() after MoveToHistory() error = %v, want errors.Is(_, domain.ErrTransferTaskNotFound)", err)
	}

	// Present in history.
	entries, err := historyRepo.GetAll(ctx, 10)
	if err != nil {
		t.Fatalf("historyRepo.GetAll() returned error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("historyRepo.GetAll() returned %d entries, want 1", len(entries))
	}

	entry := entries[0]
	if entry.QueueID != created.ID {
		t.Errorf("QueueID = %d, want %d", entry.QueueID, created.ID)
	}
	if entry.ProfileID != profileID {
		t.Errorf("ProfileID = %d, want %d", entry.ProfileID, profileID)
	}
	if entry.Type != created.Type {
		t.Errorf("Type = %q, want %q", entry.Type, created.Type)
	}
	if entry.SourcePath != created.SourcePath {
		t.Errorf("SourcePath = %q, want %q", entry.SourcePath, created.SourcePath)
	}
	if entry.DestinationPath != created.DestinationPath {
		t.Errorf("DestinationPath = %q, want %q", entry.DestinationPath, created.DestinationPath)
	}
	if entry.TotalBytes != 2048 {
		t.Errorf("TotalBytes = %d, want 2048", entry.TotalBytes)
	}
	if entry.Status != "completed" {
		t.Errorf("Status = %q, want %q", entry.Status, "completed")
	}
	if !entry.CompletedAt.Equal(completedAt) {
		t.Errorf("CompletedAt = %v, want %v", entry.CompletedAt, completedAt)
	}
}

func TestTransferQueueRepositoryMoveToHistoryNotFound(t *testing.T) {
	t.Parallel()

	repo, db := newTestTransferQueueRepository(t)
	historyRepo := NewTransferHistoryRepository(db)
	ctx := context.Background()
	profileID := createTestProfile(t, db)

	err := repo.MoveToHistory(ctx, 999, domain.TransferHistoryEntry{
		ProfileID:   profileID,
		Type:        "upload",
		Status:      "completed",
		CompletedAt: time.Now(),
	})
	if !errors.Is(err, domain.ErrTransferTaskNotFound) {
		t.Fatalf("MoveToHistory() error = %v, want errors.Is(_, domain.ErrTransferTaskNotFound)", err)
	}

	// Verify transactional rollback: the history insert must not have been
	// committed even though it happens before the (failing) delete.
	entries, err := historyRepo.GetAll(ctx, 10)
	if err != nil {
		t.Fatalf("historyRepo.GetAll() returned error: %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("historyRepo.GetAll() returned %d entries after failed MoveToHistory(), want 0 (rollback expected)", len(entries))
	}
}
