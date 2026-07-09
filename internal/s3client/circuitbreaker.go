package s3client

import (
	"sync"
	"time"
)

// maxConsecutiveFailures is the number of consecutive network/timeout
// failures against one host that trips the breaker to Open
// (docs/02-tech-spec.md section 10.4: "5 consecutive errors on one IP").
const maxConsecutiveFailures = 5

// openDuration is how long a host stays Open before a single trial request
// is allowed through (transitioning it to HalfOpen).
const openDuration = 60 * time.Second

// breakerStatus is the state of one host's circuit, following the standard
// Closed/Open/HalfOpen circuit-breaker state machine.
type breakerStatus int

const (
	// breakerClosed is the default state: requests are allowed, failures are
	// being counted.
	breakerClosed breakerStatus = iota
	// breakerOpen means the host is considered unavailable; requests are
	// rejected without attempting the network call until openDuration has
	// elapsed since openedAt.
	breakerOpen
	// breakerHalfOpen means openDuration has elapsed and exactly one trial
	// request is being allowed through to probe whether the host has
	// recovered.
	breakerHalfOpen
)

// hostState is the per-host circuit state tracked by CircuitBreaker.
type hostState struct {
	status              breakerStatus
	consecutiveFailures int
	openedAt            time.Time

	// probing is true while the single HalfOpen trial request is in
	// flight, i.e. between the Allow call that granted it and the matching
	// RecordSuccess/RecordFailure call. It exists purely to enforce "at
	// most one trial request at a time" - see the Allow doc comment for why
	// this needs explicit tracking rather than relying on breakerHalfOpen
	// alone.
	probing bool
}

// CircuitBreaker tracks, per host, whether recent network/timeout failures
// mean requests to that host should be short-circuited (failed immediately,
// without a network call) for a cooldown period. This is the simplified,
// per-HOST (not per-IP) breaker fixed with the user for MVP
// (docs/02-tech-spec.md section 10.4): a full DNS round-robin
// implementation with per-IP tracking is out of scope, since we do not
// cache/resolve individual IPs ourselves (see transport.go/manager.go -
// "fresh" retry connections get a new DNS lookup via net.Dialer, but we
// never see or key on the resolved IP).
//
// CircuitBreaker is intended to be layered underneath the future retry.go:
// a caller checks Allow(host) before attempting a request (immediately
// failing with a "host temporarily unavailable" error if it returns false),
// then reports the outcome via RecordSuccess/RecordFailure so later calls
// can make an informed decision.
//
// host is expected to be a bare hostname (e.g. from
// url.Parse(profile.EndpointURL).Hostname()), not a host:port pair or full
// URL - CircuitBreaker itself does not parse or normalize it.
type CircuitBreaker struct {
	mu    sync.Mutex
	hosts map[string]*hostState

	// nowFunc returns the current time and defaults to time.Now. It exists
	// solely so tests can simulate the passage of openDuration without an
	// actual 60s sleep; NewCircuitBreaker always sets it, so it is never nil
	// on a properly constructed CircuitBreaker.
	nowFunc func() time.Time
}

// NewCircuitBreaker returns a CircuitBreaker with no hosts recorded yet
// (every host starts out implicitly Closed - see Allow).
func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{
		hosts:   make(map[string]*hostState),
		nowFunc: time.Now,
	}
}

// Allow reports whether a request to host may currently be attempted.
//
//   - If host has never been recorded (or is Closed), Allow returns true.
//   - If host is Open and less than openDuration has elapsed since it
//     opened, Allow returns false: the caller should fail the request
//     immediately (e.g. "host temporarily unavailable") without making a
//     network call.
//   - If host is Open and openDuration has elapsed, Allow transitions it to
//     HalfOpen and returns true, granting exactly one trial request.
//   - If host is already HalfOpen, Allow returns true only if no trial
//     request is currently in flight; otherwise it returns false. Without
//     this check, every goroutine that happened to call Allow while the
//     host was HalfOpen would get true and fire its own "trial" request in
//     parallel, defeating the point of probing with a single request before
//     reopening the floodgates. Guarding it is a single extra bool field
//     (hostState.probing) checked under the same mutex Allow already holds
//     for the state transition itself, so there is no meaningful extra cost
//     or complexity to doing this properly rather than documenting it as a
//     known race.
//
// Allow is safe for concurrent use by multiple goroutines.
func (b *CircuitBreaker) Allow(host string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	state, ok := b.hosts[host]
	if !ok {
		return true
	}

	switch state.status {
	case breakerClosed:
		return true

	case breakerHalfOpen:
		if state.probing {
			return false
		}

		state.probing = true

		return true

	case breakerOpen:
		if b.nowFunc().Sub(state.openedAt) < openDuration {
			return false
		}

		// openDuration has elapsed. Transition to HalfOpen and grant this
		// caller the single trial request now, while still holding mu, so
		// no concurrently-blocked Allow call can observe the stale Open
		// state and independently reach the same conclusion.
		state.status = breakerHalfOpen
		state.probing = true

		return true

	default:
		return true
	}
}

// RecordSuccess reports that a request to host succeeded. It resets
// consecutiveFailures to 0 and moves host to Closed, whatever its previous
// state - including HalfOpen, where a successful trial request is exactly
// what signals the host has recovered.
//
// RecordSuccess is safe for concurrent use by multiple goroutines.
func (b *CircuitBreaker) RecordSuccess(host string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	state, ok := b.hosts[host]
	if !ok {
		// Nothing recorded for this host yet, so there is no state to
		// reset; it is already implicitly Closed (see Allow).
		return
	}

	state.status = breakerClosed
	state.consecutiveFailures = 0
	state.probing = false
}

// RecordFailure reports that a request to host failed, classified as
// category by ClassifyError. Only "network" and "timeout" failures count
// toward tripping the breaker:
//
//   - "auth" is excluded because bad credentials on one profile must not
//     lock other profiles out of the same host.
//   - "tls" is excluded because a certificate error does not mean the host
//     itself is unreachable.
//   - "cancelled" is excluded because a user-initiated abort (pause,
//     closing a dialog) is not a host failure.
//
// Any other category is ignored (not counted) by the same logic - only
// "network"/"timeout" are ever counted, everything else is a no-op call.
//
// When host was HalfOpen (a trial request just failed), the breaker goes
// straight back to Open with a new 60s window (openedAt reset to now);
// consecutiveFailures is not reset, since it was already at or above
// maxConsecutiveFailures when the host first opened. Otherwise, if
// incrementing consecutiveFailures reaches maxConsecutiveFailures, the
// breaker trips to Open with openedAt set to now.
//
// RecordFailure is safe for concurrent use by multiple goroutines.
func (b *CircuitBreaker) RecordFailure(host string, category string) {
	if !countsTowardBreaker(category) {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	state, ok := b.hosts[host]
	if !ok {
		state = &hostState{}
		b.hosts[host] = state
	}

	wasHalfOpen := state.status == breakerHalfOpen

	state.consecutiveFailures++
	state.probing = false

	switch {
	case wasHalfOpen:
		state.status = breakerOpen
		state.openedAt = b.nowFunc()

	case state.consecutiveFailures >= maxConsecutiveFailures:
		state.status = breakerOpen
		state.openedAt = b.nowFunc()
	}
}

// countsTowardBreaker reports whether category (as returned by
// ClassifyError) should increment a host's consecutiveFailures count. Only
// connectivity-related failures do - see the RecordFailure doc comment.
func countsTowardBreaker(category string) bool {
	return category == "network" || category == "timeout"
}
