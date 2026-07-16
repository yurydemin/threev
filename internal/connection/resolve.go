package connection

import (
	"context"
	"fmt"

	"threev/internal/crypto"
	"threev/internal/domain"
	"threev/internal/storage"
)

// ResolveProfile loads the profile identified by id from repo and decrypts
// its SecretAccessKey (if present) and SessionToken (if present) using key,
// returning a domain.Profile with plaintext credentials ready to build an
// S3 client from.
//
// This is a package-level function - rather than a ConnectionService method
// - so that other services needing a fully-resolved, decrypted profile
// (e.g. FileManagerService, Stage 2) can call it directly without taking a
// dependency on the whole ConnectionService: they only need the same
// *storage.ProfileRepository and encryption key that are already
// constructed once in app.go. ConnectionService.GetProfile is a thin
// wrapper around this function.
//
// SecretAccessKey is guarded the same way SessionToken already is (skip
// decryption entirely when empty) rather than unconditionally decrypting it
// - a profile created by ConnectionService.ImportProfiles (Block G) has a
// genuinely blank, never-encrypted SecretAccessKey, and calling
// crypto.Decrypt("", key) does not return a helpful "no secret" result: an
// empty string decodes as zero bytes of base64 with no error, then fails
// GCM's nonce-length check with a confusing "ciphertext too short" message.
// Before Block G every stored profile was guaranteed a non-empty encrypted
// SecretAccessKey (connection.ValidateProfile enforced it on every write),
// so this case could not previously occur - this guard exists specifically
// because that invariant no longer holds for an imported-but-not-yet-edited
// profile.
func ResolveProfile(ctx context.Context, repo *storage.ProfileRepository, key [32]byte, id int64) (domain.Profile, error) {
	p, err := repo.GetByID(ctx, id)
	if err != nil {
		return domain.Profile{}, fmt.Errorf("get profile %d: %w", id, err)
	}

	if p.SecretAccessKey != "" {
		secret, err := crypto.Decrypt(p.SecretAccessKey, key)
		if err != nil {
			return domain.Profile{}, fmt.Errorf("get profile %d: decrypt secret access key: %w", id, err)
		}

		p.SecretAccessKey = string(secret)
	}

	if p.SessionToken != "" {
		token, err := crypto.Decrypt(p.SessionToken, key)
		if err != nil {
			return domain.Profile{}, fmt.Errorf("get profile %d: decrypt session token: %w", id, err)
		}

		p.SessionToken = string(token)
	}

	return p, nil
}
