package connection

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"threev/internal/domain"
	"threev/internal/s3client"
)

// testTimeout bounds a single explicit TestConnection call. This is a
// deliberately generous, fixed timeout for an interactive, user-initiated
// action - distinct from the adaptive per-request timeout strategy
// described in docs/02-tech-spec.md section 10.4, which governs the
// Transfer Engine (Stage 3).
//
// Precedence vs. s3client's HTTP transport timeout: NewS3Client's
// http.Client carries its own 30s Timeout (s3client/factory.go,
// defaultHTTPTimeout), which is meant to bound individual future data-plane
// operations (Stage 2+). Here, ctx is wrapped with context.WithTimeout(ctx,
// testTimeout) *before* the request is issued, so for TestConnection
// specifically this 10s deadline always fires first and the 30s transport
// timeout never gets a chance to. That is intentional, not a bug: an
// explicit, user-initiated "Test connection" click should fail fast well
// before the generic transport ceiling, while the 30s value remains the
// sensible default for other, non-interactive S3 operations built on the
// same client.
const testTimeout = 10 * time.Second

// TestConnection verifies that a profile's endpoint is reachable and its
// credentials are valid by calling ListBuckets. It never returns a Go
// error: every possible outcome, success or failure, is reported through
// the returned domain.ConnectionTestResult, since this is a terminal
// operation consumed directly by the UI.
func TestConnection(ctx context.Context, p domain.Profile) domain.ConnectionTestResult {
	client, err := s3client.NewS3Client(p)
	if err != nil {
		return domain.ConnectionTestResult{
			Success:  false,
			Message:  "Не удалось создать S3-клиент",
			Detail:   err.Error(),
			Category: "unknown",
		}
	}

	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()

	if _, err := client.ListBuckets(ctx, &s3.ListBucketsInput{}); err != nil {
		return mapConnectionError(err)
	}

	return domain.ConnectionTestResult{
		Success: true,
		Message: "Подключение успешно",
	}
}

// mapConnectionError classifies err into a domain.ConnectionTestResult with
// a human-readable message and a Category suitable for UI iconography.
func mapConnectionError(err error) domain.ConnectionTestResult {
	category, message := s3client.ClassifyError(err)

	return domain.ConnectionTestResult{
		Success:  false,
		Message:  message,
		Detail:   err.Error(),
		Category: category,
	}
}
