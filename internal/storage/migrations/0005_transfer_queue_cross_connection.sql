-- 0005_transfer_queue_cross_connection.sql
-- Widens transfer_queue's "type" CHECK constraint to also allow
-- 'copy_cross' (a copy/move of an S3 object between two DIFFERENT saved
-- connection profiles - via a local staging file, since a single
-- CopyObject cannot span two different S3 endpoints - see
-- transfer.TransferService.QueueCopyBetweenProfiles/
-- runCrossConnectionCopyTask), alongside the existing
-- 'upload'/'download'/'download_zip'. It also adds the two columns a
-- 'copy_cross' task needs beyond every other type:
--
--   - dest_profile_id: the destination connection profile (the source
--     profile is already carried by the existing profile_id column - see
--     domain.TransferTask.DestProfileID's own doc comment). Nullable and
--     with no FOREIGN KEY of its own for the same reason profile_id's
--     existing FOREIGN KEY has no ON DELETE CASCADE (see
--     TransferService.CancelTasksForProfile's doc comment): a second
--     REFERENCES profiles(id) here would just as readily block deleting a
--     profile that is still a copy_cross task's DESTINATION, and this
--     migration is not the place to revisit that existing, already-shipped
--     design decision.
--   - is_move: distinguishes a copy_cross COPY from a copy_cross MOVE (the
--     source object is deleted once the upload phase confirms success -
--     see runCrossConnectionCopyTask's doc comment). NOT NULL DEFAULT 0
--     (copy), since every other existing task type has no notion of
--     "move" at all.
--
-- SQLite has no ALTER TABLE ... ALTER COLUMN / DROP CONSTRAINT, so the
-- standard rebuild procedure already used by
-- 0003_transfer_queue_zip_type.sql is repeated here: create the table under
-- a new name with the widened constraint and the two new columns, copy
-- every existing row across (the two new columns come out NULL/0 for every
-- pre-existing row, exactly matching what a real, non-copy_cross task
-- would have anyway), drop the old table, and rename the new one into its
-- place.

CREATE TABLE transfer_queue_new (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    profile_id          INTEGER NOT NULL,
    dest_profile_id     INTEGER, -- only set for 'copy_cross' tasks; NULL for every other type
    type                TEXT NOT NULL CHECK (type IN ('upload', 'download', 'download_zip', 'copy_cross')),
    source_path         TEXT NOT NULL,
    destination_path    TEXT NOT NULL,
    status              TEXT NOT NULL CHECK (status IN ('pending', 'running', 'paused', 'completed', 'failed', 'cancelled')),
    total_bytes         INTEGER DEFAULT 0,
    transferred_bytes   INTEGER DEFAULT 0,
    error_message       TEXT,
    multipart_upload_id TEXT, -- for resuming uploads (and a copy_cross task's own upload phase)
    parts_completed     TEXT, -- JSON array of completed part numbers
    file_offset         INTEGER DEFAULT 0, -- for resuming downloads
    is_move             BOOLEAN NOT NULL DEFAULT 0, -- only meaningful for 'copy_cross' tasks: 0 = copy, 1 = move (delete source after a confirmed upload)
    priority            INTEGER DEFAULT 0,
    created_at          TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (profile_id) REFERENCES profiles (id)
);

INSERT INTO transfer_queue_new
    (id, profile_id, dest_profile_id, type, source_path, destination_path, status, total_bytes,
     transferred_bytes, error_message, multipart_upload_id, parts_completed,
     file_offset, is_move, priority, created_at, updated_at)
SELECT
    id, profile_id, NULL, type, source_path, destination_path, status, total_bytes,
    transferred_bytes, error_message, multipart_upload_id, parts_completed,
    file_offset, 0, priority, created_at, updated_at
FROM transfer_queue;

DROP TABLE transfer_queue;

ALTER TABLE transfer_queue_new RENAME TO transfer_queue;

CREATE INDEX idx_transfer_queue_status ON transfer_queue (status);
CREATE INDEX idx_transfer_queue_profile_id ON transfer_queue (profile_id);
