package domain

import "time"

// Favorite is a bookmarked bucket/prefix location within a connection
// profile, mapping 1:1 to the "favorites" table. A favorite is uniquely
// identified by (ProfileID, Bucket, Prefix); it deliberately has no
// label/name field - display text (bucket, or bucket/prefix) is always
// computed by the frontend from Bucket/Prefix, never stored.
type Favorite struct {
	ID int64
	// ProfileID identifies the owning connection profile. Deleting that
	// profile cascade-deletes this favorite (ON DELETE CASCADE on the
	// favorites.profile_id foreign key).
	ProfileID int64
	// ProfileName is the owning profile's Name, populated by
	// FavoriteRepository's read queries via a JOIN against profiles.name
	// for convenient display (e.g. grouping the favorites list by profile
	// in the Sidebar). It is NOT a persisted column on the favorites table
	// itself.
	ProfileName string
	Bucket      string
	Prefix      string
	CreatedAt   time.Time
}
