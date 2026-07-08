package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"threev/internal/domain"
)

// ProfileRepository provides CRUD persistence for domain.Profile against
// the "profiles" SQLite table.
//
// ProfileRepository never encrypts or decrypts AccessKeyID,
// SecretAccessKey, or SessionToken: it treats whatever string it is given
// as an opaque value to store or return unchanged. Encrypting values before
// Create/Update and decrypting values returned by GetByID/GetAll is the
// Service layer's responsibility.
type ProfileRepository struct {
	db *sql.DB
}

// NewProfileRepository returns a ProfileRepository backed by db.
func NewProfileRepository(db *sql.DB) *ProfileRepository {
	return &ProfileRepository{db: db}
}

// Create inserts a new profile row and returns p with ID, CreatedAt, and
// UpdatedAt populated from the database.
func (r *ProfileRepository) Create(ctx context.Context, p domain.Profile) (domain.Profile, error) {
	headers, err := encodeCustomHeaders(p.CustomHeaders)
	if err != nil {
		return domain.Profile{}, fmt.Errorf("encode custom headers: %w", err)
	}

	const query = `
INSERT INTO profiles (
    name, endpoint_url, region, access_key_id, secret_access_key,
    session_token, path_style, verify_ssl, custom_headers
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	result, err := r.db.ExecContext(ctx, query,
		p.Name, p.EndpointURL, p.Region, p.AccessKeyID, p.SecretAccessKey,
		nullableString(p.SessionToken), p.PathStyle, p.VerifySSL, headers,
	)
	if err != nil {
		return domain.Profile{}, fmt.Errorf("create profile: %w", mapProfileWriteError(err))
	}

	id, err := result.LastInsertId()
	if err != nil {
		return domain.Profile{}, fmt.Errorf("create profile: read last insert id: %w", err)
	}

	return r.GetByID(ctx, id)
}

// GetByID returns the profile with the given id, or domain.ErrProfileNotFound
// if no such profile exists.
func (r *ProfileRepository) GetByID(ctx context.Context, id int64) (domain.Profile, error) {
	const query = `
SELECT id, name, endpoint_url, region, access_key_id, secret_access_key,
       session_token, path_style, verify_ssl, custom_headers, created_at, updated_at
FROM profiles
WHERE id = ?`

	row := r.db.QueryRowContext(ctx, query, id)

	p, err := scanProfile(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Profile{}, fmt.Errorf("get profile %d: %w", id, domain.ErrProfileNotFound)
		}

		return domain.Profile{}, fmt.Errorf("get profile %d: %w", id, err)
	}

	return p, nil
}

// GetAll returns every profile, ordered by name.
func (r *ProfileRepository) GetAll(ctx context.Context) ([]domain.Profile, error) {
	const query = `
SELECT id, name, endpoint_url, region, access_key_id, secret_access_key,
       session_token, path_style, verify_ssl, custom_headers, created_at, updated_at
FROM profiles
ORDER BY name COLLATE NOCASE`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get all profiles: %w", err)
	}
	defer rows.Close()

	profiles := make([]domain.Profile, 0)

	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, fmt.Errorf("get all profiles: scan row: %w", err)
		}

		profiles = append(profiles, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get all profiles: iterate rows: %w", err)
	}

	return profiles, nil
}

// Update overwrites every column of the profile identified by p.ID with the
// values in p, and refreshes updated_at. It returns domain.ErrProfileNotFound
// if p.ID does not exist.
func (r *ProfileRepository) Update(ctx context.Context, p domain.Profile) (domain.Profile, error) {
	headers, err := encodeCustomHeaders(p.CustomHeaders)
	if err != nil {
		return domain.Profile{}, fmt.Errorf("encode custom headers: %w", err)
	}

	const query = `
UPDATE profiles
SET name = ?, endpoint_url = ?, region = ?, access_key_id = ?, secret_access_key = ?,
    session_token = ?, path_style = ?, verify_ssl = ?, custom_headers = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?`

	result, err := r.db.ExecContext(ctx, query,
		p.Name, p.EndpointURL, p.Region, p.AccessKeyID, p.SecretAccessKey,
		nullableString(p.SessionToken), p.PathStyle, p.VerifySSL, headers, p.ID,
	)
	if err != nil {
		return domain.Profile{}, fmt.Errorf("update profile %d: %w", p.ID, mapProfileWriteError(err))
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return domain.Profile{}, fmt.Errorf("update profile %d: read rows affected: %w", p.ID, err)
	}

	if affected == 0 {
		return domain.Profile{}, fmt.Errorf("update profile %d: %w", p.ID, domain.ErrProfileNotFound)
	}

	return r.GetByID(ctx, p.ID)
}

// Delete removes the profile with the given id. It returns
// domain.ErrProfileNotFound if no such profile exists.
func (r *ProfileRepository) Delete(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM profiles WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete profile %d: %w", id, err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete profile %d: read rows affected: %w", id, err)
	}

	if affected == 0 {
		return fmt.Errorf("delete profile %d: %w", id, domain.ErrProfileNotFound)
	}

	return nil
}

// ExistsByName reports whether a profile named name already exists, other
// than the profile identified by excludeID. Pass excludeID <= 0 when
// checking for a brand new profile (create); pass the profile's own ID when
// checking during an update, so the profile does not conflict with itself.
func (r *ProfileRepository) ExistsByName(ctx context.Context, name string, excludeID int64) (bool, error) {
	const query = `SELECT EXISTS(SELECT 1 FROM profiles WHERE name = ? AND id != ?)`

	var exists bool
	if err := r.db.QueryRowContext(ctx, query, name, excludeID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check profile name %q exists: %w", name, err)
	}

	return exists, nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows, letting scanProfile
// be shared between single-row and multi-row queries.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanProfile scans a single profiles row (matching the column order used by
// GetByID/GetAll) into a domain.Profile.
func scanProfile(s rowScanner) (domain.Profile, error) {
	var (
		p             domain.Profile
		sessionToken  sql.NullString
		customHeaders sql.NullString
	)

	if err := s.Scan(
		&p.ID, &p.Name, &p.EndpointURL, &p.Region, &p.AccessKeyID, &p.SecretAccessKey,
		&sessionToken, &p.PathStyle, &p.VerifySSL, &customHeaders, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return domain.Profile{}, err
	}

	p.SessionToken = sessionToken.String

	headers, err := decodeCustomHeaders(customHeaders)
	if err != nil {
		return domain.Profile{}, fmt.Errorf("decode custom headers: %w", err)
	}

	p.CustomHeaders = headers

	return p, nil
}

// encodeCustomHeaders serializes headers to a JSON string for storage in the
// custom_headers TEXT column. A nil or empty map is stored as SQL NULL.
func encodeCustomHeaders(headers map[string]string) (sql.NullString, error) {
	if len(headers) == 0 {
		return sql.NullString{}, nil
	}

	b, err := json.Marshal(headers)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("marshal custom headers: %w", err)
	}

	return sql.NullString{String: string(b), Valid: true}, nil
}

// decodeCustomHeaders parses the custom_headers TEXT column back into a map.
// A NULL/empty column yields a nil map.
func decodeCustomHeaders(raw sql.NullString) (map[string]string, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}

	var headers map[string]string
	if err := json.Unmarshal([]byte(raw.String), &headers); err != nil {
		return nil, fmt.Errorf("unmarshal custom headers: %w", err)
	}

	return headers, nil
}

// nullableString converts an empty string to SQL NULL, so optional
// text columns like session_token read back as "" via sql.NullString.
func nullableString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}

	return sql.NullString{String: s, Valid: true}
}

// mapProfileWriteError maps a SQLite UNIQUE constraint violation on
// profiles.name to domain.ErrDuplicateProfileName, leaving other errors
// unchanged.
func mapProfileWriteError(err error) error {
	if err == nil {
		return nil
	}

	if isUniqueConstraintError(err) {
		return domain.ErrDuplicateProfileName
	}

	return err
}
