package s3client

import (
	"sync"
	"testing"
	"time"
)

func TestCircuitBreaker_AllowUnknownHost(t *testing.T) {
	b := NewCircuitBreaker()

	if !b.Allow("s3.example.com") {
		t.Fatal("Allow() = false for a never-seen host, want true")
	}
}

func TestCircuitBreaker_OpensAfterConsecutiveFailures(t *testing.T) {
	b := NewCircuitBreaker()
	host := "s3.example.com"

	for i := 0; i < maxConsecutiveFailures; i++ {
		b.RecordFailure(host, "network")
	}

	if b.Allow(host) {
		t.Fatal("Allow() = true after 5 consecutive network failures, want false (Open)")
	}
}

func TestCircuitBreaker_NonCountingCategoriesDoNotTripBreaker(t *testing.T) {
	tests := []string{"auth", "tls", "cancelled", "unknown"}

	for _, category := range tests {
		t.Run(category, func(t *testing.T) {
			b := NewCircuitBreaker()
			host := "s3.example.com"

			for i := 0; i < maxConsecutiveFailures; i++ {
				b.RecordFailure(host, category)
			}

			if !b.Allow(host) {
				t.Fatalf("Allow() = false after 5 consecutive %q failures, want true (not counted)", category)
			}
		})
	}
}

func TestCircuitBreaker_SuccessResetsConsecutiveFailures(t *testing.T) {
	b := NewCircuitBreaker()
	host := "s3.example.com"

	for i := 0; i < maxConsecutiveFailures-1; i++ {
		b.RecordFailure(host, "network")
	}

	b.RecordSuccess(host)

	for i := 0; i < maxConsecutiveFailures-1; i++ {
		b.RecordFailure(host, "network")
	}

	if !b.Allow(host) {
		t.Fatal("Allow() = false after only 4 consecutive failures (reset by an intervening success), want true")
	}
}

func TestCircuitBreaker_TransitionsToHalfOpenAfterOpenDuration(t *testing.T) {
	b := NewCircuitBreaker()
	host := "s3.example.com"

	now := time.Now()
	b.nowFunc = func() time.Time { return now }

	for i := 0; i < maxConsecutiveFailures; i++ {
		b.RecordFailure(host, "network")
	}

	if b.Allow(host) {
		t.Fatal("Allow() = true immediately after opening, want false")
	}

	// Advance the injected clock past openDuration.
	now = now.Add(openDuration + time.Second)

	if !b.Allow(host) {
		t.Fatal("Allow() = false after openDuration elapsed, want true (HalfOpen trial)")
	}
}

func TestCircuitBreaker_HalfOpenAllowsOnlyOneTrialRequest(t *testing.T) {
	b := NewCircuitBreaker()
	host := "s3.example.com"

	now := time.Now()
	b.nowFunc = func() time.Time { return now }

	for i := 0; i < maxConsecutiveFailures; i++ {
		b.RecordFailure(host, "network")
	}

	now = now.Add(openDuration + time.Second)

	if !b.Allow(host) {
		t.Fatal("Allow() = false for the first HalfOpen call, want true")
	}

	if b.Allow(host) {
		t.Fatal("Allow() = true for a second concurrent HalfOpen call, want false (only one trial in flight)")
	}
}

func TestCircuitBreaker_HalfOpenSuccessCloses(t *testing.T) {
	b := NewCircuitBreaker()
	host := "s3.example.com"

	now := time.Now()
	b.nowFunc = func() time.Time { return now }

	for i := 0; i < maxConsecutiveFailures; i++ {
		b.RecordFailure(host, "network")
	}

	now = now.Add(openDuration + time.Second)

	if !b.Allow(host) {
		t.Fatal("Allow() = false for the HalfOpen trial, want true")
	}

	b.RecordSuccess(host)

	if !b.Allow(host) {
		t.Fatal("Allow() = false after a successful trial request, want true (Closed)")
	}

	// Closed also means a further trial-in-flight restriction no longer
	// applies: a second concurrent Allow should also succeed now.
	if !b.Allow(host) {
		t.Fatal("Allow() = false for a second call once Closed, want true")
	}
}

func TestCircuitBreaker_HalfOpenFailureReopens(t *testing.T) {
	b := NewCircuitBreaker()
	host := "s3.example.com"

	now := time.Now()
	b.nowFunc = func() time.Time { return now }

	for i := 0; i < maxConsecutiveFailures; i++ {
		b.RecordFailure(host, "network")
	}

	now = now.Add(openDuration + time.Second)

	if !b.Allow(host) {
		t.Fatal("Allow() = false for the HalfOpen trial, want true")
	}

	b.RecordFailure(host, "network")

	if b.Allow(host) {
		t.Fatal("Allow() = true immediately after a failed trial request, want false (back to Open)")
	}

	// A fresh openDuration window should have started: advancing only
	// past the *original* window (already consumed above) must not be
	// enough; advance a full new window from here.
	now = now.Add(openDuration + time.Second)

	if !b.Allow(host) {
		t.Fatal("Allow() = false after a fresh openDuration elapsed post-reopen, want true (HalfOpen trial)")
	}
}

func TestCircuitBreaker_RecordSuccessOnUnknownHostIsNoop(t *testing.T) {
	b := NewCircuitBreaker()

	b.RecordSuccess("s3.example.com")

	if !b.Allow("s3.example.com") {
		t.Fatal("Allow() = false after RecordSuccess on a never-seen host, want true")
	}
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	b := NewCircuitBreaker()
	hosts := []string{"a.example.com", "b.example.com", "c.example.com"}

	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			host := hosts[i%len(hosts)]

			if b.Allow(host) {
				if i%3 == 0 {
					b.RecordFailure(host, "network")
				} else {
					b.RecordSuccess(host)
				}
			}
		}(i)
	}

	wg.Wait()
}
