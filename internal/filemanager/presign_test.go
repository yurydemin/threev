package filemanager

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// presignedExpirySeconds parses the X-Amz-Expires query parameter (the
// TTL, in seconds, actually baked into the SigV4 signature) out of a
// presigned URL, failing the test if it is missing or malformed.
func presignedExpirySeconds(t *testing.T, rawURL string) int64 {
	t.Helper()

	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("url.Parse(%q) returned error: %v", rawURL, err)
	}

	raw := parsed.Query().Get("X-Amz-Expires")
	if raw == "" {
		t.Fatalf("URL %q has no X-Amz-Expires query parameter", rawURL)
	}

	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		t.Fatalf("X-Amz-Expires = %q, not a valid integer: %v", raw, err)
	}

	return seconds
}

func TestFileManagerServiceGetPresignedURL(t *testing.T) {
	t.Parallel()

	fm, repo, key := newTestFileManagerService(t)
	// Presigning never makes a network call, so there is no need to spin
	// up an httptest.Server; the profile's endpoint just needs to parse as
	// a valid URL.
	profileID := saveTestProfile(t, repo, key, "http://localhost:9000")

	rawURL, err := fm.GetPresignedURL(profileID, "bucket1", "path/to/file.txt", 300)
	if err != nil {
		t.Fatalf("GetPresignedURL() returned error: %v", err)
	}

	if rawURL == "" {
		t.Fatal("GetPresignedURL() returned an empty URL")
	}
	if !strings.Contains(rawURL, "X-Amz-Signature=") {
		t.Errorf("URL %q does not contain X-Amz-Signature", rawURL)
	}
	if !strings.Contains(rawURL, "bucket1") || !strings.Contains(rawURL, "path/to/file.txt") {
		t.Errorf("URL %q does not reference bucket/key", rawURL)
	}
}

func TestFileManagerServiceGetPresignedURLClampsExpiry(t *testing.T) {
	t.Parallel()

	fm, repo, key := newTestFileManagerService(t)
	profileID := saveTestProfile(t, repo, key, "http://localhost:9000")

	tests := []struct {
		name          string
		expirySeconds int64
		wantSeconds   int64
	}{
		{name: "below minimum clamps up to 60s", expirySeconds: 10, wantSeconds: 60},
		{name: "above maximum clamps down to 3600s", expirySeconds: 99999, wantSeconds: 3600},
		{name: "zero falls back to the 300s default", expirySeconds: 0, wantSeconds: 300},
		{name: "negative falls back to the 300s default", expirySeconds: -5, wantSeconds: 300},
		{name: "within range is left untouched", expirySeconds: 900, wantSeconds: 900},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rawURL, err := fm.GetPresignedURL(profileID, "bucket1", "file.txt", tt.expirySeconds)
			if err != nil {
				t.Fatalf("GetPresignedURL(expirySeconds=%d) returned error: %v", tt.expirySeconds, err)
			}

			if got := presignedExpirySeconds(t, rawURL); got != tt.wantSeconds {
				t.Errorf("X-Amz-Expires = %d, want %d", got, tt.wantSeconds)
			}
		})
	}
}

func TestClampPresignExpiry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		expirySeconds int64
		want          int64 // seconds
	}{
		{name: "zero uses default", expirySeconds: 0, want: 300},
		{name: "negative uses default", expirySeconds: -1, want: 300},
		{name: "below floor clamps to 60", expirySeconds: 1, want: 60},
		{name: "above ceiling clamps to 3600", expirySeconds: 7 * 24 * 3600, want: 3600},
		{name: "in range unchanged", expirySeconds: 120, want: 120},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := clampPresignExpiry(tt.expirySeconds)
			if got.Seconds() != float64(tt.want) {
				t.Errorf("clampPresignExpiry(%d) = %v, want %ds", tt.expirySeconds, got, tt.want)
			}
		})
	}
}
