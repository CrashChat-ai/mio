package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crashchat-ai/mio/pkg/channels"
)

// InboundStore adapts the free EnsureConversation / EnsureUniqueMessage
// functions onto channels.Store so adapter packages can depend on the
// interface in pkg/channels rather than the concrete pgxpool-bound store
// API.
type InboundStore struct {
	pool *pgxpool.Pool
}

// NewInboundStore wires a pgxpool.Pool behind the channels.Store interface.
func NewInboundStore(pool *pgxpool.Pool) *InboundStore {
	return &InboundStore{pool: pool}
}

// EnsureConversation delegates to the package-level EnsureConversation
// function and re-packs its Conversation into channels.Conversation.
func (s *InboundStore) EnsureConversation(
	ctx context.Context,
	id uuid.UUID,
	tenantID, accountID, channelType, kind, externalID string,
	parentConversationID *uuid.UUID,
	parentExternalID *string,
	displayName *string,
	attributes map[string]string,
) (channels.Conversation, error) {
	conv, err := EnsureConversation(ctx, s.pool,
		id, tenantID, accountID, channelType, kind, externalID,
		parentConversationID, parentExternalID, displayName, attributes,
	)
	if err != nil {
		return channels.Conversation{}, err
	}
	return channels.Conversation{ID: conv.ID}, nil
}

// EnsureUniqueMessage delegates to the package-level EnsureUniqueMessage.
func (s *InboundStore) EnsureUniqueMessage(
	ctx context.Context,
	id uuid.UUID,
	tenantID, accountID, conversationID string,
	threadRootMessageID *string,
	sourceMessageID, senderExternalID, text string,
	attributes map[string]string,
) (uuid.UUID, bool, error) {
	return EnsureUniqueMessage(ctx, s.pool,
		id, tenantID, accountID, conversationID,
		threadRootMessageID, sourceMessageID, senderExternalID, text, attributes,
	)
}
