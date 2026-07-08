package connection

import (
	"errors"
	"path/filepath"
	"testing"

	"threev/internal/domain"
	"threev/internal/storage"
)

// newTestConnectionService opens a fresh migrated SQLite database backed by
// a temporary file and returns a ConnectionService over it, using a fixed
// (test-only) 32-byte encryption key.
func newTestConnectionService(t *testing.T) *ConnectionService {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "connection_service_test.db")

	db, err := storage.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB(%q) returned error: %v", dbPath, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close() returned error: %v", err)
		}
	})

	repo := storage.NewProfileRepository(db)

	var key [32]byte
	for i := range key {
		key[i] = byte(i)
	}

	return NewConnectionService(repo, key)
}

func sampleServiceProfile(name string) domain.Profile {
	return domain.Profile{
		Name:            name,
		EndpointURL:     "https://s3.example.com",
		Region:          "us-east-1",
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "plaintext-secret",
		SessionToken:    "plaintext-session-token",
		PathStyle:       true,
		VerifySSL:       true,
		CustomHeaders:   map[string]string{"X-Custom-Header": "value"},
	}
}

func TestConnectionServiceSaveProfileCreate(t *testing.T) {
	t.Parallel()

	svc := newTestConnectionService(t)

	saved, err := svc.SaveProfile(sampleServiceProfile("prod"))
	if err != nil {
		t.Fatalf("SaveProfile() returned error: %v", err)
	}

	if saved.ID == 0 {
		t.Fatal("SaveProfile() did not populate ID")
	}
	if saved.Name != "prod" {
		t.Errorf("Name = %q, want %q", saved.Name, "prod")
	}
	if saved.AccessKeyID != "AKIAEXAMPLE" {
		t.Errorf("AccessKeyID = %q, want %q (must not be masked/encrypted)", saved.AccessKeyID, "AKIAEXAMPLE")
	}
	if saved.SecretAccessKey != "" {
		t.Errorf("SaveProfile() response SecretAccessKey = %q, want empty (must not echo secret back)", saved.SecretAccessKey)
	}
	if saved.SessionToken != "" {
		t.Errorf("SaveProfile() response SessionToken = %q, want empty (must not echo secret back)", saved.SessionToken)
	}
}

func TestConnectionServiceGetProfilesOmitsSecrets(t *testing.T) {
	t.Parallel()

	svc := newTestConnectionService(t)

	if _, err := svc.SaveProfile(sampleServiceProfile("prod")); err != nil {
		t.Fatalf("SaveProfile() returned error: %v", err)
	}

	dtos, err := svc.GetProfiles()
	if err != nil {
		t.Fatalf("GetProfiles() returned error: %v", err)
	}

	if len(dtos) != 1 {
		t.Fatalf("GetProfiles() returned %d profiles, want 1", len(dtos))
	}
	if dtos[0].Name != "prod" {
		t.Errorf("Name = %q, want %q", dtos[0].Name, "prod")
	}
}

func TestConnectionServiceGetProfileDecryptsSecrets(t *testing.T) {
	t.Parallel()

	svc := newTestConnectionService(t)

	saved, err := svc.SaveProfile(sampleServiceProfile("prod"))
	if err != nil {
		t.Fatalf("SaveProfile() returned error: %v", err)
	}

	got, err := svc.GetProfile(saved.ID)
	if err != nil {
		t.Fatalf("GetProfile(%d) returned error: %v", saved.ID, err)
	}

	if got.AccessKeyID != "AKIAEXAMPLE" {
		t.Errorf("AccessKeyID = %q, want %q", got.AccessKeyID, "AKIAEXAMPLE")
	}
	if got.SecretAccessKey != "plaintext-secret" {
		t.Errorf("SecretAccessKey = %q, want %q (decrypted)", got.SecretAccessKey, "plaintext-secret")
	}
	if got.SessionToken != "plaintext-session-token" {
		t.Errorf("SessionToken = %q, want %q (decrypted)", got.SessionToken, "plaintext-session-token")
	}
}

