package crypto

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"threev/internal/config"
)

func TestMachineSeedStable(t *testing.T) {
	t.Parallel()

	first, err := MachineSeed()
	if err != nil {
		t.Fatalf("MachineSeed() error = %v", err)
	}

	if len(first) == 0 {
		t.Fatal("MachineSeed() returned empty seed")
	}

	second, err := MachineSeed()
	if err != nil {
		t.Fatalf("MachineSeed() (second call) error = %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Errorf("MachineSeed() not stable across calls: %q != %q", first, second)
	}
}

// TestFallbackMachineSeedCreatesAndReuses exercises fallbackMachineSeed
// directly rather than relying on MachineSeed to reach it naturally.
// machineid.ProtectedID succeeds on ordinary GitHub Actions runners
// (ubuntu-latest, macos-latest and windows-latest are full VMs, each with
// the OS-level machine identifier the library reads readily available),
// so TestMachineSeedStable alone never actually forces the fallback code
// path in CI. Calling fallbackMachineSeed directly (it is unexported but
// this test lives in the same package) guarantees the fallback path itself
// -- file creation, persistence and reuse -- is covered independently of
// whatever machineid.ProtectedID happens to do on a given machine.
func TestFallbackMachineSeedCreatesAndReuses(t *testing.T) {
	dir, err := config.AppDataDir()
	if err != nil {
		t.Fatalf("config.AppDataDir() error = %v", err)
	}
	path := filepath.Join(dir, machineSeedFileName)

	// Preserve and restore any pre-existing seed file so this test does
	// not disturb real key material a developer may already have on disk
	// (e.g. if fallbackMachineSeed had genuinely been exercised before on
	// this machine).
	original, readErr := os.ReadFile(path) //nolint:gosec // fixed, package-controlled path
	existed := readErr == nil

	t.Cleanup(func() {
		if existed {
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Logf("failed to restore original machine seed file %q: %v", path, err)
			}
			return
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Logf("failed to remove test-created machine seed file %q: %v", path, err)
		}
	})

	if existed {
		if err := os.Remove(path); err != nil {
			t.Fatalf("failed to remove pre-existing machine seed file for test isolation: %v", err)
		}
	}

	seed1, err := fallbackMachineSeed()
	if err != nil {
		t.Fatalf("fallbackMachineSeed() error = %v", err)
	}
	if len(seed1) != 32 {
		t.Fatalf("fallbackMachineSeed() returned %d bytes, want 32", len(seed1))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("fallbackMachineSeed() did not persist seed file: %v", err)
	}

	// os.WriteFile's 0600 permission argument maps onto POSIX file mode
	// bits on Linux/macOS. Windows has no such POSIX permission model --
	// os/File there only distinguishes read-only vs. read-write via file
	// attributes, so 0600 is a no-op beyond "not read-only" and is not
	// meaningfully assertable here.
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("machine seed file permissions = %o, want 0600", perm)
		}
	}

	seed2, err := fallbackMachineSeed()
	if err != nil {
		t.Fatalf("fallbackMachineSeed() (second call) error = %v", err)
	}

	if !bytes.Equal(seed1, seed2) {
		t.Error("fallbackMachineSeed() not stable across calls: file was not reused")
	}
}

func TestDeriveKeyDeterministic(t *testing.T) {
	t.Parallel()

	salt := []byte("0123456789abcdef")

	key1, err := DeriveKey("correct horse", salt)
	if err != nil {
		t.Fatalf("DeriveKey() error = %v", err)
	}

	key2, err := DeriveKey("correct horse", salt)
	if err != nil {
		t.Fatalf("DeriveKey() (second call) error = %v", err)
	}

	if key1 != key2 {
		t.Error("DeriveKey() is not deterministic for identical inputs")
	}
}

func TestDeriveKeyEmptyPassphraseIsValid(t *testing.T) {
	t.Parallel()

	salt := []byte("0123456789abcdef")

	key1, err := DeriveKey("", salt)
	if err != nil {
		t.Fatalf("DeriveKey() error = %v", err)
	}

	key2, err := DeriveKey("", salt)
	if err != nil {
		t.Fatalf("DeriveKey() (second call) error = %v", err)
	}

	if key1 != key2 {
		t.Error("DeriveKey() with empty passphrase is not deterministic")
	}

	var zero [32]byte
	if key1 == zero {
		t.Error("DeriveKey() with empty passphrase produced an all-zero key")
	}
}

func TestDeriveKeyDifferentPassphrasesDiffer(t *testing.T) {
	t.Parallel()

	salt := []byte("0123456789abcdef")

	key1, err := DeriveKey("passphrase-one", salt)
	if err != nil {
		t.Fatalf("DeriveKey() error = %v", err)
	}

	key2, err := DeriveKey("passphrase-two", salt)
	if err != nil {
		t.Fatalf("DeriveKey() error = %v", err)
	}

	if key1 == key2 {
		t.Error("DeriveKey() produced identical keys for different passphrases")
	}
}

func TestDeriveKeyDifferentSaltsDiffer(t *testing.T) {
	t.Parallel()

	key1, err := DeriveKey("same passphrase", []byte("0123456789abcdef"))
	if err != nil {
		t.Fatalf("DeriveKey() error = %v", err)
	}

	key2, err := DeriveKey("same passphrase", []byte("fedcba9876543210"))
	if err != nil {
		t.Fatalf("DeriveKey() error = %v", err)
	}

	if key1 == key2 {
		t.Error("DeriveKey() produced identical keys for different salts")
	}
}

func TestGenerateSaltLengthAndUniqueness(t *testing.T) {
	t.Parallel()

	salt1, err := GenerateSalt()
	if err != nil {
		t.Fatalf("GenerateSalt() error = %v", err)
	}

	if len(salt1) != 16 {
		t.Errorf("GenerateSalt() returned %d bytes, want 16", len(salt1))
	}

	salt2, err := GenerateSalt()
	if err != nil {
		t.Fatalf("GenerateSalt() error = %v", err)
	}

	if bytes.Equal(salt1, salt2) {
		t.Error("GenerateSalt() produced identical salts on two consecutive calls")
	}
}
