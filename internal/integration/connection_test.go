//go:build integration

package integration

import "testing"

// TestIntegrationConnectionTestConnection is the automated MinIO half of
// AC-002 (docs/02-tech-spec.md section 13): a profile saved through
// ConnectionService.SaveProfile (newIntegrationServices) actually
// round-trips a successful TestConnection call against a real,
// network-reachable MinIO instance - not an httptest mock.
func TestIntegrationConnectionTestConnection(t *testing.T) {
	svc := newIntegrationServices(t)

	result, err := svc.conn.TestConnection(svc.profile)
	if err != nil {
		t.Fatalf("TestConnection() returned Go error = %v, want nil", err)
	}

	if !result.Success {
		t.Fatalf("TestConnection() = %+v, want Success = true", result)
	}
}

// TestIntegrationConnectionTestConnectionBadCredentials verifies the
// negative path against the same real MinIO instance: a profile whose
// secret access key is wrong is reported as a failure (never a Go error,
// never a panic), with a non-empty Category classifying it.
func TestIntegrationConnectionTestConnectionBadCredentials(t *testing.T) {
	svc := newIntegrationServices(t)

	bad := svc.profile
	bad.SecretAccessKey = "definitely-wrong-secret-access-key"

	result, err := svc.conn.TestConnection(bad)
	if err != nil {
		t.Fatalf("TestConnection() returned Go error = %v, want nil", err)
	}

	if result.Success {
		t.Fatal("TestConnection() Success = true, want false for bad credentials")
	}

	if result.Category == "" {
		t.Error("TestConnection() Category is empty on failure, want a non-empty classification")
	}
}
