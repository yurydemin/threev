package s3client

import (
	"net/http"
	"strings"
	"testing"

	"threev/internal/domain"
)

// TestApplyProxyEmptyIsNoop verifies that an empty ProxyURL leaves the
// transport's Proxy/DialContext untouched (applyProxy's documented no-op
// case), rather than falling back to any environment-derived default.
func TestApplyProxyEmptyIsNoop(t *testing.T) {
	t.Parallel()

	tr := &http.Transport{}

	if err := applyProxy(tr, domain.Profile{}); err != nil {
		t.Fatalf("applyProxy() returned error: %v", err)
	}

	if tr.Proxy != nil {
		t.Error("applyProxy() with empty ProxyURL set Transport.Proxy, want nil")
	}
	if tr.DialContext != nil {
		t.Error("applyProxy() with empty ProxyURL set Transport.DialContext, want nil")
	}
}

// TestApplyProxyHTTPSetsProxyFunc verifies that an "http://" ProxyURL
// configures Transport.Proxy (HTTP CONNECT proxying) and leaves DialContext
// untouched.
func TestApplyProxyHTTPSetsProxyFunc(t *testing.T) {
	t.Parallel()

	tr := &http.Transport{}
	p := domain.Profile{ProxyURL: "http://proxy.example.com:8080"}

	if err := applyProxy(tr, p); err != nil {
		t.Fatalf("applyProxy() returned error: %v", err)
	}

	if tr.Proxy == nil {
		t.Fatal("applyProxy() with http:// ProxyURL did not set Transport.Proxy, want non-nil")
	}
	if tr.DialContext != nil {
		t.Error("applyProxy() with http:// ProxyURL set Transport.DialContext, want nil")
	}

	req, err := http.NewRequest(http.MethodGet, "https://s3.example.com/bucket/key", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() returned error: %v", err)
	}

	proxyURL, err := tr.Proxy(req)
	if err != nil {
		t.Fatalf("tr.Proxy(req) returned error: %v", err)
	}
	if proxyURL == nil || proxyURL.Host != "proxy.example.com:8080" {
		t.Errorf("tr.Proxy(req) = %v, want host proxy.example.com:8080", proxyURL)
	}
}

// TestApplyProxySOCKS5SetsDialContext verifies that a "socks5://" ProxyURL
// with embedded credentials configures Transport.DialContext (SOCKS5
// dialing, since net/http's Transport.Proxy field cannot express SOCKS5) and
// leaves Proxy untouched.
func TestApplyProxySOCKS5SetsDialContext(t *testing.T) {
	t.Parallel()

	tr := &http.Transport{}
	p := domain.Profile{ProxyURL: "socks5://user:pass@proxy.example.com:1080"}

	if err := applyProxy(tr, p); err != nil {
		t.Fatalf("applyProxy() returned error: %v", err)
	}

	if tr.DialContext == nil {
		t.Fatal("applyProxy() with socks5:// ProxyURL did not set Transport.DialContext, want non-nil")
	}
	if tr.Proxy != nil {
		t.Error("applyProxy() with socks5:// ProxyURL set Transport.Proxy, want nil")
	}
}

// TestApplyProxyInvalidURLReturnsError verifies that an unparsable ProxyURL
// surfaces as a descriptive error rather than a nil error/opaque failure
// later on.
func TestApplyProxyInvalidURLReturnsError(t *testing.T) {
	t.Parallel()

	tr := &http.Transport{}
	p := domain.Profile{ProxyURL: "://not-a-valid-url"}

	err := applyProxy(tr, p)
	if err == nil {
		t.Fatal("applyProxy() with an invalid URL returned nil error, want error")
	}
}

// TestApplyProxyUnsupportedSchemeReturnsError verifies that a well-formed
// URL with a scheme other than http/https/socks5/socks5h is rejected with
// an error naming the offending scheme.
func TestApplyProxyUnsupportedSchemeReturnsError(t *testing.T) {
	t.Parallel()

	tr := &http.Transport{}
	p := domain.Profile{ProxyURL: "ftp://proxy.example.com:21"}

	err := applyProxy(tr, p)
	if err == nil {
		t.Fatal("applyProxy() with an unsupported scheme returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "ftp") {
		t.Errorf("applyProxy() error = %q, want it to mention the offending scheme %q", err.Error(), "ftp")
	}
}
