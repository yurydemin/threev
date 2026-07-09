package s3client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"

	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

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

// ClassifyError inspects err's type chain (via errors.Is/As) to determine a
// category and a default human-readable message, for use by any caller that
// needs to turn a raw S3/network error into UI-facing feedback (connection
// testing, file manager listing/preview, etc). Checks are ordered from most
// to least specific: cancellation is checked before timeout so a
// user-initiated abort (closing the dialog, navigating away) is never
// reported as "the server took too long"; a deadline/timeout takes priority
// over the generic network category a *net.OpError might also match; and
// TLS/auth failures are distinguished from plain connectivity failures.
func ClassifyError(err error) (category, message string) {
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

		// HEAD responses (HeadObject) never carry a body (RFC 9110), so the
		// SDK's error deserializer has no <Error><Code>...</Code></Error>
		// XML to read a real code like "AccessDenied" from - it instead
		// synthesizes a pseudo-code from the bare HTTP status text (e.g.
		// "Forbidden" for 401/403), which authErrorCodes above does not and
		// cannot enumerate. Falling back to the raw status code here catches
		// exactly this case, so a bad-credentials HeadObject call is
		// classified "auth" (fail fast) instead of "unknown" (retried
		// through MetadataRetryPolicy's full backoff for no benefit - wrong
		// credentials will not start working on attempt 2 or 3).
		if isAuthStatusCode(apiErr) {
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

// isAuthStatusCode reports whether err (at any depth) carries an HTTP 401 or
// 403 status, the fallback signal for an auth failure when no structured API
// error code is available (see ClassifyError's HeadObject comment above).
func isAuthStatusCode(err error) bool {
	var respErr *smithyhttp.ResponseError
	if !errors.As(err, &respErr) {
		return false
	}

	switch respErr.HTTPStatusCode() {
	case http.StatusUnauthorized, http.StatusForbidden:
		return true
	default:
		return false
	}
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
