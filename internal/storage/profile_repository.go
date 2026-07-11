package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"threev/internal/crypto"
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

// reencryptRow is the in-memory snapshot ReencryptSecretsTx reads every
// profile row into (via a single SELECT) before writing anything back - see
// ReencryptSecretsTx's own doc comment for why the SELECT must be fully
// drained before the first UPDATE.
type reencryptRow struct {
	id              int64
	secretAccessKey string
	sessionToken    sql.NullString
}

// ReencryptSecretsTx re-encrypts every stored profile's SecretAccessKey
// and (if present) SessionToken from oldKey to newKey, within the given
// transaction - the caller (internal/appsettings/security.go) owns
// Begin/Commit/Rollback, so that a failure partway through (a decrypt
// failure on some corrupted row, a write error, ...) leaves EVERY profile's
// stored ciphertext untouched, never a mix of old- and new-key-encrypted
// rows. See SetMasterPassword/RemoveMasterPassword's own doc comments
// (internal/appsettings/security.go) for the full "commit only after every
// row succeeds, keyBox.Set only after commit" ordering this enables.
//
// Every row is first read into memory in full (via a single SELECT,
// fully drained into a []reencryptRow slice) before any UPDATE is issued -
// never interleaving a still-open *sql.Rows cursor with writes to that same
// table inside the same transaction. Nothing elsewhere in this file needs
// that same care (every other write method here issues exactly one
// statement at a time), but it matters here specifically because this
// method both reads and writes every row of the same table, and
// modernc.org/sqlite (this project's driver) is not guaranteed to tolerate
// a concurrent write against a table an unclosed Rows cursor from the same
// connection/transaction is still iterating - draining first sidesteps the
// question entirely rather than relying on driver-specific behavior.
//
// Any single row's failure (a decrypt failure against oldKey - e.g. a
// corrupted or already-differently-encrypted ciphertext - or an encrypt/
// write failure) aborts the entire method immediately, wrapped with the
// failing profile's id (fmt.Errorf("reencrypt profile %d: %w", id, err)):
// the caller is expected to roll back tx on any error return, per this
// method's own transaction-ownership contract above.
func (r *ProfileRepository) ReencryptSecretsTx(ctx context.Context, tx *sql.Tx, oldKey, newKey [32]byte) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, secret_access_key, session_token FROM profiles`)
	if err != nil {
		return fmt.Errorf("reencrypt secrets: list profiles: %w", err)
	}

	var toReencrypt []reencryptRow

	for rows.Next() {
		var row reencryptRow

		if err := rows.Scan(&row.id, &row.secretAccessKey, &row.sessionToken); err != nil {
			rows.Close()
			return fmt.Errorf("reencrypt secrets: scan profile row: %w", err)
		}

		toReencrypt = append(toReencrypt, row)
	}

	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("reencrypt secrets: iterate profile rows: %w", err)
	}

	if err := rows.Close(); err != nil {
		return fmt.Errorf("reencrypt secrets: close profile rows: %w", err)
	}

	const updateQuery = `UPDATE profiles SET secret_access_key = ?, session_token = ? WHERE id = ?`

	for _, row := range toReencrypt {
		newSecret, err := reencryptValue(row.secretAccessKey, oldKey, newKey)
		if err != nil {
			return fmt.Errorf("reencrypt profile %d: secret access key: %w", row.id, err)
		}

		newToken := row.sessionToken
		if row.sessionToken.Valid && row.sessionToken.String != "" {
			reencrypted, err := reencryptValue(row.sessionToken.String, oldKey, newKey)
			if err != nil {
				return fmt.Errorf("reencrypt profile %d: session token: %w", row.id, err)
			}

			newToken = sql.NullString{String: reencrypted, Valid: true}
		}

		if _, err := tx.ExecContext(ctx, updateQuery, newSecret, newToken, row.id); err != nil {
			return fmt.Errorf("reencrypt profile %d: write: %w", row.id, err)
		}
	}

	return nil
}

// reencryptValue decrypts ciphertext with oldKey and re-encrypts the
// resulting plaintext with newKey - the single-value operation
// ReencryptSecretsTx applies to each profile's SecretAccessKey and,
// separately, SessionToken.
func reencryptValue(ciphertext string, oldKey, newKey [32]byte) (string, error) {
	plaintext, err := crypto.Decrypt(ciphertext, oldKey)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	reencrypted, err := crypto.Encrypt(plaintext, newKey)
	if err != nil {
		return "", fmt.Errorf("encrypt: %w", err)
	}

	return reencrypted, nil
}
