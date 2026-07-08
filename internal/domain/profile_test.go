package domain

import (
	"reflect"
	"testing"
	"time"
)

func TestProfileToDTOExcludesSecrets(t *testing.T) {
	t.Parallel()

	p := Profile{
		ID:              1,
		Name:            "prod-s3",
		EndpointURL:     "https://s3.example.com",
		Region:          "us-east-1",
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "supersecret",
		SessionToken:    "sessiontoken",
		PathStyle:       true,
		VerifySSL:       false,
		CustomHeaders:   map[string]string{"X-Custom": "value"},
		CreatedAt:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:       time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}

	dto := p.ToDTO()

	if dto.ID != p.ID {
		t.Errorf("ID = %d, want %d", dto.ID, p.ID)
	}
	if dto.Name != p.Name {
		t.Errorf("Name = %q, want %q", dto.Name, p.Name)
	}
	if dto.EndpointURL != p.EndpointURL {
		t.Errorf("EndpointURL = %q, want %q", dto.EndpointURL, p.EndpointURL)
	}
	if dto.Region != p.Region {
		t.Errorf("Region = %q, want %q", dto.Region, p.Region)
	}
	if dto.PathStyle != p.PathStyle {
		t.Errorf("PathStyle = %v, want %v", dto.PathStyle, p.PathStyle)
	}
	if dto.VerifySSL != p.VerifySSL {
		t.Errorf("VerifySSL = %v, want %v", dto.VerifySSL, p.VerifySSL)
	}
	if !dto.CreatedAt.Equal(p.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", dto.CreatedAt, p.CreatedAt)
	}
	if !dto.UpdatedAt.Equal(p.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want %v", dto.UpdatedAt, p.UpdatedAt)
	}
}

// TestProfileDTOHasNoSecretFields is a compile-time-ish guard: it uses
// reflection to fail loudly if a future edit ever adds a field to
// ProfileDTO whose name suggests it carries a secret.
func TestProfileDTOHasNoSecretFields(t *testing.T) {
	t.Parallel()

	forbidden := map[string]bool{"AccessKeyID": true, "SecretAccessKey": true, "SessionToken": true}

	rt := reflect.TypeOf(ProfileDTO{})
	for i := range rt.NumField() {
		if forbidden[rt.Field(i).Name] {
			t.Errorf("ProfileDTO must not contain field %q", rt.Field(i).Name)
		}
	}
}
