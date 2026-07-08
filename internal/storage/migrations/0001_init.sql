-- 0001_init.sql
-- Creates the initial schema: profiles, transfer_queue, transfer_history,
-- and settings, per docs/02-tech-spec.md section 8.

CREATE TABLE profiles (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    name              TEXT NOT NULL UNIQUE,
    endpoint_url      TEXT NOT NULL,
    region            TEXT NOT NULL DEFAULT 'us-east-1',
    access_key_id     TEXT NOT NULL,
    secret_access_key TEXT NOT NULL,
    session_token     TEXT,
    path_style        BOOLEAN DEFAULT 0,
    verify_ssl        BOOLEAN DEFAULT 1,
    custom_headers    TEXT, -- JSON
    created_at        TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at        TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE transfer_queue (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    profile_id          INTEGER NOT NULL,
    type                TEXT NOT NULL CHECK (type IN ('upload', 'download')),
    source_path         TEXT NOT NULL,
    destination_path    TEXT NOT NULL,
    status              TEXT NOT NULL CHECK (status IN ('pending', 'running', 'paused', 'completed', 'failed', 'cancelled')),
    total_bytes         INTEGER DEFAULT 0,
    transferred_bytes   INTEGER DEFAULT 0,
    error_message       TEXT,
    multipart_upload_id TEXT, -- for resuming uploads
    parts_completed     TEXT, -- JSON array of completed part numbers
    file_offset         INTEGER DEFAULT 0, -- for resuming downloads
    priority            INTEGER DEFAULT 0,
    created_at          TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (profile_id) REFERENCES profiles (id)
);

CREATE INDEX idx_transfer_queue_status ON transfer_queue (status);
CREATE INDEX idx_transfer_queue_profile_id ON transfer_queue (profile_id);

CREATE TABLE transfer_history (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    queue_id         INTEGER,
    profile_id       INTEGER NOT NULL,
    type             TEXT NOT NULL,
    source_path      TEXT NOT NULL,
    destination_path TEXT NOT NULL,
    total_bytes      INTEGER,
    status           TEXT,
    completed_at     TIMESTAMP,
    error_message    TEXT
);

CREATE INDEX idx_transfer_history_profile_id ON transfer_history (profile_id);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT
);
