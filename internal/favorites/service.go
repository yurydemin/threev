// Package favorites implements the Wails-bound API for bookmarking
// bucket/prefix locations within a connection profile: add a favorite,
// remove one, and list every favorite across every profile for the
// Sidebar's global, profile-grouped favorites list. FavoritesService is
// purely local SQLite bookkeeping - it never touches encrypted credential
// fields, never derives or reads the encryption key, and never makes an S3
// call.
package favorites

import (
	"context"
	"fmt"

	"threev/internal/domain"
	"threev/internal/storage"
)

// FavoritesService implements the favorites CRUD surface bound directly to
// the frontend (see main.go's options.App.Bind). It thinly delegates every
// method to storage.FavoriteRepository, mirroring connection.
// ConnectionService's own simplicity - there is no encryption or S3 client
// involved anywhere in this package.
type FavoritesService struct {
	repo *storage.FavoriteRepository
}

// NewFavoritesService returns a FavoritesService backed by repo.
func NewFavoritesService(repo *storage.FavoriteRepository) *FavoritesService {
	return &FavoritesService{repo: repo}
}

// AddFavorite bookmarks (bucket, prefix) within the profile identified by
// profileID, returning the created domain.Favorite (with ProfileName
// populated). It returns domain.ErrDuplicateFavorite if that exact
// location is already bookmarked for profileID.
//
// Note on context: Wails v2's generated bindings do not give bound methods
// access to a per-call context.Context, so this uses context.Background()
// internally - acceptable here because every underlying operation is a
// local SQLite query with no meaningful deadline to propagate.
func (s *FavoritesService) AddFavorite(profileID int64, bucket, prefix string) (domain.Favorite, error) {
	favorite, err := s.repo.Create(context.Background(), profileID, bucket, prefix)
	if err != nil {
		return domain.Favorite{}, fmt.Errorf("add favorite: %w", err)
	}

	return favorite, nil
}

// RemoveFavorite deletes the favorite identified by id.
func (s *FavoritesService) RemoveFavorite(id int64) error {
	if err := s.repo.Delete(context.Background(), id); err != nil {
		return fmt.Errorf("remove favorite %d: %w", id, err)
	}

	return nil
}

// GetFavorites returns every favorite across every profile, each with
// ProfileName populated, ordered newest-first - suitable for the Sidebar's
// global, profile-grouped favorites list.
func (s *FavoritesService) GetFavorites() ([]domain.Favorite, error) {
	favoritesList, err := s.repo.GetAll(context.Background())
	if err != nil {
		return nil, fmt.Errorf("get favorites: %w", err)
	}

	return favoritesList, nil
}
