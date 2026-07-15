package favorites

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"threev/internal/domain"
	"threev/internal/storage"
)

// newTestFavoritesService opens a fresh migrated SQLite database backed by
// a temporary file and returns a FavoritesService over it, along with the
// ProfileRepository sharing the same underlying *sql.DB (needed to create
// an owning profile before a favorite can be added).
func newTestFavoritesService(t *testing.T) (*FavoritesService, *storage.ProfileRepository) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "favorites_service_test.db")

	db, err := storage.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB(%q) returned error: %v", dbPath, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close() returned error: %v", err)
		}
	})

	return NewFavoritesService(storage.NewFavoriteRepository(db)), storage.NewProfileRepository(db)
}

func createTestProfile(t *testing.T, repo *storage.ProfileRepository) domain.Profile {
	t.Helper()

	created, err := repo.Create(context.Background(), domain.Profile{
		Name:            "prod",
		EndpointURL:     "https://s3.example.com",
		Region:          "us-east-1",
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "encrypted-secret",
	})
	if err != nil {
		t.Fatalf("createTestProfile(): Create() returned error: %v", err)
	}

	return created
}

func TestFavoritesServiceAddFavorite(t *testing.T) {
	t.Parallel()

	svc, profileRepo := newTestFavoritesService(t)
	profile := createTestProfile(t, profileRepo)

	favorite, err := svc.AddFavorite(profile.ID, "my-bucket", "photos")
	if err != nil {
		t.Fatalf("AddFavorite() returned error: %v", err)
	}

	if favorite.ID == 0 {
		t.Fatal("AddFavorite() did not populate ID")
	}
	if favorite.ProfileID != profile.ID {
		t.Errorf("ProfileID = %d, want %d", favorite.ProfileID, profile.ID)
	}
	if favorite.ProfileName != "prod" {
		t.Errorf("ProfileName = %q, want %q", favorite.ProfileName, "prod")
	}
	if favorite.Bucket != "my-bucket" {
		t.Errorf("Bucket = %q, want %q", favorite.Bucket, "my-bucket")
	}
	if favorite.Prefix != "photos" {
		t.Errorf("Prefix = %q, want %q", favorite.Prefix, "photos")
	}
}

func TestFavoritesServiceAddFavoriteDuplicate(t *testing.T) {
	t.Parallel()

	svc, profileRepo := newTestFavoritesService(t)
	profile := createTestProfile(t, profileRepo)

	if _, err := svc.AddFavorite(profile.ID, "my-bucket", "photos"); err != nil {
		t.Fatalf("first AddFavorite() returned error: %v", err)
	}

	_, err := svc.AddFavorite(profile.ID, "my-bucket", "photos")
	if !errors.Is(err, domain.ErrDuplicateFavorite) {
		t.Fatalf("second AddFavorite() error = %v, want errors.Is(_, domain.ErrDuplicateFavorite)", err)
	}
}

func TestFavoritesServiceGetFavorites(t *testing.T) {
	t.Parallel()

	svc, profileRepo := newTestFavoritesService(t)
	profile := createTestProfile(t, profileRepo)

	if _, err := svc.AddFavorite(profile.ID, "bucket-a", ""); err != nil {
		t.Fatalf("AddFavorite() returned error: %v", err)
	}
	if _, err := svc.AddFavorite(profile.ID, "bucket-b", "sub"); err != nil {
		t.Fatalf("AddFavorite() returned error: %v", err)
	}

	all, err := svc.GetFavorites()
	if err != nil {
		t.Fatalf("GetFavorites() returned error: %v", err)
	}

	if len(all) != 2 {
		t.Fatalf("GetFavorites() returned %d favorites, want 2", len(all))
	}
}

func TestFavoritesServiceRemoveFavorite(t *testing.T) {
	t.Parallel()

	svc, profileRepo := newTestFavoritesService(t)
	profile := createTestProfile(t, profileRepo)

	favorite, err := svc.AddFavorite(profile.ID, "my-bucket", "")
	if err != nil {
		t.Fatalf("AddFavorite() returned error: %v", err)
	}

	if err := svc.RemoveFavorite(favorite.ID); err != nil {
		t.Fatalf("RemoveFavorite() returned error: %v", err)
	}

	all, err := svc.GetFavorites()
	if err != nil {
		t.Fatalf("GetFavorites() returned error: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("GetFavorites() after RemoveFavorite() returned %d favorites, want 0", len(all))
	}
}

func TestFavoritesServiceRemoveFavoriteNotFound(t *testing.T) {
	t.Parallel()

	svc, _ := newTestFavoritesService(t)

	err := svc.RemoveFavorite(999)
	if !errors.Is(err, domain.ErrFavoriteNotFound) {
		t.Fatalf("RemoveFavorite() error = %v, want errors.Is(_, domain.ErrFavoriteNotFound)", err)
	}
}
