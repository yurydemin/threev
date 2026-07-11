package crypto

import (
	"sync"
	"testing"
)

func TestKeyBoxGetOnFreshBoxReturnsNotOK(t *testing.T) {
	t.Parallel()

	box := NewKeyBox()

	key, ok := box.Get()
	if ok {
		t.Errorf("Get() on a fresh KeyBox returned ok=true, want false")
	}
	if key != ([32]byte{}) {
		t.Errorf("Get() on a fresh KeyBox returned key=%v, want zero value", key)
	}
}

func TestKeyBoxSetThenGet(t *testing.T) {
	t.Parallel()

	box := NewKeyBox()

	var want [32]byte
	for i := range want {
		want[i] = byte(i)
	}

	box.Set(want)

	got, ok := box.Get()
	if !ok {
		t.Fatalf("Get() after Set() returned ok=false, want true")
	}
	if got != want {
		t.Errorf("Get() after Set(%v) = %v, want %v", want, got, want)
	}
}

func TestKeyBoxSetReplacesPreviousKey(t *testing.T) {
	t.Parallel()

	box := NewKeyBox()

	var first, second [32]byte
	first[0] = 0x01
	second[0] = 0x02

	box.Set(first)
	box.Set(second)

	got, ok := box.Get()
	if !ok {
		t.Fatalf("Get() after two Set() calls returned ok=false, want true")
	}
	if got != second {
		t.Errorf("Get() after Set(first); Set(second) = %v, want %v (the second key)", got, second)
	}
}

func TestKeyBoxClearOnFreshBoxDoesNotPanic(t *testing.T) {
	t.Parallel()

	box := NewKeyBox()

	box.Clear()
	box.Clear() // idempotent - a second Clear on an already-empty box must not panic either.

	if _, ok := box.Get(); ok {
		t.Errorf("Get() after Clear() on a fresh box returned ok=true, want false")
	}
}

func TestKeyBoxClearAfterSetIsIdempotent(t *testing.T) {
	t.Parallel()

	box := NewKeyBox()

	var key [32]byte
	key[0] = 0xAB

	box.Set(key)
	box.Clear()
	box.Clear()

	got, ok := box.Get()
	if ok {
		t.Errorf("Get() after Clear() returned ok=true, want false")
	}
	if got != ([32]byte{}) {
		t.Errorf("Get() after Clear() returned key=%v, want zero value", got)
	}
}

// TestKeyBoxConcurrentAccess exercises Get/Set/Clear from many goroutines at
// once under -race: KeyBox's whole reason to exist is to be shared, mutably,
// across every *Service constructed in app.go's newApp() and later written
// to once from SettingsService.Unlock/SetMasterPassword/
// RemoveMasterPassword, so a data race here would be a direct,
// security-relevant regression (a torn/half-written [32]byte key observed
// mid-Set by a concurrent Get). This does not assert any particular final
// value - only that concurrent access itself is race-free and never panics.
func TestKeyBoxConcurrentAccess(t *testing.T) {
	t.Parallel()

	box := NewKeyBox()

	const goroutines = 50
	const iterations = 200

	var wg sync.WaitGroup

	for g := 0; g < goroutines; g++ {
		wg.Add(1)

		go func(seed byte) {
			defer wg.Done()

			var key [32]byte
			key[0] = seed

			for i := 0; i < iterations; i++ {
				switch i % 3 {
				case 0:
					box.Set(key)
				case 1:
					box.Get()
				case 2:
					box.Clear()
				}
			}
		}(byte(g))
	}

	wg.Wait()
}
