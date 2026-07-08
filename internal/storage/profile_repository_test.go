package storage

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"threev/internal/domain"
)

// newTestProfileRepository opens a fresh migrated SQLite database backed by
// a temporary file and returns a ProfileRepository over it.
func newTestProfileRepository(t *testing.T) *ProfileRepository {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "profiles_test.db")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB(%q) returned error: %v", dbPath, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close() returned error: %v", err)
		}
	})

	return NewProfileRepository(db)
}

func sampleProfile(name string) domain.Profile {
	return domain.Profile{
		Name:            name,
		EndpointURL:     "https://s3.example.com",
		Region:          "us-east-1",
		AccessKeyID:     "encrypted-access-key",
		SecretAccessKey: "encrypted-secret-key",
		SessionToken:    "encrypted-session-token",
		PathStyle:       true,
		VerifySSL:       false,
		CustomHeaders:   map[string]string{"X-Custom-Header": "value"},
	}
}

func TestProfileRepositoryCreateAndGetByID(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, sampleProfile("prod"))
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	if created.ID == 0 {
		t.Fatal("Create() did not populate ID")
	}
	if created.CreatedAt.IsZero() {
		t.Error("Create() did not populate CreatedAt")
	}
	if created.UpdatedAt.IsZero() {
		t.Error("Create() did not populate UpdatedAt")
	}

	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID(%d) returned error: %v", created.ID, err)
	}

	if got.Name != "prod" {
		t.Errorf("Name = %q, want %q", got.Name, "prod")
	}
	if got.EndpointURL != created.EndpointURL {
		t.Errorf("EndpointURL = %q, want %q", got.EndpointURL, created.EndpointURL)
	}
	if got.AccessKeyID != created.AccessKeyID {
		t.Errorf("AccessKeyID = %q, want %q", got.AccessKeyID, created.AccessKeyID)
	}
	if got.SecretAccessKey != created.SecretAccessKey {
		t.Errorf("SecretAccessKey = %q, want %q", got.SecretAccessKey, created.SecretAccessKey)
	}
	if got.SessionToken != created.SessionToken {
		t.Errorf("SessionToken = %q, want %q", got.SessionToken, created.SessionToken)
	}
	if got.PathStyle != true {
		t.Errorf("PathStyle = %v, want true", got.PathStyle)
	}
	if got.VerifySSL != false {
		t.Errorf("VerifySSL = %v, want false", got.VerifySSL)
	}
	if !reflect.DeepEqual(got.CustomHeaders, map[string]string{"X-Custom-Header": "value"}) {
		t.Errorf("CustomHeaders = %#v, want %#v", got.CustomHeaders, map[string]string{"X-Custom-Header": "value"})
	}
}

func TestProfileRepositoryCreateWithoutOptionalFields(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	p := sampleProfile("minimal")
	p.SessionToken = ""
	p.CustomHeaders = nil

	created, err := repo.Create(ctx, p)
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID(%d) returned error: %v", created.ID, err)
	}

	if got.SessionToken != "" {
		t.Errorf("SessionToken = %q, want empty", got.SessionToken)
	}
	if got.CustomHeaders != nil {
		t.Errorf("CustomHeaders = %#v, want nil", got.CustomHeaders)
	}
}

func TestProfileRepositoryGetByIDNotFound(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, 999)
	if !errors.Is(err, domain.ErrProfileNotFound) {
		t.Fatalf("GetByID() error = %v, want errors.Is(_, domain.ErrProfileNotFound)", err)
	}
}

func TestProfileRepositoryGetAllOrderedByName(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	for _, name := range []string{"charlie", "alpha", "bravo"} {
		if _, err := repo.Create(ctx, sampleProfile(name)); err != nil {
			t.Fatalf("Create(%q) returned error: %v", name, err)
		}
	}

	all, err := repo.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll() returned error: %v", err)
	}

	if len(all) != 3 {
		t.Fatalf("GetAll() returned %d profiles, want 3", len(all))
	}

	wantOrder := []string{"alpha", "bravo", "charlie"}
	for i, want := range wantOrder {
		if all[i].Name != want {
			t.Errorf("GetAll()[%d].Name = %q, want %q", i, all[i].Name, want)
		}
	}
}

