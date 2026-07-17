package s3client

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/aws/smithy-go"
)

// fastPolicy is a RetryPolicy with the same shape as PartRetryPolicy but a
// millisecond-scale backoff, so tests that need several attempts/pauses to
// run don't spend real seconds waiting on time.Sleep between them.
var fastPolicy = RetryPolicy{MaxAttempts: 5, BaseDelay: time.Millisecond, MaxDelay: 4 * time.Millisecond}

// networkErr returns a *net.OpError, which ClassifyError/isNetworkError
// recognizes as category "network" - a retryable, breaker-counted failure.
func networkErr() error {
	return &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset")}
}

func TestWithRetry_SuccessFirstAttempt(t *testing.T) {
	breaker := NewCircuitBreaker()
	host := "s3.example.com"

	var calls int

	err := WithRetry(context.Background(), breaker, fastPolicy, host, func(_ context.Context, isRetry bool) error {
		calls++

		if isRetry {
			t.Error("isRetry = true on the first attempt, want false")
		}

		return nil
	})
	if err != nil {
		t.Fatalf("WithRetry() error = %v, want nil", err)
	}

	if calls != 1 {
		t.Fatalf("attempt called %d times, want 1", calls)
	}

	if !breaker.Allow(host) {
		t.Fatal("Allow() = false after a successful WithRetry call, want true")
	}
}

func TestWithRetry_SuccessOnSecondAttempt(t *testing.T) {
	breaker := NewCircuitBreaker()
	host := "s3.example.com"

	var (
		calls                             int
		consecutiveFailuresBeforeAttempt2 = -1
	)

	err := WithRetry(context.Background(), breaker, fastPolicy, host, func(_ context.Context, isRetry bool) error {
		calls++

		if calls == 1 {
			if isRetry {
				t.Error("isRetry = true on the first attempt, want false")
			}

			return networkErr()
		}

		if !isRetry {
			t.Error("isRetry = false on the second attempt, want true")
		}

		// Snapshot the breaker's internal per-host failure counter right
		// before this (successful) attempt runs. Per WithRetry's contract,
		// RecordFailure must only be called once the whole operation is
		// exhausted (all attempts failed) or has hit a non-retryable error -
		// never just because one attempt with retries remaining failed. If
		// that contract were violated, the first attempt's failure would
		// already be reflected here as consecutiveFailures == 1.
		breaker.mu.Lock()
		if state, ok := breaker.hosts[host]; ok {
			consecutiveFailuresBeforeAttempt2 = state.consecutiveFailures
		} else {
			consecutiveFailuresBeforeAttempt2 = 0
		}
		breaker.mu.Unlock()

		return nil
	})
	if err != nil {
		t.Fatalf("WithRetry() error = %v, want nil", err)
	}

	if calls != 2 {
		t.Fatalf("attempt called %d times, want 2", calls)
	}

	if consecutiveFailuresBeforeAttempt2 != 0 {
		t.Fatalf("breaker consecutiveFailures = %d immediately before the retry attempt, want 0 "+
			"(RecordFailure must not be called for an individual failed attempt while retries remain)",
			consecutiveFailuresBeforeAttempt2)
	}

	if !breaker.Allow(host) {
		t.Fatal("Allow() = false after a WithRetry call that ultimately succeeded, want true")
	}
}

func TestWithRetry_AllAttemptsFailed(t *testing.T) {
	breaker := NewCircuitBreaker()
	host := "s3.example.com"

	var calls int

	err := WithRetry(context.Background(), breaker, fastPolicy, host, func(_ context.Context, _ bool) error {
		calls++
		return networkErr()
	})
	if err == nil {
		t.Fatal("WithRetry() error = nil, want an error (all attempts exhausted)")
	}

	if calls != fastPolicy.MaxAttempts {
		t.Fatalf("attempt called %d times, want %d (policy.MaxAttempts)", calls, fastPolicy.MaxAttempts)
	}

	// The whole point of the "RecordFailure once per operation" contract:
	// one fully-exhausted WithRetry call (5 failed attempts) must record
	// exactly ONE failure against the breaker, not 5 - otherwise a single
	// operation's internal retry churn could trip the breaker on its own,
	// rather than requiring maxConsecutiveFailures independent operations
	// to fail.
	breaker.mu.Lock()
	got := breaker.hosts[host].consecutiveFailures
	breaker.mu.Unlock()

	if got != 1 {
		t.Fatalf("breaker consecutiveFailures = %d after one exhausted WithRetry call (%d failed attempts), want 1 "+
			"(RecordFailure must be called once per operation, not once per attempt)", got, fastPolicy.MaxAttempts)
	}

	if !breaker.Allow(host) {
		t.Fatal("Allow() = false after only one exhausted operation, want true (breaker opens only after " +
			"maxConsecutiveFailures separate operations, not attempts within one)")
	}
}

