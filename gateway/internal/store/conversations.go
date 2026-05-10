package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Conversation is returned by EnsureConversation.
type Conversation struct {
	ID uuid.UUID
}

// EnsureConversation inserts a conversation idempotently.
// On conflict (account_id, external_id), returns the existing row's id.
// NEVER updates kind or display_name on conflict — first-write-wins
// (immutability rule). Kind miscoding is logged CRITICAL by the caller.
func EnsureConversation(
	ctx context.Context,
	pool *pgxpool.Pool,
	id uuid.UUID,
	tenantID, accountID, channelType, kind, externalID string,
	parentConversationID *uuid.UUID,
	parentExternalID *string,
	displayName *string,
	attributes map[string]string,
) (Conversation, error) {
	attrsJSON, err := marshalAttrs(attributes)
	if err != nil {
		return Conversation{}, fmt.Errorf("store: ensure_conversation attrs: %w", err)
	}

	const q = `
INSERT INTO conversations (
  id, tenant_id, account_id, channel_type, kind,
  external_id, parent_conversation_id, parent_external_id,
  display_name, attributes
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (account_id, external_id) DO NOTHING
RETURNING id`

	row := pool.QueryRow(ctx, q,
		id, tenantID, accountID, channelType, kind,
		externalID, parentConversationID, parentExternalID,
		displayName, attrsJSON,
	)
	var returnedID uuid.UUID
	err = row.Scan(&returnedID)
	if err == nil {
		return Conversation{ID: returnedID}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, fmt.Errorf("store: ensure_conversation insert: %w", err)
	}

	// Conflict: fetch existing id.
	const sel = `SELECT id FROM conversations WHERE account_id = $1 AND external_id = $2`
	if err2 := pool.QueryRow(ctx, sel, accountID, externalID).Scan(&returnedID); err2 != nil {
		return Conversation{}, fmt.Errorf("store: ensure_conversation select after conflict: %w", err2)
	}
	return Conversation{ID: returnedID}, nil
}

// LookupConversationByExternalID resolves a conversation UUID from its
// (account_id, external_id) pair. Used to resolve thread parent refs.
func LookupConversationByExternalID(
	ctx context.Context,
	pool *pgxpool.Pool,
	accountID, externalID string,
) (uuid.UUID, error) {
	var id uuid.UUID
	err := pool.QueryRow(ctx,
		`SELECT id FROM conversations WHERE account_id = $1 AND external_id = $2`,
		accountID, externalID,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil // not found; caller decides
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("store: lookup_conversation: %w", err)
	}
	return id, nil
}