func TestProfileRepositoryGetAllEmpty(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	all, err := repo.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll() returned error: %v", err)
	}

	if len(all) != 0 {
		t.Fatalf("GetAll() returned %d profiles, want 0", len(all))
	}
}

func TestProfileRepositoryUpdate(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, sampleProfile("to-update"))
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	created.Name = "updated-name"
	created.Region = "eu-west-1"
	created.PathStyle = false
	created.VerifySSL = true

	updated, err := repo.Update(ctx, created)
	if err != nil {
		t.Fatalf("Update() returned error: %v", err)
	}

	if updated.Name != "updated-name" {
		t.Errorf("Name = %q, want %q", updated.Name, "updated-name")
	}
	if updated.Region != "eu-west-1" {
		t.Errorf("Region = %q, want %q", updated.Region, "eu-west-1")
	}
	if updated.PathStyle != false {
		t.Errorf("PathStyle = %v, want false", updated.PathStyle)
	}
	if updated.VerifySSL != true {
		t.Errorf("VerifySSL = %v, want true", updated.VerifySSL)
	}
	if updated.UpdatedAt.IsZero() {
		t.Error("Update() did not populate UpdatedAt")
	}

	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID() returned error: %v", err)
	}
	if got.Name != "updated-name" {
		t.Errorf("persisted Name = %q, want %q", got.Name, "updated-name")
	}
}

func TestProfileRepositoryUpdateNotFound(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	p := sampleProfile("ghost")
	p.ID = 999

	_, err := repo.Update(ctx, p)
	if !errors.Is(err, domain.ErrProfileNotFound) {
		t.Fatalf("Update() error = %v, want errors.Is(_, domain.ErrProfileNotFound)", err)
	}
}

func TestProfileRepositoryDelete(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, sampleProfile("to-delete"))
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete() returned error: %v", err)
	}

	_, err = repo.GetByID(ctx, created.ID)
	if !errors.Is(err, domain.ErrProfileNotFound) {
		t.Fatalf("GetByID() after Delete() error = %v, want errors.Is(_, domain.ErrProfileNotFound)", err)
	}
}

func TestProfileRepositoryDeleteNotFound(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	err := repo.Delete(ctx, 999)
	if !errors.Is(err, domain.ErrProfileNotFound) {
		t.Fatalf("Delete() error = %v, want errors.Is(_, domain.ErrProfileNotFound)", err)
	}
}

func TestProfileRepositoryCreateDuplicateName(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, sampleProfile("dup")); err != nil {
		t.Fatalf("first Create() returned error: %v", err)
	}

	_, err := repo.Create(ctx, sampleProfile("dup"))
	if !errors.Is(err, domain.ErrDuplicateProfileName) {
		t.Fatalf("second Create() error = %v, want errors.Is(_, domain.ErrDuplicateProfileName)", err)
	}
}

func TestProfileRepositoryUpdateDuplicateName(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, sampleProfile("first")); err != nil {
		t.Fatalf("Create(first) returned error: %v", err)
	}

	second, err := repo.Create(ctx, sampleProfile("second"))
	if err != nil {
		t.Fatalf("Create(second) returned error: %v", err)
	}

	second.Name = "first"

	_, err = repo.Update(ctx, second)
	if !errors.Is(err, domain.ErrDuplicateProfileName) {
		t.Fatalf("Update() error = %v, want errors.Is(_, domain.ErrDuplicateProfileName)", err)
	}
}

func TestProfileRepositoryExistsByName(t *testing.T) {
	t.Parallel()

	repo := newTestProfileRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, sampleProfile("existing"))
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	tests := []struct {
		name      string
		checkName string
		excludeID int64
		want      bool
	}{
		{name: "new name does not exist", checkName: "brand-new", excludeID: 0, want: false},
		{name: "existing name without exclusion", checkName: "existing", excludeID: 0, want: true},
		{name: "existing name excluding itself", checkName: "existing", excludeID: created.ID, want: false},
		{name: "existing name excluding a different id", checkName: "existing", excludeID: created.ID + 1, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := repo.ExistsByName(ctx, tt.checkName, tt.excludeID)
			if err != nil {
				t.Fatalf("ExistsByName() returned error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ExistsByName(%q, %d) = %v, want %v", tt.checkName, tt.excludeID, got, tt.want)
			}
		})
	}
}
