package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"threev/internal/domain"
)

// newTestFavoriteRepository opens a fresh migrated SQLite database backed
// by a temporary file and returns a FavoriteRepository over it, along with
// the ProfileRepository sharing the same underlying *sql.DB (favorites
// always belong to a profile, so most tests need one to exist first).
func newTestFavoriteRepository(t *testing.T) (*FavoriteRepository, *ProfileRepository) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "favorites_test.db")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB(%q) returned error: %v", dbPath, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close() returned error: %v", err)
		}
	})

	return NewFavoriteRepository(db), NewProfileRepository(db)
}

// createTestProfileForFavorites persists a minimal profile named name,
// suitable as the owning profile of test favorites.
func createTestProfileForFavorites(t *testing.T, repo *ProfileRepository, name string) domain.Profile {
	t.Helper()

	created, err := repo.Create(context.Background(), domain.Profile{
		Name:            name,
		EndpointURL:     "https://s3.example.com",
		Region:          "us-east-1",
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "encrypted-secret",
	})
	if err != nil {
		t.Fatalf("createTestProfileForFavorites(%q): Create() returned error: %v", name, err)
	}

	return created
}

func TestFavoriteRepositoryCreateAndGetByID(t *testing.T) {
	t.Parallel()

	repo, profileRepo := newTestFavoriteRepository(t)
	ctx := context.Background()

	profile := createTestProfileForFavorites(t, profileRepo, "prod")

	created, err := repo.Create(ctx, profile.ID, "my-bucket", "photos/2024")
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	if created.ID == 0 {
		t.Fatal("Create() did not populate ID")
	}
	if created.ProfileID != profile.ID {
		t.Errorf("ProfileID = %d, want %d", created.ProfileID, profile.ID)
	}
	if created.ProfileName != "prod" {
		t.Errorf("ProfileName = %q, want %q", created.ProfileName, "prod")
	}
	if created.Bucket != "my-bucket" {
		t.Errorf("Bucket = %q, want %q", created.Bucket, "my-bucket")
	}
	if created.Prefix != "photos/2024" {
		t.Errorf("Prefix = %q, want %q", created.Prefix, "photos/2024")
	}
	if created.CreatedAt.IsZero() {
		t.Error("Create() did not populate CreatedAt")
	}

	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID(%d) returned error: %v", created.ID, err)
	}
	if got.Bucket != "my-bucket" {
		t.Errorf("GetByID() Bucket = %q, want %q", got.Bucket, "my-bucket")
	}
}

func TestFavoriteRepositoryCreateEmptyPrefixDefaultsToBucketRoot(t *testing.T) {
	t.Parallel()

	repo, profileRepo := newTestFavoriteRepository(t)
	ctx := context.Background()

	profile := createTestProfileForFavorites(t, profileRepo, "prod")

	created, err := repo.Create(ctx, profile.ID, "my-bucket", "")
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	if created.Prefix != "" {
		t.Errorf("Prefix = %q, want empty", created.Prefix)
	}
}

func TestFavoriteRepositoryCreateDuplicateLocation(t *testing.T) {
	t.Parallel()

	repo, profileRepo := newTestFavoriteRepository(t)
	ctx := context.Background()

	profile := createTestProfileForFavorites(t, profileRepo, "prod")

	if _, err := repo.Create(ctx, profile.ID, "my-bucket", "photos"); err != nil {
		t.Fatalf("first Create() returned error: %v", err)
	}

	_, err := repo.Create(ctx, profile.ID, "my-bucket", "photos")
	if !errors.Is(err, domain.ErrDuplicateFavorite) {
		t.Fatalf("second Create() error = %v, want errors.Is(_, domain.ErrDuplicateFavorite)", err)
	}
}

func TestFavoriteRepositoryCreateSameLocationDifferentProfilesAllowed(t *testing.T) {
	t.Parallel()

	repo, profileRepo := newTestFavoriteRepository(t)
	ctx := context.Background()

	profileA := createTestProfileForFavorites(t, profileRepo, "alpha")
	profileB := createTestProfileForFavorites(t, profileRepo, "bravo")

	if _, err := repo.Create(ctx, profileA.ID, "shared-bucket", "docs"); err != nil {
		t.Fatalf("Create(profileA) returned error: %v", err)
	}

	if _, err := repo.Create(ctx, profileB.ID, "shared-bucket", "docs"); err != nil {
		t.Fatalf("Create(profileB) returned error: %v", err)
	}
}

