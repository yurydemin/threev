package crypto

import (
	"encoding/base64"
	"strings"
	"testing"
)

func testKey(t *testing.T, seed byte) [32]byte {
	t.Helper()

	var key [32]byte
	for i := range key {
		key[i] = seed + byte(i)
	}

	return key
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		plaintext []byte
	}{
		{"empty", []byte("")},
		{"short", []byte("hello")},
		{"credentials json", []byte(`{"access_key":"AKIA...","secret_key":"abc123"}`)},
		{"binary", []byte{0x00, 0x01, 0xFF, 0xFE, 0x10}},
		{"long", []byte(strings.Repeat("threev-secret-", 1000))},
	}

	key := testKey(t, 1)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ciphertext, err := Encrypt(tt.plaintext, key)
			if err != nil {
				t.Fatalf("Encrypt() error = %v", err)
			}

			got, err := Decrypt(ciphertext, key)
			if err != nil {
				t.Fatalf("Decrypt() error = %v", err)
			}

			if string(got) != string(tt.plaintext) {
				t.Errorf("round trip mismatch: got %q, want %q", got, tt.plaintext)
			}
		})
	}
}

func TestEncryptProducesUniqueNonces(t *testing.T) {
	t.Parallel()

	key := testKey(t, 2)
	plaintext := []byte("same plaintext every time")

	first, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	second, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	if first == second {
		t.Error("Encrypt() produced identical ciphertext for two calls; nonce is not being randomized")
	}
}

func TestDecryptInvalidBase64(t *testing.T) {
	t.Parallel()

	key := testKey(t, 3)

	_, err := Decrypt("not-valid-base64!!!", key)
	if err == nil {
		t.Fatal("Decrypt() error = nil, want error for invalid base64")
	}
}

func TestDecryptTooShort(t *testing.T) {
	t.Parallel()

	key := testKey(t, 4)

	// Fewer bytes than the 12-byte GCM nonce.
	short := base64.StdEncoding.EncodeToString([]byte{1, 2, 3})

	_, err := Decrypt(short, key)
	if err == nil {
		t.Fatal("Decrypt() error = nil, want error for too-short ciphertext")
	}
}

func TestDecryptCorruptedCiphertext(t *testing.T) {
	t.Parallel()

	key := testKey(t, 5)

	ciphertext, err := Encrypt([]byte("secret payload"), key)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		t.Fatalf("failed to decode test fixture: %v", err)
	}

	// Flip a byte well past the nonce, inside the ciphertext/tag.
	raw[len(raw)-1] ^= 0xFF
	corrupted := base64.StdEncoding.EncodeToString(raw)

	_, err = Decrypt(corrupted, key)
	if err == nil {
		t.Fatal("Decrypt() error = nil, want authentication error for corrupted ciphertext")
	}
}

func TestDecryptWrongKey(t *testing.T) {
	t.Parallel()

	encryptKey := testKey(t, 6)
	decryptKey := testKey(t, 7)

	ciphertext, err := Encrypt([]byte("only readable with the right key"), encryptKey)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	_, err = Decrypt(ciphertext, decryptKey)
	if err == nil {
		t.Fatal("Decrypt() error = nil, want authentication error for wrong key")
	}
}
