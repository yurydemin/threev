package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// Encrypt encrypts plaintext with AES-256-GCM using key, producing a
// base64-encoded string of the form nonce‖ciphertext‖tag. A fresh random
// 12-byte nonce is generated on every call and never reused.
func Encrypt(plaintext []byte, key [32]byte) (string, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", fmt.Errorf("encrypt: new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("encrypt: new gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("encrypt: generate nonce: %w", err)
	}

	sealed := gcm.Seal(nonce, nonce, plaintext, nil)

	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt: it base64-decodes ciphertext, splits off the
// leading nonce, and authenticates/decrypts the remainder with AES-256-GCM
// using key. It returns a non-nil error (never panics) if the input is not
// valid base64, is shorter than the nonce size, or fails GCM authentication
// (e.g. wrong key or corrupted/tampered data).
func Decrypt(ciphertext string, key [32]byte) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt: invalid base64: %w", err)
	}

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("decrypt: new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("decrypt: new gcm: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(raw) < nonceSize {
		return nil, fmt.Errorf("decrypt: ciphertext too short: got %d bytes, need at least %d", len(raw), nonceSize)
	}

	nonce, sealed := raw[:nonceSize], raw[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: authentication failed: %w", err)
	}

	return plaintext, nil
}
