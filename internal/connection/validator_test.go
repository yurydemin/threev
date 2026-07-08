package connection

import (
	"errors"
	"testing"

	"threev/internal/domain"
)

func validProfile() domain.Profile {
	return domain.Profile{
		Name:            "prod",
		EndpointURL:     "https://s3.example.com",
		Region:          "us-east-1",
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "supersecret",
	}
}

func TestValidateProfileValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(p *domain.Profile)
	}{
		{name: "minimal valid profile", mutate: func(p *domain.Profile) {}},
		{name: "http scheme", mutate: func(p *domain.Profile) { p.EndpointURL = "http://minio.local:9000" }},
		{name: "with port and path", mutate: func(p *domain.Profile) { p.EndpointURL = "https://s3.example.com:9000/" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := validProfile()
			tt.mutate(&p)

			if err := ValidateProfile(p); err != nil {
				t.Errorf("ValidateProfile() error = %v, want nil", err)
			}
		})
	}
}

func TestValidateProfileInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(p *domain.Profile)
		wantErr error // checked via errors.Is when non-nil
	}{
		{
			name:    "empty name",
			mutate:  func(p *domain.Profile) { p.Name = "" },
			wantErr: domain.ErrInvalidProfileName,
		},
		{
			name:    "empty endpoint",
			mutate:  func(p *domain.Profile) { p.EndpointURL = "" },
			wantErr: domain.ErrInvalidEndpoint,
		},
		{
			name:    "unparseable endpoint",
			mutate:  func(p *domain.Profile) { p.EndpointURL = "://not-a-url" },
			wantErr: domain.ErrInvalidEndpoint,
		},
		{
			name:    "unsupported scheme",
			mutate:  func(p *domain.Profile) { p.EndpointURL = "ftp://s3.example.com" },
			wantErr: domain.ErrInvalidEndpoint,
		},
		{
			name:    "missing host",
			mutate:  func(p *domain.Profile) { p.EndpointURL = "https://" },
			wantErr: domain.ErrInvalidEndpoint,
		},
		{
			name:   "empty access key id",
			mutate: func(p *domain.Profile) { p.AccessKeyID = "" },
		},
		{
			name:   "empty secret access key",
			mutate: func(p *domain.Profile) { p.SecretAccessKey = "" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := validProfile()
			tt.mutate(&p)

			err := ValidateProfile(p)
			if err == nil {
				t.Fatal("ValidateProfile() error = nil, want error")
			}

			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidateProfile() error = %v, want errors.Is(_, %v)", err, tt.wantErr)
			}
		})
	}
}
