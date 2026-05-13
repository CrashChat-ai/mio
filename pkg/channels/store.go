package channels

import (
	"context"

	"github.com/google/uuid"
)

// Conversation is returned by Store.EnsureConversation. Carries only the
// resolved primary key — adapters don't need the full row to publish.
type Conversation struct {
	ID uuid.UUID
}

// Store captures the durable-state operations every inbound webhook adapter
// needs: idempotent conversation upsert and idempotent message upsert.
//
// The interface exists so adapter packages (channels/<name>/) stay
// independent of the gateway's concrete database layer. The gateway-side
// runtime wires a concrete implementation (services/gateway/store) when
// constructing HandlerDeps.
//
// Both methods are idempotent and safe to retry across redeliveries.
type Store interface {
	// EnsureConversation inserts a conversation idempotently keyed on
	// (account_id, external_id). On conflict, returns the existing row's id;
	// NEVER mutates kind or display_name (first-write-wins).
	EnsureConversation(
		ctx context.Context,
		id uuid.UUID,
		tenantID, accountID, channelType, kind, externalID string,
		parentConversationID *uuid.UUID,
		parentExternalID *string,
		displayName *string,
		attributes map[string]string,
	) (Conversation, error)

	// EnsureUniqueMessage inserts a message row idempotently keyed on
	// (account_id, source_message_id). Returns (id, fresh=true) on first
	// insert; (id, fresh=false) when the message already exists.
	EnsureUniqueMessage(
		ctx context.Context,
		id uuid.UUID,
		tenantID, accountID, conversationID string,
		threadRootMessageID *string,
		sourceMessageID, senderExternalID, text string,
		attributes map[string]string,
	) (msgID uuid.UUID, fresh bool, err error)
}
