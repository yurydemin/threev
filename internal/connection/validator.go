package connection

import (
	"errors"
	"fmt"
	"net/url"

	"threev/internal/domain"
)

// errEmptyAccessKeyID and errEmptySecretAccessKey guard against saving a
// profile with no credentials at all. Unlike domain.ErrInvalidEndpoint /
// domain.ErrInvalidProfileName, these are not part of the sentinel-error
// contract shared with callers via errors.Is - they exist purely so
// ValidateProfile's error messages are specific.
var (
	errEmptyAccessKeyID     = errors.New("access key id must not be empty")
	errEmptySecretAccessKey = errors.New("secret access key must not be empty")
)

// ValidateProfile checks that a profile's fields are well-formed:
//   - Name is not empty.
//   - EndpointURL parses as an absolute URL with an http or https scheme.
//   - AccessKeyID and SecretAccessKey are not empty.
//
// ValidateProfile does not check name uniqueness: it has no database
// access, so that check is the caller's responsibility (repository's
// ExistsByName / Service layer).
func ValidateProfile(p domain.Profile) error {
	if p.Name == "" {
		return domain.ErrInvalidProfileName
	}

	if err := validateEndpoint(p.EndpointURL); err != nil {
		return err
	}

	if p.AccessKeyID == "" {
		return errEmptyAccessKeyID
	}

	if p.SecretAccessKey == "" {
		return errEmptySecretAccessKey
	}

	return nil
}

// validateEndpoint parses raw as a URL and requires an http or https scheme
// plus a non-empty host.
func validateEndpoint(raw string) error {
	if raw == "" {
		return fmt.Errorf("%w: endpoint URL must not be empty", domain.ErrInvalidEndpoint)
	}

	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: %s", domain.ErrInvalidEndpoint, err.Error())
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: scheme must be http or https, got %q", domain.ErrInvalidEndpoint, u.Scheme)
	}

	if u.Host == "" {
		return fmt.Errorf("%w: missing host", domain.ErrInvalidEndpoint)
	}

	return nil
}
