package domain

import "errors"

// Sentinel errors returned by the storage and connection layers. Callers
// should compare against these with errors.Is rather than string matching.
var (
	// ErrProfileNotFound is returned when a profile lookup by ID finds no
	// matching row.
	ErrProfileNotFound = errors.New("profile not found")

	// ErrDuplicateProfileName is returned when a profile is created or
	// renamed to a name that is already in use by another profile.
	ErrDuplicateProfileName = errors.New("a profile with this name already exists")

	// ErrInvalidEndpoint is returned when a profile's EndpointURL is not a
	// valid absolute http:// or https:// URL.
	ErrInvalidEndpoint = errors.New("invalid endpoint URL")

	// ErrInvalidProfileName is returned when a profile's Name is empty.
	ErrInvalidProfileName = errors.New("profile name must not be empty")

	// ErrTransferTaskNotFound is returned when a transfer_queue lookup by ID
	// finds no matching row.
	ErrTransferTaskNotFound = errors.New("transfer task not found")
)
