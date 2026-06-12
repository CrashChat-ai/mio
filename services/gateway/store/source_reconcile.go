package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SourceReconcileCursor is the persisted status for one account/conversation
// history reconciliation cursor.
type SourceReconcileCursor struct {
	AccountID              uuid.UUID
	ChannelType            string
	ConversationExternalID string
	Cursor                 string
	LastSuccessAt          *time.Time
	LastError              string
	LastErrorAt            *time.Time
	UpdatedAt              time.Time
}

func GetSourceReconcileCursor(
	ctx context.Context,
	pool *pgxpool.Pool,
	accountID uuid.UUID,
	conversationExternalID string,
) (SourceReconcileCursor, bool, error) {
	const q = `
SELECT account_id, channel_type, conversation_external_id, cursor,
       last_success_at, last_error, last_error_at, updated_at
FROM source_reconcile_cursors
WHERE account_id = $1 AND conversation_external_id = $2`
	var row SourceReconcileCursor
	var lastError *string
	err := pool.QueryRow(ctx, q, accountID, conversationExternalID).Scan(
		&row.AccountID,
		&row.ChannelType,
		&row.ConversationExternalID,
		&row.Cursor,
		&row.LastSuccessAt,
		&lastError,
		&row.LastErrorAt,
		&row.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SourceReconcileCursor{}, false, nil
		}
		return SourceReconcileCursor{}, false, fmt.Errorf("store: get source reconcile cursor: %w", err)
	}
	if lastError != nil {
		row.LastError = *lastError
	}
	return row, true, nil
}

func RecordSourceReconcileSuccess(
	ctx context.Context,
	pool *pgxpool.Pool,
	accountID uuid.UUID,
	channelType, conversationExternalID, cursor string,
) error {
	const q = `
INSERT INTO source_reconcile_cursors (
  account_id, channel_type, conversation_external_id, cursor,
  last_success_at, last_error, last_error_at, updated_at
) VALUES ($1, $2, $3, $4, NOW(), NULL, NULL, NOW())
ON CONFLICT (account_id, conversation_external_id) DO UPDATE SET
  channel_type = EXCLUDED.channel_type,
  cursor = EXCLUDED.cursor,
  last_success_at = NOW(),
  last_error = NULL,
  last_error_at = NULL,
  updated_at = NOW()`
	if _, err := pool.Exec(ctx, q, accountID, channelType, conversationExternalID, cursor); err != nil {
		return fmt.Errorf("store: record source reconcile success: %w", err)
	}
	return nil
}

func RecordSourceReconcileError(
	ctx context.Context,
	pool *pgxpool.Pool,
	accountID uuid.UUID,
	channelType, conversationExternalID string,
	runErr error,
) error {
	errText := ""
	if runErr != nil {
		errText = runErr.Error()
	}
	const q = `
INSERT INTO source_reconcile_cursors (
  account_id, channel_type, conversation_external_id, last_error, last_error_at, updated_at
) VALUES ($1, $2, $3, $4, NOW(), NOW())
ON CONFLICT (account_id, conversation_external_id) DO UPDATE SET
  channel_type = EXCLUDED.channel_type,
  last_error = EXCLUDED.last_error,
  last_error_at = NOW(),
  updated_at = NOW()`
	if _, err := pool.Exec(ctx, q, accountID, channelType, conversationExternalID, errText); err != nil {
		return fmt.Errorf("store: record source reconcile error: %w", err)
	}
	return nil
}
