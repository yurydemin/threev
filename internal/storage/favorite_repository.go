package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"threev/internal/domain"
)

// FavoriteRepository provides persistence for domain.Favorite against the
// "favorites" SQLite table.
type FavoriteRepository struct {
	db *sql.DB
}

// NewFavoriteRepository returns a FavoriteRepository backed by db.
func NewFavoriteRepository(db *sql.DB) *FavoriteRepository {
	return &FavoriteRepository{db: db}
}

// Create inserts a new favorite for (profileID, bucket, prefix) and returns
// the created row, with ProfileName populated from profiles.name. It
// returns domain.ErrDuplicateFavorite if that exact location is already
// bookmarked for profileID (favorites.profile_id, bucket, prefix is
// enforced UNIQUE by the schema).
func (r *FavoriteRepository) Create(ctx context.Context, profileID int64, bucket, prefix string) (domain.Favorite, error) {
	const query = `
INSERT INTO favorites (profile_id, bucket, prefix)
VALUES (?, ?, ?)`

	result, err := r.db.ExecContext(ctx, query, profileID, bucket, prefix)
	if err != nil {
		return domain.Favorite{}, fmt.Errorf("create favorite: %w", mapFavoriteWriteError(err))
	}

	id, err := result.LastInsertId()
	if err != nil {
		return domain.Favorite{}, fmt.Errorf("create favorite: read last insert id: %w", err)
	}

	return r.GetByID(ctx, id)
}

// GetByID returns the favorite with the given id, with ProfileName
// populated from profiles.name, or domain.ErrFavoriteNotFound if no such
// favorite exists.
func (r *FavoriteRepository) GetByID(ctx context.Context, id int64) (domain.Favorite, error) {
	const query = `
SELECT f.id, f.profile_id, p.name, f.bucket, f.prefix, f.created_at
FROM favorites f
JOIN profiles p ON p.id = f.profile_id
WHERE f.id = ?`

	row := r.db.QueryRowContext(ctx, query, id)

	f, err := scanFavorite(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Favorite{}, fmt.Errorf("get favorite %d: %w", id, domain.ErrFavoriteNotFound)
		}

		return domain.Favorite{}, fmt.Errorf("get favorite %d: %w", id, err)
	}

	return f, nil
}

// GetAll returns every favorite across every profile, each with
// ProfileName populated from profiles.name, ordered by created_at
// descending (newest first) - suitable for the global, profile-grouped
// Sidebar favorites list. f.id DESC is a secondary sort key, breaking ties
// between rows created within the same created_at second (SQLite's
// CURRENT_TIMESTAMP has only whole-second resolution) so that "newest
// first" stays well-defined even for favorites added in quick succession.
func (r *FavoriteRepository) GetAll(ctx context.Context) ([]domain.Favorite, error) {
	const query = `
SELECT f.id, f.profile_id, p.name, f.bucket, f.prefix, f.created_at
FROM favorites f
JOIN profiles p ON p.id = f.profile_id
ORDER BY f.created_at DESC, f.id DESC`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get all favorites: %w", err)
	}
	defer rows.Close()

	favorites := make([]domain.Favorite, 0)

	for rows.Next() {
		f, err := scanFavorite(rows)
		if err != nil {
			return nil, fmt.Errorf("get all favorites: scan row: %w", err)
		}

		favorites = append(favorites, f)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get all favorites: iterate rows: %w", err)
	}

	return favorites, nil
}

// Delete removes the favorite with the given id. It returns
// domain.ErrFavoriteNotFound if no such favorite exists.
func (r *FavoriteRepository) Delete(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM favorites WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete favorite %d: %w", id, err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete favorite %d: read rows affected: %w", id, err)
	}

	if affected == 0 {
		return fmt.Errorf("delete favorite %d: %w", id, domain.ErrFavoriteNotFound)
	}

	return nil
}

// scanFavorite scans a single joined favorites/profiles row (matching the
// column order used by GetByID/GetAll) into a domain.Favorite.
func scanFavorite(s rowScanner) (domain.Favorite, error) {
	var f domain.Favorite

	if err := s.Scan(
		&f.ID, &f.ProfileID, &f.ProfileName, &f.Bucket, &f.Prefix, &f.CreatedAt,
	); err != nil {
		return domain.Favorite{}, err
	}

	return f, nil
}

// mapFavoriteWriteError maps a SQLite UNIQUE constraint violation on
// favorites (profile_id, bucket, prefix) to domain.ErrDuplicateFavorite,
// leaving other errors unchanged.
func mapFavoriteWriteError(err error) error {
	if err == nil {
		return nil
	}

	if isUniqueConstraintError(err) {
		return domain.ErrDuplicateFavorite
	}

	return err
}
