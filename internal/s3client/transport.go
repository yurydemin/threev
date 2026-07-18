package s3client

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/proxy"

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

// applyProxy configures t.Proxy (HTTP/HTTPS CONNECT) or t.DialContext
// (SOCKS5, via golang.org/x/net/proxy - net/http's Transport.Proxy field
// only understands HTTP(S) CONNECT proxies, not SOCKS5) from p.ProxyURL.
// An empty ProxyURL is a no-op. Returns a descriptive error for an
// unparsable URL or unsupported scheme, rather than letting a malformed
// value surface later as an opaque dial failure.
func applyProxy(t *http.Transport, p domain.Profile) error {
	if p.ProxyURL == "" {
		return nil
	}

	u, err := url.Parse(p.ProxyURL)
	if err != nil {
		return fmt.Errorf("parse proxy URL: %w", err)
	}

	switch u.Scheme {
	case "http", "https":
		t.Proxy = http.ProxyURL(u)
		return nil
	case "socks5", "socks5h":
		var auth *proxy.Auth
		if u.User != nil {
			password, _ := u.User.Password()
			auth = &proxy.Auth{User: u.User.Username(), Password: password}
		}

		dialer, err := proxy.SOCKS5("tcp", u.Host, auth, newDialer())
		if err != nil {
			return fmt.Errorf("build SOCKS5 dialer: %w", err)
		}

		ctxDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return fmt.Errorf("SOCKS5 dialer does not support context (unexpected)")
		}

		t.DialContext = ctxDialer.DialContext
		return nil
	default:
		return fmt.Errorf("unsupported proxy scheme %q (expected http, https, or socks5)", u.Scheme)
	}
}

// newPooledTransport returns the long-lived, connection-reusing
// *http.Transport used for the first attempt of every request against
// profile p (docs/02-tech-spec.md section 10.1): idle connections are kept
// around (MaxIdleConns/MaxIdleConnsPerHost/IdleConnTimeout) so repeated
// calls against the same profile reuse existing TCP+TLS connections instead
// of paying handshake cost every time.
func newPooledTransport(p domain.Profile) (*http.Transport, error) {
	t := &http.Transport{
		DialContext:           newDialer().DialContext,
		TLSClientConfig:       tlsConfigFor(p),
		MaxIdleConns:          maxIdleConns,
		MaxIdleConnsPerHost:   maxIdleConnsPerHost,
		IdleConnTimeout:       idleConnTimeout,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ExpectContinueTimeout: expectContinueTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout,
	}

	if err := applyProxy(t, p); err != nil {
		return nil, err
	}

	return t, nil
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
func newFreshTransport(p domain.Profile) (*http.Transport, error) {
	t, err := newPooledTransport(p)
	if err != nil {
		return nil, err
	}

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
	return t, nil
}
