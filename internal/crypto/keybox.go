package crypto

import "sync"

// KeyBox is a thread-safe, mutable container for the application's
// AES-256-GCM credential-encryption key. It exists because every *Service
// (ConnectionService, FileManagerService, TransferService,
// s3client.ConnectionManager) used to be constructed with a plain
// [32]byte encryption key, computed once in app.go's newApp() before
// wails.Run() / OnStartup ever fires - i.e. before the frontend can even
// show a password prompt. Since Этап 4 суб-этап 4.4 lets that key instead
// depend on a user-supplied master password, the key can no longer be a
// constructor-time constant when a master password is set: newApp() must
// construct every service with an EMPTY KeyBox, to be filled in later (via
// SettingsService.Unlock) once the frontend has collected the password.
//
// KeyBox deliberately holds at most one key at a time, mutually exclusive:
// either the machine-only key (no master password configured) or a
// password-derived key (crypto.DeriveKey(passphrase, salt) with a non-empty
// passphrase) - never both, never layered. See DeriveKey's own doc comment
// for why a single combined derivation already achieves "you need both this
// machine AND this password" without double encryption.
//
// There is deliberately no Lock() method and no background expiry timer:
// once a process successfully installs a key (either automatically, for a
// machine-only setup, or via an explicit Unlock call), it stays installed
// for the remaining lifetime of the process - this is a settled decision
// (Этап 4 суб-этап 4.4 plan), not an oversight; re-locking on idle timeout
// is explicitly out of scope.
type KeyBox struct {
	mu  sync.RWMutex
	key [32]byte
	set bool
}

// NewKeyBox returns an empty KeyBox (Get returns ok=false) until Set is
// called.
func NewKeyBox() *KeyBox {
	return &KeyBox{}
}

// Set installs key, replacing whatever was previously held (if anything).
func (b *KeyBox) Set(key [32]byte) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.key = key
	b.set = true
}

// Get returns the currently held key and true, or a zero key and false if
// Set has never been called (or Clear was called since).
func (b *KeyBox) Get() (key [32]byte, ok bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.key, b.set
}

// Clear zeroes the held key and marks the box empty. Idempotent - safe to
// call on an already-empty box. Best-effort memory hygiene only: Go's
// garbage collector gives no cryptographic guarantee that copies of the key
// made elsewhere (e.g. by value on some earlier Get call's stack frame) are
// also scrubbed - this is a documented MVP limitation (Этап 4 plan's "Known
// risks" section), not a claim of secure erasure.
func (b *KeyBox) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.key = [32]byte{}
	b.set = false
}