func TestConnectionServiceGetProfileNoSessionToken(t *testing.T) {
	t.Parallel()

	svc := newTestConnectionService(t)

	p := sampleServiceProfile("no-token")
	p.SessionToken = ""

	saved, err := svc.SaveProfile(p)
	if err != nil {
		t.Fatalf("SaveProfile() returned error: %v", err)
	}

	got, err := svc.GetProfile(saved.ID)
	if err != nil {
		t.Fatalf("GetProfile(%d) returned error: %v", saved.ID, err)
	}

	if got.SessionToken != "" {
		t.Errorf("SessionToken = %q, want empty", got.SessionToken)
	}
}

func TestConnectionServiceGetProfileNotFound(t *testing.T) {
	t.Parallel()

	svc := newTestConnectionService(t)

	_, err := svc.GetProfile(999)
	if !errors.Is(err, domain.ErrProfileNotFound) {
		t.Fatalf("GetProfile() error = %v, want errors.Is(_, domain.ErrProfileNotFound)", err)
	}
}

func TestConnectionServiceUpdateProfile(t *testing.T) {
	t.Parallel()

	svc := newTestConnectionService(t)

	saved, err := svc.SaveProfile(sampleServiceProfile("prod"))
	if err != nil {
		t.Fatalf("SaveProfile() returned error: %v", err)
	}

	toUpdate, err := svc.GetProfile(saved.ID)
	if err != nil {
		t.Fatalf("GetProfile(%d) returned error: %v", saved.ID, err)
	}

	toUpdate.Region = "eu-west-1"
	toUpdate.SecretAccessKey = "new-plaintext-secret"
	toUpdate.SessionToken = "new-plaintext-session-token"

	updated, err := svc.SaveProfile(toUpdate)
	if err != nil {
		t.Fatalf("SaveProfile() (update) returned error: %v", err)
	}

	if updated.ID != saved.ID {
		t.Errorf("updated.ID = %d, want %d", updated.ID, saved.ID)
	}
	if updated.Region != "eu-west-1" {
		t.Errorf("Region = %q, want %q", updated.Region, "eu-west-1")
	}

	got, err := svc.GetProfile(saved.ID)
	if err != nil {
		t.Fatalf("GetProfile() after update returned error: %v", err)
	}
	if got.SecretAccessKey != "new-plaintext-secret" {
		t.Errorf("SecretAccessKey = %q, want %q", got.SecretAccessKey, "new-plaintext-secret")
	}
	if got.SessionToken != "new-plaintext-session-token" {
		t.Errorf("SessionToken = %q, want %q", got.SessionToken, "new-plaintext-session-token")
	}
}

func TestConnectionServiceUpdateProfileKeepsSecretWhenBlank(t *testing.T) {
	t.Parallel()

	svc := newTestConnectionService(t)

	saved, err := svc.SaveProfile(sampleServiceProfile("prod"))
	if err != nil {
		t.Fatalf("SaveProfile() returned error: %v", err)
	}

	// Simulate an edit form that changes an unrelated field and submits a
	// blank SecretAccessKey, meaning "leave the secret unchanged".
	update := domain.Profile{
		ID:              saved.ID,
		Name:            saved.Name,
		EndpointURL:     saved.EndpointURL,
		Region:          "ap-southeast-1",
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "",
		SessionToken:    "plaintext-session-token",
		PathStyle:       saved.PathStyle,
		VerifySSL:       saved.VerifySSL,
	}

	updated, err := svc.SaveProfile(update)
	if err != nil {
		t.Fatalf("SaveProfile() (blank secret update) returned error: %v", err)
	}
	if updated.Region != "ap-southeast-1" {
		t.Errorf("Region = %q, want %q", updated.Region, "ap-southeast-1")
	}

	got, err := svc.GetProfile(saved.ID)
	if err != nil {
		t.Fatalf("GetProfile() after update returned error: %v", err)
	}
	if got.SecretAccessKey != "plaintext-secret" {
		t.Errorf("SecretAccessKey = %q, want %q (unchanged)", got.SecretAccessKey, "plaintext-secret")
	}
}

