package s3client

import (
	"context"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"net/http"
	"time"

	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// RetryPolicy describes one retry strategy: how many attempts to make and
// the exponential-backoff parameters between them
// (docs/02-tech-spec.md section 10.4). There are two policies in use: parts
// of a multipart upload/range download get more attempts than a plain
// metadata call (HeadObject, ListParts, ...), since a part is the unit of
// work most exposed to a flaky link and cheapest to simply try again.
type RetryPolicy struct {
	// MaxAttempts is the total number of attempts made, including the
	// first (non-retry) one - e.g. 5 means "try once, then retry up to 4
	// more times".
	MaxAttempts int
	// BaseDelay is the backoff cap used for the first retry pause;
	// subsequent pauses double it, up to MaxDelay.
	BaseDelay time.Duration
	// MaxDelay caps the backoff pause, however many attempts have already
	// been made.
	MaxDelay time.Duration
}

// PartRetryPolicy is used for multipart-upload part uploads and
// range-download segment reads: the highest-volume, most failure-exposed
// operations, so they get the most attempts (docs/02-tech-spec.md section
// 10.4: "Максимум retries: 5 для частей").
var PartRetryPolicy = RetryPolicy{MaxAttempts: 5, BaseDelay: 2 * time.Second, MaxDelay: 32 * time.Second}

// MetadataRetryPolicy is used for metadata-only calls (HeadObject,
// ListParts, CreateMultipartUpload, CompleteMultipartUpload, ...), which
// get fewer attempts than a part transfer (docs/02-tech-spec.md section
// 10.4: "3 для metadata-операций").
var MetadataRetryPolicy = RetryPolicy{MaxAttempts: 3, BaseDelay: 2 * time.Second, MaxDelay: 32 * time.Second}

// errCircuitOpen is wrapped into the error WithRetry returns when the
// circuit breaker refuses a host outright, so callers can recognize this
// specific case (e.g. errors.Is) if they ever need to distinguish it from
// an ordinary exhausted-retries failure.
var errCircuitOpen = errors.New("host temporarily unavailable (circuit breaker open)")

// WithRetry runs attempt, retrying it up to policy.MaxAttempts times against
// host, coordinating with breaker per the architecture decision recorded in
// docs/02-tech-spec.md section 10.4 and the Stage 3 review with
// network-engineer:
//
//   - breaker.Allow(host) is checked before every single attempt (cheap, so
//     checking it on every iteration is fine). If it returns false, WithRetry
//     returns immediately without calling attempt and without touching the
//     breaker's failure count - an Open breaker is already-known state, not
//     a new failure.
//   - breaker.RecordSuccess(host) is called as soon as any attempt succeeds,
//     however many prior attempts failed.
//   - breaker.RecordFailure(host, category) is called at most ONCE per
//     WithRetry call: only once the whole operation has definitively
//     failed, either because the error is not retryable or because
//     policy.MaxAttempts has been exhausted. It is deliberately NOT called
//     after each individual failed attempt - doing so would let a single
//     1-2s network blip that happens to hit several parallel multipart
//     workers at once trip the breaker and lock out a host (e.g. a
//     self-hosted MinIO endpoint, this product's key flaky-network
//     scenario) for 60s over what was really a transient hiccup. The
//     breaker exists to catch sustained unavailability observed across many
//     independent operations, not a single operation's internal retry
//     churn.
//
// attempt receives ctx (which WithRetry never wraps or cancels itself - a
// caller that wants a per-attempt timeout should derive its own
// context.WithTimeout, e.g. using AdaptiveTimeout, inside attempt) and
// isRetry (false for the first attempt, true for every attempt after that).
// WithRetry does not choose between the pooled/fresh S3 client itself - that
// decision belongs to the caller (multipart_upload.go/range_download.go),
// which is expected to use isRetry to pick the fresh, no-keep-alive client
// on retries per docs/02-tech-spec.md section 10.4 ("новое TCP-соединение
// + свежий DNS lookup").
//
// context.Canceled (a Pause/Cancel) is never retried and never recorded
// against the breaker in either direction: it is returned immediately,
// since it reflects a user action, not a host failure.
func WithRetry(ctx context.Context, breaker *CircuitBreaker, policy RetryPolicy, host string, attempt func(ctx context.Context, isRetry bool) error) error {
	var throttled bool

	for attemptNum := 1; attemptNum <= policy.MaxAttempts; attemptNum++ {
		if !breaker.Allow(host) {
			return fmt.Errorf("%s: %w", host, errCircuitOpen)
		}

		if attemptNum > 1 {
			if err := sleepBackoff(ctx, backoffDelay(policy, attemptNum-1, throttled)); err != nil {
				return err
			}
		}

		err := attempt(ctx, attemptNum > 1)
		if err == nil {
			breaker.RecordSuccess(host)
			return nil
		}

		category, _ := ClassifyError(err)

		// A user-initiated abort is not a host failure - return it as-is,
		// with no further attempts and no breaker bookkeeping either way.
		if category == "cancelled" {
			return err
		}

		throttled = isThrottlingError(err)

		if !isRetryable(category, err) || attemptNum == policy.MaxAttempts {
			breaker.RecordFailure(host, category)
			return err
		}

		// Retryable and attempts remain: loop back around to Allow ->
		// backoff -> attempt, without recording a failure yet (see the doc
		// comment above).
	}

	// Unreachable for any policy with MaxAttempts >= 1 (both PartRetryPolicy
	// and MetadataRetryPolicy are); kept only so the compiler is satisfied
	// this function always returns.
	return fmt.Errorf("s3client: WithRetry called with a non-positive MaxAttempts (%d)", policy.MaxAttempts)
}

// isRetryable reports whether an error classified as category by
// ClassifyError is worth retrying at all.
//
//   - "network"/"timeout": always retried - exactly the transient,
//     connectivity-level failures retry exists for.
//   - "cancelled": never retried, though WithRetry already special-cases
//     this before calling isRetryable; the case is kept here too so this
//     function is correct in isolation.
//   - "auth"/"tls": never retried - bad credentials or an untrusted
//     certificate will not fix itself on the next attempt.
//   - "unknown": retried. ClassifyError falls back to "unknown" both for
//     genuinely unrecognized errors and for any S3 API error whose code
//     isn't specifically an auth code (see errors.go) - which includes
//     throttling responses (SlowDown, RequestTimeout, ...) that absolutely
//     should be retried (see isThrottlingError, used to lengthen the
//     backoff for these specifically). Treating "unknown" as retryable is a
//     deliberate, conservative choice for this product: an extra retry on a
//     genuinely unretryable "unknown" error costs a few seconds, while
//     refusing to retry a transient-but-unrecognized failure could turn a
//     recoverable blip into a failed multi-gigabyte transfer - the
//     asymmetry favors retrying.
func isRetryable(category string, _ error) bool {
	switch category {
	case "network", "timeout", "unknown":
		return true
	case "cancelled", "auth", "tls":
		return false
	default:
		return false
	}
}

// throttlingErrorCodes are the S3/AWS API error codes that indicate the
// service is asking the client to slow down, as opposed to some other
// unrecognized API failure. ClassifyError has no dedicated "throttling"
// category (see isRetryable) - these all fall under "unknown" there - so
// this check exists purely to lengthen the backoff for them specifically,
// per the network-engineer review: retrying a throttled request on the same
// short backoff as a plain network blip just adds to the load that
// triggered the throttling in the first place.
var throttlingErrorCodes = map[string]bool{
	"SlowDown":                 true,
	"SlowDownException":        true,
	"RequestTimeout":           true,
	"ThrottlingException":      true,
	"TooManyRequestsException": true,
}

// isThrottlingError reports whether err (at any depth) is an S3/AWS
// throttling response: either a recognized API error code (see
// throttlingErrorCodes) or, as a fallback for providers that don't surface
// one of those specific codes, an HTTP 429 or 503 status.
func isThrottlingError(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && throttlingErrorCodes[apiErr.ErrorCode()] {
		return true
	}

	var respErr *smithyhttp.ResponseError
	if errors.As(err, &respErr) {
		switch respErr.HTTPStatusCode() {
		case http.StatusTooManyRequests, http.StatusServiceUnavailable:
			return true
		}
	}

	return false
}

// backoffDelay returns the full-jitter backoff duration to wait before
// making a retry attempt, given how many pauses have already happened
// (pauseNumber, 1 for the pause before the second overall attempt, 2 for
// the pause before the third, and so on) - matching the exponential
// sequence documented in docs/02-tech-spec.md section 10.4 (2s, 4s, 8s,
// 16s, ... capped at policy.MaxDelay). If throttled is true (the failure
// that led to this pause was an S3 throttling response, see
// isThrottlingError), the cap for this pause is doubled: a plain
// connectivity blip warrants the standard schedule, but a service telling
// us explicitly to slow down warrants backing off harder before the next
// attempt.
func backoffDelay(policy RetryPolicy, pauseNumber int, throttled bool) time.Duration {
	capDelay := policy.BaseDelay
	if pauseNumber > 1 {
		capDelay = policy.BaseDelay * time.Duration(int64(1)<<uint(pauseNumber-1)) //nolint:gosec // pauseNumber is bounded by policy.MaxAttempts (<=5), no overflow risk
	}

	if throttled {
		capDelay *= 2
	}

	if capDelay > policy.MaxDelay {
		capDelay = policy.MaxDelay
	}

	if capDelay <= 0 {
		return 0
	}

	//nolint:gosec // jitter for retry backoff timing, not a security-sensitive use of randomness
	return time.Duration(mrand.Int64N(int64(capDelay) + 1))
}

// sleepBackoff waits for d, or returns ctx's error immediately if ctx is
// canceled first - so a Pause/Cancel during a backoff pause between retry
// attempts is reacted to instantly rather than after the pause completes.
func sleepBackoff(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// defaultAssumedSpeedBytesPerSec is the conservative channel-speed estimate
// AdaptiveTimeout falls back to when the caller has no real sample yet (a
// task's first part/segment, before progress.go - a later Stage 3 step -
// has accumulated any throughput measurements for it).
const defaultAssumedSpeedBytesPerSec = 1024 * 1024 // 1 MB/s

// minAdaptiveTimeout is the floor AdaptiveTimeout never goes below,
// regardless of how small partSize or how fast assumedSpeedBytesPerSec is -
// docs/02-tech-spec.md section 10.4 sets this to guard against a spuriously
// tight timeout on a tiny part racing ahead of normal request/response
// overhead (TLS handshake, S3 processing time, ...) that has nothing to do
// with transfer throughput.
const minAdaptiveTimeout = 30 * time.Second

// AdaptiveTimeout returns a per-attempt timeout for transferring partSize
// bytes, given assumedSpeedBytesPerSec as the current estimate of channel
// throughput (docs/02-tech-spec.md section 10.4: "max(30s, partSize /
// currentSpeed * 2)"). If assumedSpeedBytesPerSec is zero or negative (no
// usable estimate yet, or a caller passing a not-yet-measured zero value),
// defaultAssumedSpeedBytesPerSec is used instead, so this function never
// divides by zero or returns a nonsensical (negative/huge) duration.
//
// AdaptiveTimeout is a pure function: it knows nothing about the circuit
// breaker or retry loop above. The caller (multipart_upload.go/
// range_download.go, later Stage 3 steps) is expected to wrap the single
// HTTP call inside its WithRetry attempt closure with
// context.WithTimeout(ctx, AdaptiveTimeout(...)).
func AdaptiveTimeout(partSize int64, assumedSpeedBytesPerSec float64) time.Duration {
	if assumedSpeedBytesPerSec <= 0 {
		assumedSpeedBytesPerSec = defaultAssumedSpeedBytesPerSec
	}

	seconds := 2 * float64(partSize) / assumedSpeedBytesPerSec
	computed := time.Duration(seconds * float64(time.Second))

	if computed < minAdaptiveTimeout {
		return minAdaptiveTimeout
	}

	return computed
}