// TestWithRetry_RecordFailureAccumulatesAcrossOperations confirms the
// counterpart to TestWithRetry_AllAttemptsFailed: it does take
// maxConsecutiveFailures separate, fully-exhausted WithRetry operations
// (not attempts) to trip the breaker to Open.
func TestWithRetry_RecordFailureAccumulatesAcrossOperations(t *testing.T) {
	breaker := NewCircuitBreaker()
	host := "s3.example.com"

	failingAttempt := func(_ context.Context, _ bool) error { return networkErr() }

	for i := 0; i < maxConsecutiveFailures-1; i++ {
		if err := WithRetry(context.Background(), breaker, fastPolicy, host, failingAttempt); err == nil {
			t.Fatalf("operation %d: WithRetry() error = nil, want an error", i)
		}

		if !breaker.Allow(host) {
			t.Fatalf("Allow() = false after only %d exhausted operations, want true", i+1)
		}
	}

	if err := WithRetry(context.Background(), breaker, fastPolicy, host, failingAttempt); err == nil {
		t.Fatal("final operation: WithRetry() error = nil, want an error")
	}

	if breaker.Allow(host) {
		t.Fatalf("Allow() = true after %d exhausted operations, want false (Open)", maxConsecutiveFailures)
	}
}

func TestWithRetry_NonRetryableErrorStopsImmediately(t *testing.T) {
	breaker := NewCircuitBreaker()
	host := "s3.example.com"

	authErr := &smithy.GenericAPIError{Code: "AccessDenied", Message: "denied"}

	var calls int

	err := WithRetry(context.Background(), breaker, PartRetryPolicy, host, func(_ context.Context, _ bool) error {
		calls++
		return authErr
	})
	if !errors.Is(err, authErr) {
		t.Fatalf("WithRetry() error = %v, want the original auth error returned unwrapped", err)
	}

	if calls != 1 {
		t.Fatalf("attempt called %d times, want 1 (non-retryable error must stop immediately)", calls)
	}
}

func TestWithRetry_ContextCanceledIsNotRetried(t *testing.T) {
	breaker := NewCircuitBreaker()
	host := "s3.example.com"

	var calls int

	err := WithRetry(context.Background(), breaker, PartRetryPolicy, host, func(_ context.Context, _ bool) error {
		calls++
		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WithRetry() error = %v, want context.Canceled", err)
	}

	if calls != 1 {
		t.Fatalf("attempt called %d times, want 1 (cancellation must not be retried)", calls)
	}

	if !breaker.Allow(host) {
		t.Fatal("Allow() = false after a canceled WithRetry call, want true (cancellation must not count against the breaker)")
	}
}

func TestWithRetry_CircuitOpenSkipsAttemptEntirely(t *testing.T) {
	breaker := NewCircuitBreaker()
	host := "s3.example.com"

	for i := 0; i < maxConsecutiveFailures; i++ {
		breaker.RecordFailure(host, "network")
	}

	var calls int

	err := WithRetry(context.Background(), breaker, PartRetryPolicy, host, func(_ context.Context, _ bool) error {
		calls++
		return nil
	})
	if err == nil {
		t.Fatal("WithRetry() error = nil, want an error (circuit breaker open)")
	}

	if !errors.Is(err, errCircuitOpen) {
		t.Fatalf("WithRetry() error = %v, want it to wrap errCircuitOpen", err)
	}

	if calls != 0 {
		t.Fatalf("attempt called %d times, want 0 (breaker must short-circuit before any network call)", calls)
	}
}

func TestWithRetry_ContextCanceledDuringBackoff(t *testing.T) {
	breaker := NewCircuitBreaker()
	host := "s3.example.com"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// A deliberately long backoff: if cancellation during the pause were
	// not honored promptly, the test would have to wait out this whole
	// window before observing a result.
	policy := RetryPolicy{MaxAttempts: 3, BaseDelay: 10 * time.Second, MaxDelay: 10 * time.Second}

	var calls int

	done := make(chan error, 1)

	go func() {
		done <- WithRetry(ctx, breaker, policy, host, func(_ context.Context, _ bool) error {
			calls++

			// Cancel right after the first attempt fails, so the backoff
			// pause WithRetry is about to take before attempt 2 gets
			// interrupted partway through.
			if calls == 1 {
				cancel()
			}

			return networkErr()
		})
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("WithRetry() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WithRetry() did not return promptly after context cancellation during a backoff pause")
	}

	if calls != 1 {
		t.Fatalf("attempt called %d times, want 1 (no further attempt after cancellation during backoff)", calls)
	}
}

func TestAdaptiveTimeout(t *testing.T) {
	tests := []struct {
		name     string
		partSize int64
		speed    float64
		want     time.Duration
	}{
		{
			name:     "tiny part hits the 30s floor",
			partSize: 1024,
			speed:    10 * 1024 * 1024,
			want:     30 * time.Second,
		},
		{
			name:     "zero speed falls back to the 1MB/s default",
			partSize: 20 * 1024 * 1024,
			speed:    0,
			want:     40 * time.Second, // 2 * 20MiB / 1MiB/s
		},
		{
			name:     "negative speed falls back to the 1MB/s default",
			partSize: 20 * 1024 * 1024,
			speed:    -5,
			want:     40 * time.Second,
		},
		{
			name:     "large part at low speed scales proportionally",
			partSize: 128 * 1024 * 1024,
			speed:    1024 * 1024,
			want:     256 * time.Second, // 2 * 128MiB / 1MiB/s
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AdaptiveTimeout(tt.partSize, tt.speed, minAdaptiveTimeout)
			if got != tt.want {
				t.Fatalf("AdaptiveTimeout(%d, %v, %v) = %v, want %v", tt.partSize, tt.speed, minAdaptiveTimeout, got, tt.want)
			}
		})
	}
}

