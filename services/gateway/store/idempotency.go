package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/crashchat-ai/mio/pkg/channels"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EnsureUniqueMessage inserts a message row idempotently.
// Returns (id, fresh=true) on first insert; (id, fresh=false) if
// (account_id, source_message_id) already exists.
//
// Idempotency address is (account_id, source_message_id) per arch contract.
// NEVER (channel_type, source_message_id) — that breaks for tenants with two
// installs of the same channel.
func EnsureUniqueMessage(
	ctx context.Context,
	pool *pgxpool.Pool,
	id uuid.UUID,
	tenantID, accountID, conversationID string,
	threadRootMessageID *string,
	sourceMessageID, senderExternalID, text string,
	attributes map[string]string,
) (msgID uuid.UUID, fresh bool, err error) {
	// Convert attributes map to JSONB-compatible string.
	attrsJSON, err := marshalAttrs(attributes)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("store: ensure_unique_message attrs: %w", err)
	}

	const q = `
INSERT INTO messages (
  id, tenant_id, account_id, conversation_id,
  thread_root_message_id, source_message_id,
  sender_external_id, text, attributes
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (account_id, source_message_id) DO NOTHING
RETURNING id`

	row := pool.QueryRow(ctx, q,
		id, tenantID, accountID, conversationID,
		threadRootMessageID, sourceMessageID,
		senderExternalID, text, attrsJSON,
	)
	var returnedID uuid.UUID
	err = row.Scan(&returnedID)
	if err == nil {
		// Inserted fresh row.
		return returnedID, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, fmt.Errorf("store: ensure_unique_message insert: %w", err)
	}

	// Conflict fired (DO NOTHING returned 0 rows). Fetch the existing id.
	const sel = `SELECT id FROM messages WHERE account_id = $1 AND source_message_id = $2`
	if err2 := pool.QueryRow(ctx, sel, accountID, sourceMessageID).Scan(&returnedID); err2 != nil {
		return uuid.Nil, false, fmt.Errorf("store: ensure_unique_message select after conflict: %w", err2)
	}
	return returnedID, false, nil
}

// FindMessageBySource resolves a previously captured platform message id into
// mio's message id and effective thread root.
func FindMessageBySource(
	ctx context.Context,
	pool *pgxpool.Pool,
	accountID, sourceMessageID string,
) (channels.MessageRef, bool, error) {
	const q = `
SELECT id, COALESCE(thread_root_message_id, id)
FROM messages
WHERE account_id = $1 AND source_message_id = $2`

	var ref channels.MessageRef
	err := pool.QueryRow(ctx, q, accountID, sourceMessageID).Scan(
		&ref.ID,
		&ref.ThreadRootMessageID,
	)
	if err == nil {
		return ref, true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return channels.MessageRef{}, false, nil
	}
	return channels.MessageRef{}, false, fmt.Errorf("store: find_message_by_source: %w", err)
}
