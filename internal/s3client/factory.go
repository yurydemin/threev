package s3client

import (
	"crypto/tls"
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
// optional TLS verification skip, and optional custom headers.
func NewS3Client(p domain.Profile) (*s3.Client, error) {
	region := p.Region
	if region == "" {
		region = defaultRegion
	}

	cfg := aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(p.AccessKeyID, p.SecretAccessKey, p.SessionToken),
		HTTPClient:  newHTTPClient(p),
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		endpoint := p.EndpointURL
		o.BaseEndpoint = &endpoint
		o.UsePathStyle = p.PathStyle

		for header, value := range p.CustomHeaders {
			o.APIOptions = append(o.APIOptions, smithyhttp.AddHeaderValue(header, value))
		}
	})

	return client, nil
}

// newHTTPClient builds the *http.Client used to send every request for
// profile p. When p.VerifySSL is false, TLS certificate verification is
// disabled on this client only, and only because the user explicitly
// requested it for this specific profile - never globally - per SEC-004
// (docs/02-tech-spec.md section 11).
func newHTTPClient(p domain.Profile) *http.Client {
	client := &http.Client{
		Timeout: defaultHTTPTimeout,
	}

	if !p.VerifySSL {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // explicit, per-profile opt-out required by SEC-004; never a global default
		}
	}

	return client
}