func TestAdaptiveTimeout_CustomFloor(t *testing.T) {
	// A floor other than minAdaptiveTimeout is honored verbatim, both when
	// it exceeds the computed value (the floor wins) and when it doesn't
	// (the computed value wins) - exercising the floor parameter itself,
	// independent of RetryPolicyStore.
	const customFloor = 5 * time.Second

	if got := AdaptiveTimeout(1024, 10*1024*1024, customFloor); got != customFloor {
		t.Fatalf("AdaptiveTimeout() = %v, want the custom floor %v", got, customFloor)
	}

	// 2 * 128MiB / 1MiB/s = 256s, comfortably above a 10s floor - the
	// computed value must win here, not the floor.
	const smallFloor = 10 * time.Second
	const wantComputed = 256 * time.Second
	if got := AdaptiveTimeout(128*1024*1024, 1024*1024, smallFloor); got != wantComputed {
		t.Fatalf("AdaptiveTimeout() = %v, want the computed value %v (above the floor %v)", got, wantComputed, smallFloor)
	}
}

func TestNewRetryPolicyStore_Defaults(t *testing.T) {
	s := NewRetryPolicyStore()

	if got := s.Part(); got != PartRetryPolicy {
		t.Fatalf("Part() = %+v, want PartRetryPolicy %+v", got, PartRetryPolicy)
	}

	if got := s.Metadata(); got != MetadataRetryPolicy {
		t.Fatalf("Metadata() = %+v, want MetadataRetryPolicy %+v", got, MetadataRetryPolicy)
	}

	if got := s.TimeoutFloor(); got != minAdaptiveTimeout {
		t.Fatalf("TimeoutFloor() = %v, want minAdaptiveTimeout %v", got, minAdaptiveTimeout)
	}
}

func TestRetryPolicyStore_SetThenRead(t *testing.T) {
	s := NewRetryPolicyStore()

	newPart := RetryPolicy{MaxAttempts: 7, BaseDelay: time.Second, MaxDelay: 10 * time.Second}
	newMetadata := RetryPolicy{MaxAttempts: 2, BaseDelay: time.Second, MaxDelay: 5 * time.Second}
	newFloor := 45 * time.Second

	s.Set(newPart, newMetadata, newFloor)

	if got := s.Part(); got != newPart {
		t.Fatalf("Part() after Set() = %+v, want %+v", got, newPart)
	}

	if got := s.Metadata(); got != newMetadata {
		t.Fatalf("Metadata() after Set() = %+v, want %+v", got, newMetadata)
	}

	if got := s.TimeoutFloor(); got != newFloor {
		t.Fatalf("TimeoutFloor() after Set() = %v, want %v", got, newFloor)
	}
}

// TestRetryPolicyStore_ConcurrentAccess mirrors
// crypto.TestKeyBoxConcurrentAccess/CircuitBreaker_TestConcurrentAccess: one
// goroutine calls Set in a loop while others call Part/Metadata/TimeoutFloor
// in a loop, asserting only that concurrent access is race-free (run under
// `go test -race`) and never panics - not any particular observed value,
// since Set/the readers race by design.
func TestRetryPolicyStore_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	s := NewRetryPolicyStore()

	const goroutines = 50
	const iterations = 200

	var wg sync.WaitGroup

	for g := 0; g < goroutines; g++ {
		wg.Add(1)

		go func(seed int) {
			defer wg.Done()

			for i := 0; i < iterations; i++ {
				switch (seed + i) % 4 {
				case 0:
					s.Set(
						RetryPolicy{MaxAttempts: seed%5 + 1, BaseDelay: time.Duration(seed) * time.Millisecond, MaxDelay: time.Second},
						RetryPolicy{MaxAttempts: seed%3 + 1, BaseDelay: time.Duration(seed) * time.Millisecond, MaxDelay: time.Second},
						time.Duration(seed)*time.Second,
					)
				case 1:
					_ = s.Part()
				case 2:
					_ = s.Metadata()
				case 3:
					_ = s.TimeoutFloor()
				}
			}
		}(g)
	}

	wg.Wait()
}
