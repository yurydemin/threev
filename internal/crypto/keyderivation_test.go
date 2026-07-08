package crypto

import (
	"bytes"
	"testing"
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
