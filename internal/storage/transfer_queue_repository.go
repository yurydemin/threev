package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"threev/internal/domain"
)

// TransferQueueRepository provides CRUD and lifecycle persistence for
// domain.TransferTask against the "transfer_queue" SQLite table
// (docs/02-tech-spec.md section 8.2).
type TransferQueueRepository struct {
	db *sql.DB
}

// NewTransferQueueRepository returns a TransferQueueRepository backed by db.
func NewTransferQueueRepository(db *sql.DB) *TransferQueueRepository {
	return &TransferQueueRepository{db: db}
}

// Create inserts a new transfer_queue row and returns t with ID, CreatedAt,
// and UpdatedAt populated from the database.
func (r *TransferQueueRepository) Create(ctx context.Context, t domain.TransferTask) (domain.TransferTask, error) {
	const query = `
INSERT INTO transfer_queue (
    profile_id, type, source_path, destination_path, status,
    total_bytes, transferred_bytes, error_message, multipart_upload_id,
    parts_completed, file_offset, priority
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	result, err := r.db.ExecContext(ctx, query,
		t.ProfileID, t.Type, t.SourcePath, t.DestinationPath, t.Status,
		t.TotalBytes, t.TransferredBytes, nullableString(t.ErrorMessage), nullableString(t.MultipartUploadID),
		nullableString(t.PartsCompleted), t.FileOffset, t.Priority,
	)
	if err != nil {
		return domain.TransferTask{}, fmt.Errorf("create transfer task: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return domain.TransferTask{}, fmt.Errorf("create transfer task: read last insert id: %w", err)
	}

	return r.GetByID(ctx, id)
}

// GetByID returns the transfer task with the given id, or
// domain.ErrTransferTaskNotFound if no such task exists.
func (r *TransferQueueRepository) GetByID(ctx context.Context, id int64) (domain.TransferTask, error) {
	const query = `
SELECT id, profile_id, type, source_path, destination_path, status,
       total_bytes, transferred_bytes, error_message, multipart_upload_id,
       parts_completed, file_offset, priority, created_at, updated_at
FROM transfer_queue
WHERE id = ?`

	row := r.db.QueryRowContext(ctx, query, id)

	t, err := scanTransferTask(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.TransferTask{}, fmt.Errorf("get transfer task %d: %w", id, domain.ErrTransferTaskNotFound)
		}

		return domain.TransferTask{}, fmt.Errorf("get transfer task %d: %w", id, err)
	}

	return t, nil
}

// GetAll returns every transfer task, ordered by priority (ascending, lower
// runs first) and then by creation time (FR-QUEUE-003).
func (r *TransferQueueRepository) GetAll(ctx context.Context) ([]domain.TransferTask, error) {
	const query = `
SELECT id, profile_id, type, source_path, destination_path, status,
       total_bytes, transferred_bytes, error_message, multipart_upload_id,
       parts_completed, file_offset, priority, created_at, updated_at
FROM transfer_queue
ORDER BY priority ASC, created_at ASC`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get all transfer tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]domain.TransferTask, 0)

	for rows.Next() {
		t, err := scanTransferTask(rows)
		if err != nil {
			return nil, fmt.Errorf("get all transfer tasks: scan row: %w", err)
		}

		tasks = append(tasks, t)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get all transfer tasks: iterate rows: %w", err)
	}

	return tasks, nil
}

// UpdateStatus sets the status and error_message of the transfer task
// identified by id. Pass an empty errMsg to clear any previously recorded
// error. Returns domain.ErrTransferTaskNotFound if no such task exists.
func (r *TransferQueueRepository) UpdateStatus(ctx context.Context, id int64, status, errMsg string) error {
	const query = `
UPDATE transfer_queue
SET status = ?, error_message = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?`

	result, err := r.db.ExecContext(ctx, query, status, nullableString(errMsg), id)
	if err != nil {
		return fmt.Errorf("update transfer task %d status: %w", id, err)
	}

	return requireRowAffected(result, id, "update transfer task status")
}

// UpdateProgress sets the transferred_bytes and total_bytes of the transfer
// task identified by id. Returns domain.ErrTransferTaskNotFound if no such
// task exists.
func (r *TransferQueueRepository) UpdateProgress(ctx context.Context, id, transferred, total int64) error {
	const query = `
UPDATE transfer_queue
SET transferred_bytes = ?, total_bytes = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?`

	result, err := r.db.ExecContext(ctx, query, transferred, total, id)
	if err != nil {
		return fmt.Errorf("update transfer task %d progress: %w", id, err)
	}

	return requireRowAffected(result, id, "update transfer task progress")
}

// UpdateMultipartUploadID sets the multipart_upload_id of the transfer task
// identified by id (recorded once CreateMultipartUpload succeeds, so a
// later resume can ListParts against it). Returns
// domain.ErrTransferTaskNotFound if no such task exists.
func (r *TransferQueueRepository) UpdateMultipartUploadID(ctx context.Context, id int64, uploadID string) error {
	const query = `
UPDATE transfer_queue
SET multipart_upload_id = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?`

	result, err := r.db.ExecContext(ctx, query, nullableString(uploadID), id)
	if err != nil {
		return fmt.Errorf("update transfer task %d multipart upload id: %w", id, err)
	}

	return requireRowAffected(result, id, "update transfer task multipart upload id")
}

// UpdatePriority sets the priority of the transfer task identified by id
// (FR-QUEUE-003, reordering the queue). Returns
// domain.ErrTransferTaskNotFound if no such task exists.
func (r *TransferQueueRepository) UpdatePriority(ctx context.Context, id int64, priority int) error {
	const query = `
UPDATE transfer_queue
SET priority = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?`

	result, err := r.db.ExecContext(ctx, query, priority, id)
	if err != nil {
		return fmt.Errorf("update transfer task %d priority: %w", id, err)
	}

	return requireRowAffected(result, id, "update transfer task priority")
}

// Delete removes the transfer task with the given id. Returns
// domain.ErrTransferTaskNotFound if no such task exists.
func (r *TransferQueueRepository) Delete(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM transfer_queue WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete transfer task %d: %w", id, err)
	}

	return requireRowAffected(result, id, "delete transfer task")
}

// MoveToHistory atomically inserts historyEntry into transfer_history and
// deletes the transfer_queue row identified by id, in a single transaction:
// either both happen, or (on any error, including id not matching an
// existing queue row) neither does. Called once a task reaches a terminal
// status (Completed/Failed/Cancelled) - Paused tasks remain in the queue.
func (r *TransferQueueRepository) MoveToHistory(ctx context.Context, id int64, historyEntry domain.TransferHistoryEntry) (err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("move transfer task %d to history: begin transaction: %w", id, err)
	}
	defer func() {
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				err = errors.Join(err, fmt.Errorf("rollback: %w", rbErr))
			}
		}
	}()

	const insertQuery = `
INSERT INTO transfer_history (
    queue_id, profile_id, type, source_path, destination_path,
    total_bytes, status, completed_at, error_message
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	if _, err := tx.ExecContext(ctx, insertQuery,
		historyEntry.QueueID, historyEntry.ProfileID, historyEntry.Type,
		historyEntry.SourcePath, historyEntry.DestinationPath, historyEntry.TotalBytes,
		historyEntry.Status, historyEntry.CompletedAt, nullableString(historyEntry.ErrorMessage),
	); err != nil {
		return fmt.Errorf("move transfer task %d to history: insert history row: %w", id, err)
	}

	result, err := tx.ExecContext(ctx, `DELETE FROM transfer_queue WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("move transfer task %d to history: delete queue row: %w", id, err)
	}

	if err := requireRowAffected(result, id, "move transfer task to history"); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("move transfer task %d to history: commit: %w", id, err)
	}

	return nil
}

// requireRowAffected returns domain.ErrTransferTaskNotFound (wrapped with
// op and id context) if result reports zero affected rows, surfacing a
// sql.Result-reading error unchanged otherwise.
func requireRowAffected(result sql.Result, id int64, op string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s %d: read rows affected: %w", op, id, err)
	}

	if affected == 0 {
		return fmt.Errorf("%s %d: %w", op, id, domain.ErrTransferTaskNotFound)
	}

	return nil
}

// scanTransferTask scans a single transfer_queue row (matching the column
// order used by GetByID/GetAll) into a domain.TransferTask.
func scanTransferTask(s rowScanner) (domain.TransferTask, error) {
	var (
		t                 domain.TransferTask
		errMsg            sql.NullString
		multipartUploadID sql.NullString
		partsCompleted    sql.NullString
	)

	if err := s.Scan(
		&t.ID, &t.ProfileID, &t.Type, &t.SourcePath, &t.DestinationPath, &t.Status,
		&t.TotalBytes, &t.TransferredBytes, &errMsg, &multipartUploadID,
		&partsCompleted, &t.FileOffset, &t.Priority, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return domain.TransferTask{}, err
	}

	t.ErrorMessage = errMsg.String
	t.MultipartUploadID = multipartUploadID.String
	t.PartsCompleted = partsCompleted.String

	return t, nil
}
