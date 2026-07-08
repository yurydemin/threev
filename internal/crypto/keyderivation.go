package crypto

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"

	"github.com/denisbrodbeck/machineid"
	"golang.org/x/crypto/argon2"

	"threev/internal/config"
)

// machineSeedFileName is the name of the fallback seed file created under
// config.AppDataDir when the OS-level machine ID cannot be determined.
const machineSeedFileName = "machine.seed"

// appID scopes the machine-id HMAC (see machineid.ProtectedID) to this
// application, so the resulting protected ID cannot be correlated with IDs
// derived by other applications on the same machine.
const appID = "threev"

// Argon2id parameters used by DeriveKey. These follow the OWASP-recommended
// baseline for interactive, single-user desktop use: 64 MiB memory, 3
// iterations, 4 parallel lanes, 32-byte (256-bit) output suitable for
// AES-256.
const (
	argon2Time    = 3
	argon2Memory  = 64 * 1024 // 64 MB, in KiB as required by argon2.IDKey
	argon2Threads = 4
	argon2KeyLen  = 32
)

// saltSeparator is inserted between the machine seed and the user
// passphrase before hashing, so an empty passphrase cannot collide with a
// non-empty one that happens to start with the same bytes as the seed.
var saltSeparator = []byte{0}

// MachineSeed returns a stable, machine-specific byte sequence suitable for
// use as key derivation material.
//
// It first attempts to derive a protected (HMAC-SHA256) machine ID via
// machineid.ProtectedID, which never exposes the raw OS machine ID. If that
// fails (e.g. the platform-specific mechanism is unavailable in a sandboxed
// or restricted environment), it falls back to a randomly generated 32-byte
// seed persisted at <AppDataDir>/machine.seed (permissions 0600), created on
// first use and reused thereafter.
func MachineSeed() ([]byte, error) {
	protectedID, err := machineid.ProtectedID(appID)
	if err == nil {
		return []byte(protectedID), nil
	}

	seed, seedErr := fallbackMachineSeed()
	if seedErr != nil {
		return nil, fmt.Errorf("derive machine seed: protected id failed (%w), fallback failed: %w", err, seedErr)
	}

	return seed, nil
}

// fallbackMachineSeed reads the persisted fallback seed file, creating it
// with a fresh random value if it does not yet exist.
func fallbackMachineSeed() ([]byte, error) {
	dir, err := config.AppDataDir()
	if err != nil {
		return nil, fmt.Errorf("resolve app data dir: %w", err)
	}

	path := filepath.Join(dir, machineSeedFileName)

	existing, err := os.ReadFile(path) //nolint:gosec // path is built from config.AppDataDir() + a fixed file name, not user input
	if err == nil {
		return existing, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read machine seed file %q: %w", path, err)
	}

	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return nil, fmt.Errorf("generate machine seed: %w", err)
	}

	if err := os.WriteFile(path, seed, 0o600); err != nil {
		return nil, fmt.Errorf("write machine seed file %q: %w", path, err)
	}

	return seed, nil
}

// GenerateSalt returns 16 cryptographically random bytes, intended to be
// generated once at first run and persisted by the caller (e.g. in a
// settings store) for reuse on subsequent calls to DeriveKey.
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}

	return salt, nil
}

// DeriveKey derives a 256-bit symmetric key from the current machine's
// identity, an optional user passphrase, and the given salt, using
// Argon2id.
//
// passphrase may be the empty string when the user has not configured one;
// the machine seed alone still makes the derived key machine-specific. salt
// should be generated once via GenerateSalt and persisted by the caller,
// then passed in unchanged on every subsequent call so the same key is
// reproduced.
func DeriveKey(passphrase string, salt []byte) ([32]byte, error) {
	var key [32]byte

	seed, err := MachineSeed()
	if err != nil {
		return key, fmt.Errorf("derive key: %w", err)
	}

	password := make([]byte, 0, len(seed)+len(saltSeparator)+len(passphrase))
	password = append(password, seed...)
	password = append(password, saltSeparator...)
	password = append(password, []byte(passphrase)...)

	derived := argon2.IDKey(password, salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)
	copy(key[:], derived)

	return key, nil
}
