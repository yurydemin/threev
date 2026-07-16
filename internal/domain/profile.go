package domain

import "time"

// Profile is the full connection profile model, mapping 1:1 to the
// "profiles" table (docs/02-tech-spec.md section 8.1). It includes
// credential fields (AccessKeyID, SecretAccessKey, SessionToken); in
// storage these are kept encrypted, but by the time a Profile value
// reaches application code that needs to use the credentials (e.g. to
// build an S3 client) they are expected to hold plaintext values -
// encryption/decryption is the Service layer's responsibility, not
// domain's or storage's.
type Profile struct {
	ID              int64
	Name            string
	EndpointURL     string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	PathStyle       bool
	VerifySSL       bool
	CustomHeaders   map[string]string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ProfileDTO is the connection profile representation safe to send to the
// frontend for list views: it deliberately omits AccessKeyID,
// SecretAccessKey, and SessionToken. Per docs/03-ux-ui-spec.md section 5.2,
// the connections list screen only needs identity/status/endpoint fields.
type ProfileDTO struct {
	ID          int64
	Name        string
	EndpointURL string
	Region      string
	PathStyle   bool
	VerifySSL   bool
	CreatedAt   time.Time
	UpdatedAt   time.Time

	// HasCredentials reports whether this profile currently has both an
	// AccessKeyID and a SecretAccessKey set. This is a safe, non-secret
	// derived signal - the mere presence/absence of credentials is not
	// sensitive, only their values are (which is exactly what the rest of
	// ProfileDTO already omits) - so the frontend can render a "requires
	// credentials" badge on a connection card without ever receiving
	// AccessKeyID itself.
	//
	// Under normal use this is always true: every profile that has gone
	// through the connection.ConnectionService.SaveProfile path had
	// AccessKeyID/SecretAccessKey validated as non-empty by
	// connection.ValidateProfile. The one exception is a profile created by
	// connection.ConnectionService.ImportProfiles (Блок G, profile export/
	// import), which deliberately creates profiles with blank credential
	// fields - HasCredentials is false for such a profile until the user
	// edits it and supplies real credentials.
	HasCredentials bool
}

// ToDTO converts a Profile into its secret-free ProfileDTO representation.
func (p Profile) ToDTO() ProfileDTO {
	return ProfileDTO{
		ID:             p.ID,
		Name:           p.Name,
		EndpointURL:    p.EndpointURL,
		Region:         p.Region,
		PathStyle:      p.PathStyle,
		VerifySSL:      p.VerifySSL,
		CreatedAt:      p.CreatedAt,
		UpdatedAt:      p.UpdatedAt,
		HasCredentials: p.AccessKeyID != "" && p.SecretAccessKey != "",
	}
}

// ConnectionTestResult reports the outcome of an explicit TestConnection
// call (docs/02-tech-spec.md section 9.1). It is designed as a terminal,
// UI-facing value rather than a Go error: TestConnection always returns
// one of these, describing success or the specific way the connection
// attempt failed.
type ConnectionTestResult struct {
	// Success is true if the connection and credentials were verified.
	Success bool
	// Message is a short, human-readable summary suitable for direct
	// display (default backend copy; frontend may localize/override it).
	Message string
	// Detail carries technical information (e.g. the underlying error
	// text) suitable for a "copy technical details" affordance
	// (docs/03-ux-ui-spec.md UX-007). Empty on success.
	Detail string
	// Category classifies the failure for UI iconography/handling:
	// "network", "auth", "tls", "timeout", or "unknown". Empty on success.
	Category string
}
