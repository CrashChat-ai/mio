package reconciler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// Publisher writes fresh reconciled messages to the inbound bus.
type Publisher interface {
	PublishInbound(ctx context.Context, msg *miov1.Message) error
}

// Request describes one bounded reconciliation pull for an account and
// conversation.
type Request struct {
	TenantID    string
	AccountID   string
	ChannelType string
	Credential  channels.Credential

	Conversation channels.HistoryConversation
	Cursor       string
	Since        time.Time
	Until        time.Time
	Limit        int
}

// Result summarizes what happened during one reconciliation pull.
type Result struct {
	Seen       int
	Inserted   int
	Duplicates int
	Published  int
	NextCursor string
}

// Runner coordinates provider history reads with MIO's durable inbound store
// and publisher. It is channel-agnostic; provider behavior lives behind
// channels.HistoryAdapter.
type Runner struct {
	Store     channels.Store
	Publisher Publisher
	Adapters  map[string]channels.HistoryAdapter
	Logger    *slog.Logger
}

// Reconcile fetches one provider history page, idempotently stores any new
// messages, and publishes fresh messages to MESSAGES_INBOUND.
func (r *Runner) Reconcile(ctx context.Context, req Request) (Result, error) {
	if r.Logger == nil {
		r.Logger = slog.Default()
	}
	if r.Store == nil {
		return Result{}, fmt.Errorf("reconciler: Store is required")
	}
	if r.Publisher == nil {
		return Result{}, fmt.Errorf("reconciler: Publisher is required")
	}
	if req.TenantID == "" || req.AccountID == "" || req.ChannelType == "" {
		return Result{}, fmt.Errorf("reconciler: tenant_id, account_id, and channel_type are required")
	}
	if req.Conversation.ExternalID == "" {
		return Result{}, fmt.Errorf("reconciler: conversation external_id is required")
	}

	adapter := r.Adapters[req.ChannelType]
	if adapter == nil {
		return Result{}, fmt.Errorf("reconciler: no history adapter for channel_type %q", req.ChannelType)
	}

	page, err := adapter.FetchHistory(ctx, channels.HistoryRequest{
		Credential:   req.Credential,
		Conversation: req.Conversation,
		Cursor:       req.Cursor,
		Since:        req.Since,
		Until:        req.Until,
		Limit:        req.Limit,
	})
	if err != nil {
		return Result{}, err
	}

	kind := req.Conversation.Kind
	if kind == "" {
		kind = "CONVERSATION_KIND_CHANNEL_PUBLIC"
	}
	displayName := req.Conversation.DisplayName
	var displayNamePtr *string
	if displayName != "" {
		displayNamePtr = &displayName
	}

	conv, err := r.Store.EnsureConversation(ctx,
		uuid.New(),
		req.TenantID,
		req.AccountID,
		req.ChannelType,
		kind,
		req.Conversation.ExternalID,
		nil,
		nil,
		displayNamePtr,
		req.Conversation.Attributes,
	)
	if err != nil {
		return Result{}, fmt.Errorf("reconciler: ensure conversation: %w", err)
	}

	res := Result{
		Seen:       len(page.Messages),
		NextCursor: page.NextCursor,
	}
	for _, hm := range page.Messages {
		if hm.SourceMessageID == "" {
			r.Logger.Warn("reconciler: skipping history message with empty source id",
				"account_id", req.AccountID,
				"channel_type", req.ChannelType,
				"conversation_external_id", req.Conversation.ExternalID)
			continue
		}

		threadRootMessageID := ""
		relationTargetMessageID := ""
		if hm.ParentExternalID != "" {
			parent, found, err := r.Store.FindMessageBySource(ctx, req.AccountID, hm.ParentExternalID)
			if err != nil {
				return res, fmt.Errorf("reconciler: resolve parent %q: %w", hm.ParentExternalID, err)
			}
			if found {
				threadRootMessageID = parent.ThreadRootMessageID.String()
				relationTargetMessageID = parent.ID.String()
			}
		}

		msgID := uuid.New()
		dbMsgID, fresh, err := r.Store.EnsureUniqueMessage(ctx,
			msgID,
			req.TenantID,
			req.AccountID,
			conv.ID.String(),
			stringPtrIfNotEmpty(threadRootMessageID),
			hm.SourceMessageID,
			hm.SenderExternalID,
			hm.Text,
			hm.Attributes,
		)
		if err != nil {
			return res, fmt.Errorf("reconciler: ensure message %q: %w", hm.SourceMessageID, err)
		}
		if !fresh {
			res.Duplicates++
			continue
		}
		res.Inserted++

		protoMsg := buildInboundMessage(req, hm, dbMsgID, conv.ID, kind, threadRootMessageID, relationTargetMessageID)
		if err := r.Publisher.PublishInbound(ctx, protoMsg); err != nil {
			return res, fmt.Errorf("reconciler: publish %q: %w", hm.SourceMessageID, err)
		}
		res.Published++
	}

	return res, nil
}

