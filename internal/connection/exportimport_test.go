package connection

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConnectionServiceRequireWailsContext exercises requireWailsContext's
// own contract directly: an error before SetContext, and the stored context
// back after it.
func TestConnectionServiceRequireWailsContext(t *testing.T) {
	t.Parallel()

	svc := newTestConnectionService(t)

	if _, err := svc.requireWailsContext(); !errors.Is(err, errWailsContextNotSet) {
		t.Fatalf("requireWailsContext() before SetContext error = %v, want errors.Is(_, errWailsContextNotSet)", err)
	}

	ctx := context.Background()
	svc.SetContext(ctx)

	got, err := svc.requireWailsContext()
	if err != nil {
		t.Fatalf("requireWailsContext() after SetContext returned error: %v", err)
	}
	if got != ctx {
		t.Error("requireWailsContext() after SetContext did not return the stored context")
	}
}

func TestExportProfilesToFile(t *testing.T) {
	t.Parallel()

	svc := newTestConnectionService(t)

	if _, err := svc.SaveProfile(sampleServiceProfile("prod")); err != nil {
		t.Fatalf("SaveProfile() returned error: %v", err)
	}

	path := filepath.Join(t.TempDir(), "export.json")

	if err := exportProfilesToFile(context.Background(), svc.repo, path); err != nil {
		t.Fatalf("exportProfilesToFile() returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error: %v", path, err)
	}

	raw := string(data)

	// Spot-check the raw JSON text for the seeded secret/access key strings
	// directly, not just that ExportProfileEntry lacks the field - a stronger
	// guarantee against an accidental future field-name typo reintroducing a
	// leak.
	for _, secret := range []string{"plaintext-secret", "plaintext-session-token", "AKIAEXAMPLE"} {
		if strings.Contains(raw, secret) {
			t.Errorf("exported file contains credential value %q, want it absent", secret)
		}
	}

	var entries []ExportProfileEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("unmarshal exported file: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}

	got := entries[0]
	if got.Name != "prod" {
		t.Errorf("Name = %q, want %q", got.Name, "prod")
	}
	if got.EndpointURL != "https://s3.example.com" {
		t.Errorf("EndpointURL = %q, want %q", got.EndpointURL, "https://s3.example.com")
	}
	if got.Region != "us-east-1" {
		t.Errorf("Region = %q, want %q", got.Region, "us-east-1")
	}
	if !got.PathStyle {
		t.Error("PathStyle = false, want true")
	}
	if !got.VerifySSL {
		t.Error("VerifySSL = false, want true")
	}
	if got.CustomHeaders["X-Custom-Header"] != "value" {
		t.Errorf("CustomHeaders[X-Custom-Header] = %q, want %q", got.CustomHeaders["X-Custom-Header"], "value")
	}
}

func TestImportProfilesFromFileCreatesBlankCredentialProfile(t *testing.T) {
	t.Parallel()

	svc := newTestConnectionService(t)

	path := writeImportFile(t, []ExportProfileEntry{
		{Name: "imported", EndpointURL: "https://s3.example.com", Region: "us-east-1", PathStyle: true, VerifySSL: true},
	})

	result, err := importProfilesFromFile(context.Background(), svc.repo, path)
	if err != nil {
		t.Fatalf("importProfilesFromFile() returned error: %v", err)
	}
	if result.ImportedCount != 1 {
		t.Errorf("ImportedCount = %d, want 1", result.ImportedCount)
	}
	if len(result.SkippedNames) != 0 {
		t.Errorf("SkippedNames = %v, want empty", result.SkippedNames)
	}

	dtos, err := svc.GetProfiles()
	if err != nil {
		t.Fatalf("GetProfiles() returned error: %v", err)
	}
	if len(dtos) != 1 {
		t.Fatalf("len(dtos) = %d, want 1", len(dtos))
	}
	if dtos[0].HasCredentials {
		t.Error("HasCredentials = true for an imported profile, want false (blank credentials)")
	}

	full, err := svc.repo.GetByID(context.Background(), dtos[0].ID)
	if err != nil {
		t.Fatalf("GetByID() returned error: %v", err)
	}
	if full.AccessKeyID != "" || full.SecretAccessKey != "" || full.SessionToken != "" {
		t.Errorf("imported profile has non-blank credentials: %+v", full)
	}
}

func TestImportProfilesFromFileSkipsDuplicateName(t *testing.T) {
	t.Parallel()

	svc := newTestConnectionService(t)

	if _, err := svc.SaveProfile(sampleServiceProfile("prod")); err != nil {
		t.Fatalf("SaveProfile() returned error: %v", err)
	}

	path := writeImportFile(t, []ExportProfileEntry{
		{Name: "prod", EndpointURL: "https://s3.example.com"},
	})

	result, err := importProfilesFromFile(context.Background(), svc.repo, path)
	if err != nil {
		t.Fatalf("importProfilesFromFile() returned error: %v", err)
	}
	if result.ImportedCount != 0 {
		t.Errorf("ImportedCount = %d, want 0", result.ImportedCount)
	}
	if len(result.SkippedNames) != 1 || result.SkippedNames[0] != "prod" {
		t.Errorf("SkippedNames = %v, want [\"prod\"]", result.SkippedNames)
	}
}

// TestImportProfilesFromFileRepeatedImportSkipsDuplicates is the full dedup
// regression the plan's checkpoint calls out explicitly: re-running import
// on the same file a second time imports 0 new profiles the second time.
func TestImportProfilesFromFileRepeatedImportSkipsDuplicates(t *testing.T) {
	t.Parallel()

	svc := newTestConnectionService(t)

	path := writeImportFile(t, []ExportProfileEntry{
		{Name: "alpha", EndpointURL: "https://s3.example.com"},
		{Name: "beta", EndpointURL: "https://s3.example.com"},
	})

	first, err := importProfilesFromFile(context.Background(), svc.repo, path)
	if err != nil {
		t.Fatalf("first importProfilesFromFile() returned error: %v", err)
	}
	if first.ImportedCount != 2 {
		t.Fatalf("first ImportedCount = %d, want 2", first.ImportedCount)
	}
	if len(first.SkippedNames) != 0 {
		t.Fatalf("first SkippedNames = %v, want empty", first.SkippedNames)
	}

	second, err := importProfilesFromFile(context.Background(), svc.repo, path)
	if err != nil {
		t.Fatalf("second importProfilesFromFile() returned error: %v", err)
	}
	if second.ImportedCount != 0 {
		t.Errorf("second ImportedCount = %d, want 0", second.ImportedCount)
	}
	if len(second.SkippedNames) != 2 {
		t.Errorf("second SkippedNames = %v, want 2 entries", second.SkippedNames)
	}
}

func TestImportProfilesFromFileMalformedJSON(t *testing.T) {
	t.Parallel()

	svc := newTestConnectionService(t)

	path := filepath.Join(t.TempDir(), "malformed.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("WriteFile() returned error: %v", err)
	}

	_, err := importProfilesFromFile(context.Background(), svc.repo, path)
	if err == nil {
		t.Fatal("importProfilesFromFile() returned nil error, want an error for malformed JSON")
	}
}

// TestImportProfilesFromFileSkipsInvalidEntries verifies the Step 3.5
// decision documented on importProfilesFromFile: an entry with an empty
// Name, or an invalid EndpointURL, is folded into SkippedNames (not a hard
// import-wide error), while a valid sibling entry in the same file still
// imports successfully.
func TestImportProfilesFromFileSkipsInvalidEntries(t *testing.T) {
	t.Parallel()

	svc := newTestConnectionService(t)

	path := writeImportFile(t, []ExportProfileEntry{
		{Name: "", EndpointURL: "https://s3.example.com"},
		{Name: "bad-endpoint", EndpointURL: "not-a-url"},
		{Name: "good", EndpointURL: "https://s3.example.com"},
	})

	result, err := importProfilesFromFile(context.Background(), svc.repo, path)
	if err != nil {
		t.Fatalf("importProfilesFromFile() returned error: %v", err)
	}
	if result.ImportedCount != 1 {
		t.Errorf("ImportedCount = %d, want 1", result.ImportedCount)
	}
	if len(result.SkippedNames) != 2 {
		t.Errorf("SkippedNames = %v, want 2 entries", result.SkippedNames)
	}
}

// writeImportFile marshals entries as indented JSON (the same shape
// exportProfilesToFile produces) into a fresh file under t.TempDir() and
// returns its path.
func writeImportFile(t *testing.T, entries []ExportProfileEntry) string {
	t.Helper()

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		t.Fatalf("marshal import fixture: %v", err)
	}

	path := filepath.Join(t.TempDir(), "import.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile(%q) returned error: %v", path, err)
	}

	return path
}