func TestFavoriteRepositoryGetAllOrderedNewestFirstWithProfileName(t *testing.T) {
	t.Parallel()

	repo, profileRepo := newTestFavoriteRepository(t)
	ctx := context.Background()

	profileA := createTestProfileForFavorites(t, profileRepo, "alpha")
	profileB := createTestProfileForFavorites(t, profileRepo, "bravo")

	first, err := repo.Create(ctx, profileA.ID, "bucket-one", "")
	if err != nil {
		t.Fatalf("Create(first) returned error: %v", err)
	}

	second, err := repo.Create(ctx, profileB.ID, "bucket-two", "sub")
	if err != nil {
		t.Fatalf("Create(second) returned error: %v", err)
	}

	all, err := repo.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll() returned error: %v", err)
	}

	if len(all) != 2 {
		t.Fatalf("GetAll() returned %d favorites, want 2", len(all))
	}

	// Newest first: second was created after first.
	if all[0].ID != second.ID {
		t.Errorf("GetAll()[0].ID = %d, want %d (newest first)", all[0].ID, second.ID)
	}
	if all[0].ProfileName != "bravo" {
		t.Errorf("GetAll()[0].ProfileName = %q, want %q", all[0].ProfileName, "bravo")
	}
	if all[1].ID != first.ID {
		t.Errorf("GetAll()[1].ID = %d, want %d", all[1].ID, first.ID)
	}
	if all[1].ProfileName != "alpha" {
		t.Errorf("GetAll()[1].ProfileName = %q, want %q", all[1].ProfileName, "alpha")
	}
}

func TestFavoriteRepositoryGetAllEmpty(t *testing.T) {
	t.Parallel()

	repo, _ := newTestFavoriteRepository(t)
	ctx := context.Background()

	all, err := repo.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll() returned error: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("GetAll() returned %d favorites, want 0", len(all))
	}
}

func TestFavoriteRepositoryGetByIDNotFound(t *testing.T) {
	t.Parallel()

	repo, _ := newTestFavoriteRepository(t)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, 999)
	if !errors.Is(err, domain.ErrFavoriteNotFound) {
		t.Fatalf("GetByID() error = %v, want errors.Is(_, domain.ErrFavoriteNotFound)", err)
	}
}

func TestFavoriteRepositoryDelete(t *testing.T) {
	t.Parallel()

	repo, profileRepo := newTestFavoriteRepository(t)
	ctx := context.Background()

	profile := createTestProfileForFavorites(t, profileRepo, "prod")

	created, err := repo.Create(ctx, profile.ID, "my-bucket", "")
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete() returned error: %v", err)
	}

	_, err = repo.GetByID(ctx, created.ID)
	if !errors.Is(err, domain.ErrFavoriteNotFound) {
		t.Fatalf("GetByID() after Delete() error = %v, want errors.Is(_, domain.ErrFavoriteNotFound)", err)
	}
}

func TestFavoriteRepositoryDeleteNotFound(t *testing.T) {
	t.Parallel()

	repo, _ := newTestFavoriteRepository(t)
	ctx := context.Background()

	err := repo.Delete(ctx, 999)
	if !errors.Is(err, domain.ErrFavoriteNotFound) {
		t.Fatalf("Delete() error = %v, want errors.Is(_, domain.ErrFavoriteNotFound)", err)
	}
}

// TestFavoriteRepositoryDeleteProfileCascadesToFavorites is the key
// ON DELETE CASCADE regression test: deleting a profile that owns
// favorites must delete those favorites too, relying on
// storage.OpenDB's "PRAGMA foreign_keys = ON" (sqlite.go) actually being
// in effect on this test's connection - SQLite does not enforce declared
// foreign keys by default.
func TestFavoriteRepositoryDeleteProfileCascadesToFavorites(t *testing.T) {
	t.Parallel()

	repo, profileRepo := newTestFavoriteRepository(t)
	ctx := context.Background()

	doomed := createTestProfileForFavorites(t, profileRepo, "doomed")
	survivor := createTestProfileForFavorites(t, profileRepo, "survivor")

	if _, err := repo.Create(ctx, doomed.ID, "bucket-a", ""); err != nil {
		t.Fatalf("Create(doomed, bucket-a) returned error: %v", err)
	}
	if _, err := repo.Create(ctx, doomed.ID, "bucket-b", "sub"); err != nil {
		t.Fatalf("Create(doomed, bucket-b) returned error: %v", err)
	}

	survivorFavorite, err := repo.Create(ctx, survivor.ID, "bucket-c", "")
	if err != nil {
		t.Fatalf("Create(survivor) returned error: %v", err)
	}

	if err := profileRepo.Delete(ctx, doomed.ID); err != nil {
		t.Fatalf("profileRepo.Delete(doomed) returned error: %v", err)
	}

	all, err := repo.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll() returned error: %v", err)
	}

	if len(all) != 1 {
		t.Fatalf("GetAll() after deleting doomed profile returned %d favorites, want 1 (only survivor's)", len(all))
	}
	if all[0].ID != survivorFavorite.ID {
		t.Errorf("remaining favorite ID = %d, want %d (survivor's)", all[0].ID, survivorFavorite.ID)
	}
	if all[0].ProfileName != "survivor" {
		t.Errorf("remaining favorite ProfileName = %q, want %q", all[0].ProfileName, "survivor")
	}
}