func TestConnectionServiceUpdateProfileClearsSessionTokenWhenBlank(t *testing.T) {
	t.Parallel()

	svc := newTestConnectionService(t)

	saved, err := svc.SaveProfile(sampleServiceProfile("prod"))
	if err != nil {
		t.Fatalf("SaveProfile() returned error: %v", err)
	}

	toUpdate, err := svc.GetProfile(saved.ID)
	if err != nil {
		t.Fatalf("GetProfile(%d) returned error: %v", saved.ID, err)
	}

	toUpdate.SessionToken = ""

	if _, err := svc.SaveProfile(toUpdate); err != nil {
		t.Fatalf("SaveProfile() (clear session token) returned error: %v", err)
	}

	got, err := svc.GetProfile(saved.ID)
	if err != nil {
		t.Fatalf("GetProfile() after update returned error: %v", err)
	}
	if got.SessionToken != "" {
		t.Errorf("SessionToken = %q, want empty (explicitly cleared)", got.SessionToken)
	}
}

func TestConnectionServiceDeleteProfile(t *testing.T) {
	t.Parallel()

	svc := newTestConnectionService(t)

	saved, err := svc.SaveProfile(sampleServiceProfile("prod"))
	if err != nil {
		t.Fatalf("SaveProfile() returned error: %v", err)
	}

	if err := svc.DeleteProfile(saved.ID); err != nil {
		t.Fatalf("DeleteProfile() returned error: %v", err)
	}

	_, err = svc.GetProfile(saved.ID)
	if !errors.Is(err, domain.ErrProfileNotFound) {
		t.Fatalf("GetProfile() after delete error = %v, want errors.Is(_, domain.ErrProfileNotFound)", err)
	}
}

func TestConnectionServiceDeleteProfileNotFound(t *testing.T) {
	t.Parallel()

	svc := newTestConnectionService(t)

	err := svc.DeleteProfile(999)
	if !errors.Is(err, domain.ErrProfileNotFound) {
		t.Fatalf("DeleteProfile() error = %v, want errors.Is(_, domain.ErrProfileNotFound)", err)
	}
}

func TestConnectionServiceSaveProfileDuplicateName(t *testing.T) {
	t.Parallel()

	svc := newTestConnectionService(t)

	if _, err := svc.SaveProfile(sampleServiceProfile("dup")); err != nil {
		t.Fatalf("first SaveProfile() returned error: %v", err)
	}

	_, err := svc.SaveProfile(sampleServiceProfile("dup"))
	if !errors.Is(err, domain.ErrDuplicateProfileName) {
		t.Fatalf("second SaveProfile() error = %v, want errors.Is(_, domain.ErrDuplicateProfileName)", err)
	}
}

func TestConnectionServiceSaveProfileInvalid(t *testing.T) {
	t.Parallel()

	svc := newTestConnectionService(t)

	p := sampleServiceProfile("bad")
	p.EndpointURL = "not-a-url"

	_, err := svc.SaveProfile(p)
	if !errors.Is(err, domain.ErrInvalidEndpoint) {
		t.Fatalf("SaveProfile() error = %v, want errors.Is(_, domain.ErrInvalidEndpoint)", err)
	}
}

func TestConnectionServiceTestConnectionUsesFormDataDirectly(t *testing.T) {
	t.Parallel()

	svc := newTestConnectionService(t)

	// A profile that has never been saved (ID == 0) and points at a host
	// with nothing listening: TestConnection must work purely off the
	// struct passed in, without any database round trip.
	p := sampleServiceProfile("unsaved")
	p.EndpointURL = "http://127.0.0.1:1"

	result, err := svc.TestConnection(p)
	if err != nil {
		t.Fatalf("TestConnection() returned Go error = %v, want nil", err)
	}
	if result.Success {
		t.Fatal("Success = true, want false (nothing listening on that port)")
	}
	if result.Category == "" {
		t.Error("Category is empty on failure")
	}
}
