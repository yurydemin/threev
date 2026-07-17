-- 0003_transfer_queue_zip_type.sql
-- Widens transfer_queue's "type" CHECK constraint to also allow
-- 'download_zip' (a whole folder/prefix downloaded as a single ZIP
-- archive, transfer.TransferService.QueueDownloadPrefixZip) alongside the
-- existing 'upload'/'download'. transfer_history's own "type" column has no
-- such CHECK constraint (0001_init.sql), so it needs no equivalent change.
--
-- SQLite has no ALTER TABLE ... ALTER COLUMN / DROP CONSTRAINT, so the
-- standard rebuild procedure is used instead: create the table under a new
-- name with the widened constraint (every other column/index/foreign key
-- unchanged from 0001_init.sql), copy every existing row across as-is, drop
-- the old table, and rename the new one into its place. No transfer_queue
-- row is modified by this migration - existing 'upload'/'download' rows
-- remain exactly as they were.

CREATE TABLE transfer_queue_new (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    profile_id          INTEGER NOT NULL,
    type                TEXT NOT NULL CHECK (type IN ('upload', 'download', 'download_zip')),
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

INSERT INTO transfer_queue_new
    (id, profile_id, type, source_path, destination_path, status, total_bytes,
     transferred_bytes, error_message, multipart_upload_id, parts_completed,
     file_offset, priority, created_at, updated_at)
SELECT
    id, profile_id, type, source_path, destination_path, status, total_bytes,
    transferred_bytes, error_message, multipart_upload_id, parts_completed,
    file_offset, priority, created_at, updated_at
FROM transfer_queue;

DROP TABLE transfer_queue;

ALTER TABLE transfer_queue_new RENAME TO transfer_queue;

CREATE INDEX idx_transfer_queue_status ON transfer_queue (status);
CREATE INDEX idx_transfer_queue_profile_id ON transfer_queue (profile_id);