func buildInboundMessage(
	req Request,
	hm channels.HistoryMessage,
	dbMsgID uuid.UUID,
	convID uuid.UUID,
	kind string,
	threadRootMessageID string,
	relationTargetMessageID string,
) *miov1.Message {
	receivedAt := timestamppb.Now()
	if !hm.SentAt.IsZero() {
		receivedAt = timestamppb.New(hm.SentAt)
	}
	msg := &miov1.Message{
		Id:                     dbMsgID.String(),
		SchemaVersion:          1,
		TenantId:               req.TenantID,
		AccountId:              req.AccountID,
		ChannelType:            req.ChannelType,
		ConversationId:         convID.String(),
		ConversationExternalId: req.Conversation.ExternalID,
		ConversationKind:       kindStringToEnum(kind),
		SourceMessageId:        hm.SourceMessageID,
		Sender: &miov1.Sender{
			ExternalId:  hm.SenderExternalID,
			DisplayName: hm.SenderDisplayName,
			IsBot:       hm.SenderIsBot,
		},
		Text:        hm.Text,
		ReceivedAt:  receivedAt,
		Attributes:  cloneMap(hm.Attributes),
		Attachments: hm.Attachments,
	}
	if msg.Attributes == nil {
		msg.Attributes = map[string]string{}
	}
	msg.Attributes["mio_reconciled"] = "true"

	if hm.ParentExternalID != "" {
		msg.ParentConversationId = hm.ParentExternalID
		msg.ThreadRootMessageId = threadRootMessageID
		msg.Relation = &miov1.MessageRelation{
			Kind:             miov1.MessageRelation_KIND_REPLY,
			TargetMessageId:  relationTargetMessageID,
			TargetExternalId: hm.ParentExternalID,
		}
	}
	return msg
}

func kindStringToEnum(kind string) miov1.ConversationKind {
	switch kind {
	case "CONVERSATION_KIND_DM":
		return miov1.ConversationKind_CONVERSATION_KIND_DM
	case "CONVERSATION_KIND_GROUP_DM":
		return miov1.ConversationKind_CONVERSATION_KIND_GROUP_DM
	case "CONVERSATION_KIND_CHANNEL_PUBLIC":
		return miov1.ConversationKind_CONVERSATION_KIND_CHANNEL_PUBLIC
	case "CONVERSATION_KIND_CHANNEL_PRIVATE":
		return miov1.ConversationKind_CONVERSATION_KIND_CHANNEL_PRIVATE
	case "CONVERSATION_KIND_THREAD":
		return miov1.ConversationKind_CONVERSATION_KIND_THREAD
	case "CONVERSATION_KIND_FORUM_POST":
		return miov1.ConversationKind_CONVERSATION_KIND_FORUM_POST
	case "CONVERSATION_KIND_BROADCAST":
		return miov1.ConversationKind_CONVERSATION_KIND_BROADCAST
	default:
		return miov1.ConversationKind_CONVERSATION_KIND_UNSPECIFIED
	}
}

func stringPtrIfNotEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
