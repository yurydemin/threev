//go:build integration

// Package integration holds black-box tests that exercise threev's core
// services (ConnectionService/FileManagerService/TransferService,
// constructed exactly as app.go's newApp wires them up) against a real,
// network-reachable S3-compatible server (MinIO in practice -
// docs/02-tech-spec.md section 13, AC-002/AC-003) rather than the
// httptest-based mocks every other package's test suite relies on.
//
// Gated behind the "integration" build tag so `go test ./...` (no tag)
// never even compiles this package - no network dependency, no Docker
// requirement, for ordinary local development or the fast unit-tests CI
// job. Running these tests requires
// `go test -tags=integration ./internal/integration/...` AND a reachable
// MinIO instance (see requireMinIO's own doc comment for exactly how
// availability is determined and what happens when it is not).
//
// Endpoint/credentials are fully parameterized via THREEV_INTEGRATION_S3_*
// environment variables (defaulting to bitnamilegacy/minio's own documented
// quick-start values), so this exact same suite can be pointed at a real
// AWS S3 bucket later without a single code change (Этап 5 plan,
// "Архитектурные решения").
package integration

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// defaultS3Endpoint/defaultS3AccessKeyID/defaultS3SecretAccessKey match
// bitnamilegacy/minio's own documented quick-start defaults: a bare
// `docker run -p 9000:9000 -e MINIO_ROOT_USER=minioadmin
// -e MINIO_ROOT_PASSWORD=minioadmin bitnamilegacy/minio:2025.7.23` works
// with these values with zero further configuration.
const (
	defaultS3Endpoint        = "http://localhost:9000"
	defaultS3AccessKeyID     = "minioadmin"
	defaultS3SecretAccessKey = "minioadmin"
)

// Environment variable names read by s3Endpoint/s3AccessKeyID/
// s3SecretAccessKey.
const (
	envS3Endpoint        = "THREEV_INTEGRATION_S3_ENDPOINT"
	envS3AccessKeyID     = "THREEV_INTEGRATION_S3_ACCESS_KEY"
	envS3SecretAccessKey = "THREEV_INTEGRATION_S3_SECRET_KEY"
)

// healthCheckTimeout bounds TestMain's single startup health-check request -
// short and fixed, since this is purely an "is anything listening at all"
// probe, not a real S3 operation.
const healthCheckTimeout = 5 * time.Second

// minioAvailable/minioUnavailableReason are set once, by TestMain, before
// any test in this package runs. Every test (by way of
// newIntegrationServices, helpers_test.go) calls requireMinIO(t) first,
// t.Skip-ing with minioUnavailableReason if minioAvailable is false - this
// is deliberately NOT implemented by simply never calling m.Run() in
// TestMain: skipping each test individually, with `go test -v` recording an
// explicit "--- SKIP" line and reason per test, is far more legible in CI/
// local output than a single early os.Exit(0) that silently reports zero
// tests ran and gives no indication anything was even attempted.
var (
	minioAvailable         bool
	minioUnavailableReason string
)

// TestMain runs a single health check against the configured MinIO
// endpoint before any test in this package executes, recording the result
// in minioAvailable/minioUnavailableReason for requireMinIO to consult.
// Regardless of the outcome, m.Run() is always called (and its result
// passed to os.Exit) - a down/unreachable MinIO is reported as a clean,
// per-test skip, never a failed or panicking test run.
func TestMain(m *testing.M) {
	endpoint := s3Endpoint()

	ctx, cancel := context.WithTimeout(context.Background(), healthCheckTimeout)
	defer cancel()

	if err := checkMinIOHealth(ctx, endpoint); err != nil {
		minioUnavailableReason = fmt.Sprintf(
			"integration tests require MinIO at %s (health check failed: %v); "+
				"run `docker run -p 9000:9000 -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin bitnamilegacy/minio:2025.7.23` "+
				"or set THREEV_INTEGRATION_S3_ENDPOINT/THREEV_INTEGRATION_S3_ACCESS_KEY/THREEV_INTEGRATION_S3_SECRET_KEY "+
				"to point these tests at a different instance", endpoint, err)
		fmt.Fprintln(os.Stderr, minioUnavailableReason)
	} else {
		minioAvailable = true
	}

	os.Exit(m.Run())
}

// checkMinIOHealth issues a single GET <endpoint>/minio/health/live
// (MinIO's documented liveness probe, the same one the Этап 5 plan's
// ci.yml service-container healthcheck uses), returning nil only on a 200
// response.
func checkMinIOHealth(ctx context.Context, endpoint string) error {
	url := strings.TrimRight(endpoint, "/") + "/minio/health/live"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build health check request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}

	return nil
}

// requireMinIO skips t (with a clear, actionable message) unless TestMain's
// startup health check found a reachable MinIO instance. Called once, at
// the top of newIntegrationServices (helpers_test.go), so every test in
// this package that builds its services through that helper gets this
// guard automatically without repeating it at every call site.
func requireMinIO(t *testing.T) {
	t.Helper()

	if !minioAvailable {
		t.Skip(minioUnavailableReason)
	}
}

// s3Endpoint returns THREEV_INTEGRATION_S3_ENDPOINT, or defaultS3Endpoint if
// unset/empty.
func s3Endpoint() string {
	return envOrDefault(envS3Endpoint, defaultS3Endpoint)
}

// s3AccessKeyID returns THREEV_INTEGRATION_S3_ACCESS_KEY, or
// defaultS3AccessKeyID if unset/empty.
func s3AccessKeyID() string {
	return envOrDefault(envS3AccessKeyID, defaultS3AccessKeyID)
}

// s3SecretAccessKey returns THREEV_INTEGRATION_S3_SECRET_KEY, or
// defaultS3SecretAccessKey if unset/empty.
func s3SecretAccessKey() string {
	return envOrDefault(envS3SecretAccessKey, defaultS3SecretAccessKey)
}

// envOrDefault returns os.Getenv(key), or fallback if that is empty.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return fallback
}
