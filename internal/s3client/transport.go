package s3client

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"threev/internal/domain"
)

// dialTimeout bounds how long the underlying TCP connect (and, per
// docs/02-tech-spec.md section 10.4, the "connection timeout") is allowed to
// take for every transport built by this package.
const dialTimeout = 10 * time.Second

// dialKeepAlive is the TCP keep-alive interval applied to connections
// established by every transport built by this package.
const dialKeepAlive = 30 * time.Second

// Fixed http.Transport pool/timeout settings from docs/02-tech-spec.md
// section 10.1. ResponseHeaderTimeout here is the plain, fixed baseline
// mentioned in that section ("30s (adaptive, see 10.3)"); the adaptive
// per-attempt timeout strategy itself lives in the future retry.go, layered
// on top of these transports rather than replacing them.
const (
	maxIdleConns          = 100
	maxIdleConnsPerHost   = 32
	idleConnTimeout       = 90 * time.Second
	tlsHandshakeTimeout   = 10 * time.Second
	expectContinueTimeout = 1 * time.Second
	responseHeaderTimeout = 30 * time.Second
)

// newDialer returns the *net.Dialer shared by every transport this package
// builds, applying the connection timeout and keep-alive interval from
// docs/02-tech-spec.md section 10.4.
func newDialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: dialKeepAlive,
	}
}

// tlsConfigFor builds the *tls.Config for profile p. When p.VerifySSL is
// false, certificate verification is disabled on this transport only, and
// only because the user explicitly requested it for this specific profile -
// never globally - per SEC-004 (docs/02-tech-spec.md section 11). This
// mirrors the behavior previously implemented directly in newHTTPClient.
func tlsConfigFor(p domain.Profile) *tls.Config {
	if p.VerifySSL {
		return nil
	}

	return &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit, per-profile opt-out required by SEC-004; never a global default
}

// newPooledTransport returns the long-lived, connection-reusing
// *http.Transport used for the first attempt of every request against
// profile p (docs/02-tech-spec.md section 10.1): idle connections are kept
// around (MaxIdleConns/MaxIdleConnsPerHost/IdleConnTimeout) so repeated
// calls against the same profile reuse existing TCP+TLS connections instead
// of paying handshake cost every time.
func newPooledTransport(p domain.Profile) *http.Transport {
	return &http.Transport{
		DialContext:           newDialer().DialContext,
		TLSClientConfig:       tlsConfigFor(p),
		MaxIdleConns:          maxIdleConns,
		MaxIdleConnsPerHost:   maxIdleConnsPerHost,
		IdleConnTimeout:       idleConnTimeout,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ExpectContinueTimeout: expectContinueTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout,
	}
}

// newFreshTransport returns an *http.Transport configured identically to
// newPooledTransport except that keep-alives are disabled
// (docs/02-tech-spec.md section 10.4: "at retry: a new TCP connection (not
// from the pool) + a fresh DNS lookup"). Every request sent through a
// transport built by this function forces a brand new TCP connection (and
// therefore a fresh DNS resolution), so it is intended to be used only for
// retry attempts - never the first attempt - which is why the retry layer
// (retry.go, a later step) holds one dedicated *s3.Client built on top of
// this transport per profile, separate from the pooled one.
func newFreshTransport(p domain.Profile) *http.Transport {
	t := newPooledTransport(p)
	t.DisableKeepAlives = true

	// MaxIdleConns/MaxIdleConnsPerHost/IdleConnTimeout, inherited from
	// newPooledTransport above, have no effect once DisableKeepAlives is
	// true: an idle pool is never populated when connections aren't kept
	// alive after use. Left as-is (rather than zeroed) purely so this
	// transport's fields read identically to the pooled one at a glance;
	// they are dead configuration here, not a bug.
	//
	// DisableKeepAlives also has a side effect worth knowing: Go's net/http
	// skips HTTP/2 auto-upgrade whenever it is true, so every request
	// through this transport is forced onto HTTP/1.1 with exactly one
	// request per connection - reinforcing, not weakening, the "new TCP
	// connection per attempt" guarantee this transport exists for.
	return t
}
