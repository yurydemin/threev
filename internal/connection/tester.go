package connection

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

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

// authErrorCodes are the S3/AWS API error codes that indicate a problem
// with credentials or authorization, as opposed to some other API-level
// failure.
var authErrorCodes = map[string]bool{
	"InvalidAccessKeyId":           true,
	"SignatureDoesNotMatch":        true,
	"AccessDenied":                 true,
	"InvalidToken":                 true,
	"ExpiredToken":                 true,
	"TokenRefreshRequired":         true,
	"AuthorizationHeaderMalformed": true,
	"NotSignedUp":                  true,
	"AccountProblem":               true,
}

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
	category, message := classifyConnectionError(err)

	return domain.ConnectionTestResult{
		Success:  false,
		Message:  message,
		Detail:   err.Error(),
		Category: category,
	}
}

// classifyConnectionError inspects err's type chain (via errors.Is/As) to
// determine a Category and default Message. Checks are ordered from most
// to least specific: cancellation is checked before timeout so a
// user-initiated abort (closing the dialog, navigating away) is never
// reported as "the server took too long"; a deadline/timeout takes priority
// over the generic network category a *net.OpError might also match; and
// TLS/auth failures are distinguished from plain connectivity failures.
func classifyConnectionError(err error) (category, message string) {
	if errors.Is(err, context.Canceled) {
		return "cancelled", "Проверка отменена"
	}

	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return "timeout", "Превышено время ожидания подключения"
	}

	if isTLSError(err) {
		return "tls", "Ошибка проверки SSL-сертификата"
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		if authErrorCodes[apiErr.ErrorCode()] {
			return "auth", "Неверные учётные данные"
		}

		return "unknown", "Хранилище вернуло ошибку: " + apiErr.ErrorCode()
	}

	if isNetworkError(err) {
		return "network", "Не удалось подключиться к endpoint"
	}

	return "unknown", "Не удалось проверить подключение"
}

// isTLSError reports whether err (at any depth) is a TLS/certificate
// verification failure, or a plaintext-vs-TLS protocol mismatch (e.g. an
// https:// endpoint that is actually serving plain HTTP).
func isTLSError(err error) bool {
	var (
		certVerifyErr   *tls.CertificateVerificationError
		recordHeaderErr tls.RecordHeaderError
		hostnameErr     x509.HostnameError
		unknownAuthErr  x509.UnknownAuthorityError
		certInvalidErr  x509.CertificateInvalidError
	)

	return errors.As(err, &certVerifyErr) ||
		errors.As(err, &recordHeaderErr) ||
		errors.As(err, &hostnameErr) ||
		errors.As(err, &unknownAuthErr) ||
		errors.As(err, &certInvalidErr) ||
		errors.Is(err, http.ErrSchemeMismatch)
}

// isNetworkError reports whether err (at any depth) is a lower-level
// connectivity failure: DNS resolution, connection refused/reset, an
// unexpected end-of-stream (a peer closing/resetting the TCP connection
// mid-response, common on flaky links - a key scenario for this product),
// or any other *net.OpError/*net.DNSError.
func isNetworkError(err error) bool {
	var (
		dnsErr *net.DNSError
		opErr  *net.OpError
	)

	return errors.As(err, &dnsErr) ||
		errors.As(err, &opErr) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF)
}
