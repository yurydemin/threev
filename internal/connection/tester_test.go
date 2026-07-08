package connection

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"threev/internal/domain"
)

func testerProfile(endpoint string) domain.Profile {
	return domain.Profile{
		Name:            "test",
		EndpointURL:     endpoint,
		Region:          "us-east-1",
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "supersecret",
		PathStyle:       true,
		VerifySSL:       true,
	}
}

const listBucketsSuccessBody = `<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Owner>
    <ID>owner-id</ID>
    <DisplayName>owner-name</DisplayName>
  </Owner>
  <Buckets>
    <Bucket>
      <Name>bucket1</Name>
      <CreationDate>2019-01-01T00:00:00.000Z</CreationDate>
    </Bucket>
  </Buckets>
</ListAllMyBucketsResult>`

const accessDeniedErrorBody = `<?xml version="1.0" encoding="UTF-8"?>
<Error>
  <Code>AccessDenied</Code>
  <Message>Access Denied</Message>
  <RequestId>test-request-id</RequestId>
  <HostId>test-host-id</HostId>
</Error>`

func TestTestConnectionSuccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(listBucketsSuccessBody))
	}))
	t.Cleanup(server.Close)

	result := TestConnection(context.Background(), testerProfile(server.URL))

	if !result.Success {
		t.Fatalf("Success = false, want true; Message=%q Detail=%q Category=%q", result.Message, result.Detail, result.Category)
	}
	if result.Message == "" {
		t.Error("Message is empty on success")
	}
}

func TestTestConnectionAuthError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(accessDeniedErrorBody))
	}))
	t.Cleanup(server.Close)

	result := TestConnection(context.Background(), testerProfile(server.URL))

	if result.Success {
		t.Fatal("Success = true, want false")
	}
	if result.Category != "auth" {
		t.Errorf("Category = %q, want %q (Detail=%q)", result.Category, "auth", result.Detail)
	}
	if result.Detail == "" {
		t.Error("Detail is empty on failure")
	}
}

func TestTestConnectionNetworkError(t *testing.T) {
	t.Parallel()

	// Start and immediately close a server: its address is valid but
	// nothing is listening, so any request against it fails to connect.
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint := server.URL
	server.Close()

	result := TestConnection(context.Background(), testerProfile(endpoint))

	if result.Success {
		t.Fatal("Success = true, want false")
	}
	if result.Category != "network" {
		t.Errorf("Category = %q, want %q (Detail=%q)", result.Category, "network", result.Detail)
	}
}

func TestTestConnectionTLSError(t *testing.T) {
	t.Parallel()

	// Plain HTTP server addressed via an https:// URL: the client's TLS
	// handshake fails immediately because the server never speaks TLS.
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(server.Close)

	httpsEndpoint := "https://" + strings.TrimPrefix(server.URL, "http://")

	result := TestConnection(context.Background(), testerProfile(httpsEndpoint))

	if result.Success {
		t.Fatal("Success = true, want false")
	}
	if result.Category != "tls" {
		t.Errorf("Category = %q, want %q (Detail=%q)", result.Category, "tls", result.Detail)
	}
}

func TestTestConnectionTimeout(t *testing.T) {
	t.Parallel()

	blockUntil := make(chan struct{})
	t.Cleanup(func() { close(blockUntil) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-blockUntil:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	result := TestConnection(ctx, testerProfile(server.URL))

	if result.Success {
		t.Fatal("Success = true, want false")
	}
	if result.Category != "timeout" {
		t.Errorf("Category = %q, want %q (Detail=%q)", result.Category, "timeout", result.Detail)
	}
}
