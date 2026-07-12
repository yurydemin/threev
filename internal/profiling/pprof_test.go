package profiling

import (
	"net/http"
	"testing"
	"time"
)

// testAddr is a fixed, arbitrary loopback address used across this file's
// tests. EnableDebugServer takes a concrete addr rather than letting the OS
// pick an ephemeral port, so the tests below follow suit and just pick a
// port unlikely to already be in use on a CI/dev machine.
const testAddr = "127.0.0.1:16060"

func TestEnableDebugServerServesAndStops(t *testing.T) {
	stop, err := EnableDebugServer(testAddr)
	if err != nil {
		t.Fatalf("EnableDebugServer(%q) error = %v", testAddr, err)
	}
	t.Cleanup(stop)

	url := "http://" + testAddr + "/debug/pprof/"

	resp, err := http.Get(url) //nolint:noctx,gosec // test-only fixed loopback URL, no need for a context
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET %s status = %d, want %d", url, resp.StatusCode, http.StatusOK)
	}

	stop()

	// Give the OS a brief moment to actually release the socket after
	// Shutdown returns before asserting the connection is refused.
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, getErr := http.Get(url) //nolint:noctx,gosec // test-only fixed loopback URL
		if getErr != nil {
			lastErr = getErr
			break
		}
		_ = resp.Body.Close()
		time.Sleep(20 * time.Millisecond)
	}

	if lastErr == nil {
		t.Fatalf("GET %s after stop() succeeded, want connection error", url)
	}
}

func TestEnableDebugServerAddressInUse(t *testing.T) {
	stop, err := EnableDebugServer(testAddr)
	if err != nil {
		t.Fatalf("first EnableDebugServer(%q) error = %v", testAddr, err)
	}
	t.Cleanup(stop)

	_, err = EnableDebugServer(testAddr)
	if err == nil {
		t.Fatalf("second EnableDebugServer(%q) error = nil, want a bind error", testAddr)
	}
}
