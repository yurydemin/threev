-- 0002_favorites.sql
-- Creates the favorites table: per-profile bookmarked bucket/prefix
-- locations, cascade-deleted along with their owning profile.

CREATE TABLE favorites (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    profile_id INTEGER NOT NULL,
    bucket     TEXT NOT NULL,
    prefix     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (profile_id) REFERENCES profiles (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX idx_favorites_profile_bucket_prefix ON favorites (profile_id, bucket, prefix);
