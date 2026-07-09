package storage

import (
	"context"
	"database/sql"
	"fmt"

	"threev/internal/domain"
)

// TransferHistoryRepository provides append/read/clear persistence for
// domain.TransferHistoryEntry against the "transfer_history" SQLite table
// (docs/02-tech-spec.md section 8.3, FR-QUEUE-006).
type TransferHistoryRepository struct {
	db *sql.DB
}

// NewTransferHistoryRepository returns a TransferHistoryRepository backed by
// db.
func NewTransferHistoryRepository(db *sql.DB) *TransferHistoryRepository {
	return &TransferHistoryRepository{db: db}
}

// Create inserts a new transfer_history row and returns entry with ID
// populated from the database. Unlike TransferQueueRepository.Create, the
// row is not re-read after insert: entry is expected to already carry every
// column value the caller wants persisted (notably CompletedAt, which has
// no database-side default).
func (r *TransferHistoryRepository) Create(ctx context.Context, entry domain.TransferHistoryEntry) (domain.TransferHistoryEntry, error) {
	const query = `
INSERT INTO transfer_history (
    queue_id, profile_id, type, source_path, destination_path,
    total_bytes, status, completed_at, error_message
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	result, err := r.db.ExecContext(ctx, query,
		entry.QueueID, entry.ProfileID, entry.Type, entry.SourcePath, entry.DestinationPath,
		entry.TotalBytes, entry.Status, entry.CompletedAt, nullableString(entry.ErrorMessage),
	)
	if err != nil {
		return domain.TransferHistoryEntry{}, fmt.Errorf("create transfer history entry: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return domain.TransferHistoryEntry{}, fmt.Errorf("create transfer history entry: read last insert id: %w", err)
	}

	entry.ID = id

	return entry, nil
}

// GetAll returns up to limit transfer history entries, most recently
// completed first.
func (r *TransferHistoryRepository) GetAll(ctx context.Context, limit int) ([]domain.TransferHistoryEntry, error) {
	const query = `
SELECT id, queue_id, profile_id, type, source_path, destination_path,
       total_bytes, status, completed_at, error_message
FROM transfer_history
ORDER BY completed_at DESC
LIMIT ?`

	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("get all transfer history entries: %w", err)
	}
	defer rows.Close()

	entries := make([]domain.TransferHistoryEntry, 0)

	for rows.Next() {
		entry, err := scanTransferHistoryEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("get all transfer history entries: scan row: %w", err)
		}

		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get all transfer history entries: iterate rows: %w", err)
	}

	return entries, nil
}

// Clear deletes every transfer_history row (FR-SET-002, "Очистка истории").
func (r *TransferHistoryRepository) Clear(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM transfer_history`); err != nil {
		return fmt.Errorf("clear transfer history: %w", err)
	}

	return nil
}

// scanTransferHistoryEntry scans a single transfer_history row (matching
// the column order used by GetAll) into a domain.TransferHistoryEntry.
func scanTransferHistoryEntry(s rowScanner) (domain.TransferHistoryEntry, error) {
	var (
		entry  domain.TransferHistoryEntry
		errMsg sql.NullString
	)

	if err := s.Scan(
		&entry.ID, &entry.QueueID, &entry.ProfileID, &entry.Type, &entry.SourcePath,
		&entry.DestinationPath, &entry.TotalBytes, &entry.Status, &entry.CompletedAt, &errMsg,
	); err != nil {
		return domain.TransferHistoryEntry{}, err
	}

	entry.ErrorMessage = errMsg.String

	return entry, nil
}
