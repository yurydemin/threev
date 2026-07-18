package s3client

import (
	"fmt"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"threev/internal/domain"
)

// defaultHTTPTimeout is a safe baseline request timeout applied to the
// http.Client backing every S3 client built by NewS3Client. It is
// deliberately simple for this stage of the project; the adaptive,
// per-operation timeout strategy described in docs/02-tech-spec.md section
// 10.4 belongs to the Transfer Engine (Stage 3), not connection
// management/testing.
const defaultHTTPTimeout = 30 * time.Second

// defaultRegion mirrors the "profiles.region" column default from
// docs/02-tech-spec.md section 8.1. It is applied here - the single place
// every S3 client is constructed - rather than only at the database layer,
// so an empty Region (e.g. a not-yet-saved profile being tested straight
// from an edit form) never reaches the AWS SDK, which rejects it outright
// ("Invalid region: region was not a valid DNS name") instead of falling
// back to any default of its own.
const defaultRegion = "us-east-1"

// NewS3Client builds an *s3.Client configured from a connection profile:
// static credentials, region, custom endpoint, path-style addressing,
// optional TLS verification skip, and optional custom headers, using a
// plain, non-pooled *http.Client (see newHTTPClient). This is the
// constructor used by callers that build a short-lived client per call
// (connection testing, file manager listing) rather than the long-lived,
// pooled/fresh pair managed by ConnectionManager (Stage 3, manager.go).
func NewS3Client(p domain.Profile) (*s3.Client, error) {
	httpClient, err := newHTTPClient(p)
	if err != nil {
		return nil, fmt.Errorf("build http client: %w", err)
	}

	return buildClient(p, httpClient)
}

// NewS3ClientWithHTTPClient builds an *s3.Client identically to NewS3Client,
// except it sends requests through the given httpClient instead of building
// one internally. This lets callers that manage their own long-lived,
// pooled *http.Client/*http.Transport (ConnectionManager, Stage 3) construct
// an *s3.Client on top of it without duplicating the aws.Config/s3.Options
// wiring that also lives in NewS3Client.
func NewS3ClientWithHTTPClient(p domain.Profile, httpClient *http.Client) (*s3.Client, error) {
	return buildClient(p, httpClient)
}

// buildClient is the shared implementation behind NewS3Client and
// NewS3ClientWithHTTPClient: it builds the aws.Config (region, static
// credentials) and s3.Client (custom endpoint, path-style addressing,
// custom headers) common to both, differing only in which *http.Client
// backs outgoing requests.
//
// The SDK's built-in retry behavior is always disabled (aws.NopRetryer{}):
// this application implements its own retry strategy (docs/02-tech-spec.md
// section 10.4, s3client/retry.go, a later step) with its own backoff,
// per-attempt fresh-connection behavior, and circuit breaker integration.
// Leaving the SDK's default Standard retryer in place would let each
// logical "attempt" as seen by our retry layer silently retry up to 3 more
// times inside the SDK first, breaking backoff/circuit-breaker accounting.
func buildClient(p domain.Profile, httpClient *http.Client) (*s3.Client, error) {
	region := p.Region
	if region == "" {
		region = defaultRegion
	}

	cfg := aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(p.AccessKeyID, p.SecretAccessKey, p.SessionToken),
		HTTPClient:  httpClient,
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		endpoint := p.EndpointURL
		o.BaseEndpoint = &endpoint
		o.UsePathStyle = p.PathStyle
		o.Retryer = aws.NopRetryer{}

		for header, value := range p.CustomHeaders {
			o.APIOptions = append(o.APIOptions, smithyhttp.AddHeaderValue(header, value))
		}
	})

	return client, nil
}

// newHTTPClient builds the plain *http.Client used by NewS3Client: a
// timeout-bounded client with no connection pooling tuning of its own
// (relying on Go's http.DefaultTransport-like defaults), optionally with
// TLS verification disabled and/or routed through a per-profile proxy. This
// is deliberately simple - see transport.go's newPooledTransport/
// newFreshTransport for the tuned transports used by ConnectionManager.
//
// When p.VerifySSL is false, TLS certificate verification is disabled on
// this client only, and only because the user explicitly requested it for
// this specific profile - never globally - per SEC-004
// (docs/02-tech-spec.md section 11).
//
// A *http.Transport is now always built (previously only when
// !p.VerifySSL), because applying p.ProxyURL (via applyProxy) requires a
// concrete *http.Transport to configure regardless of the TLS verification
// setting.
func newHTTPClient(p domain.Profile) (*http.Client, error) {
	t := &http.Transport{
		TLSClientConfig: tlsConfigFor(p),
	}

	if err := applyProxy(t, p); err != nil {
		return nil, err
	}

	return &http.Client{
		Timeout:   defaultHTTPTimeout,
		Transport: t,
	}, nil
}
