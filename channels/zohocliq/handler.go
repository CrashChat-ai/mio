package zohocliq

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	sdk "github.com/crashchat-ai/mio/sdk-go"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const channelType = "zoho_cliq"

// HandlerDeps holds dependencies injected into the Cliq webhook handler.
type HandlerDeps struct {
	Store     channels.Store
	SDK       *sdk.Client
	TenantID  string
	AccountID string
	Secret    []byte // HMAC-SHA256 signing key; empty = dev mode (accepts all)

	// Metrics callbacks (injected by server to avoid circular deps).
	IncInbound     func(direction, outcome string)
	ObserveLatency func(direction, outcome string, secs float64)
	IncDedup       func()

	Logger *slog.Logger
}

// Handler returns an http.HandlerFunc for POST /webhooks/zoho-cliq.
func Handler(deps HandlerDeps) http.HandlerFunc {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Step 1: buffer body before any processing (required for HMAC verify).
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB cap
		if err != nil {
			deps.Logger.Error("cliq: read body", "err", err)
			writeErr(w, http.StatusBadRequest, "read error")
			deps.IncInbound("inbound", "parse_error")
			deps.ObserveLatency("inbound", "parse_error", time.Since(start).Seconds())
			return
		}

		// Step 2: verify signature.
		sigHeader := r.Header.Get("X-Webhook-Signature")
		if len(deps.Secret) > 0 && !VerifySignature(deps.Secret, body, sigHeader) {
			deps.Logger.Warn("cliq: signature mismatch",
				"remote", r.RemoteAddr,
				"header", sigHeader)
			writeErr(w, http.StatusUnauthorized, "invalid signature")
			deps.IncInbound("inbound", "bad_signature")
			deps.ObserveLatency("inbound", "bad_signature", time.Since(start).Seconds())
			return
		}
		if len(deps.Secret) == 0 {
			deps.Logger.Warn("cliq: WEBHOOK SECRET UNSET — accepting all requests (dev only)")
		}

		// Step 3: parse payload.
		payload, err := ParseWebhookPayload(body)
		if err != nil {
			deps.Logger.Error("cliq: parse payload", "err", err)
			writeErr(w, http.StatusBadRequest, "invalid json")
			deps.IncInbound("inbound", "parse_error")
			deps.ObserveLatency("inbound", "parse_error", time.Since(start).Seconds())
			return
		}

		// Step 4: normalize.
		nm, err := Normalize(payload)
		if err != nil {
			deps.Logger.Warn("cliq: normalize", "err", err, "operation", payload.Operation)
			// Return 200 to Cliq so it doesn't retry — we can't process this payload
			// but retrying won't help. Metric captures the failure.
			writeOK(w)
			deps.IncInbound("inbound", "normalize_error")
			deps.ObserveLatency("inbound", "normalize_error", time.Since(start).Seconds())
			return
		}

		ctx := r.Context()

		// Step 5: upsert conversation (FK dependency before message insert).
		convID := uuid.New()
		kindStr := nm.ConversationKind
		var parentExtID *string
		if nm.ParentExternalID != "" {
			parentExtID = &nm.ParentExternalID
		}
		displayName := nm.ConversationDisplayName
		var displayNamePtr *string
		if displayName != "" {
			displayNamePtr = &displayName
		}

		conv, err := deps.Store.EnsureConversation(ctx,
			convID,
			deps.TenantID, deps.AccountID, channelType, kindStr,
			nm.ConversationExternalID,
			nil, // parentConversationID UUID — resolved below for threads
			parentExtID,
			displayNamePtr,
			nil,
		)
		if err != nil {
			deps.Logger.Error("cliq: ensure conversation", "err", err)
			writeErr(w, http.StatusInternalServerError, "db error")
			deps.IncInbound("inbound", "db_error")
			deps.ObserveLatency("inbound", "db_error", time.Since(start).Seconds())
			return
		}

		// Step 6: resolve reply target before idempotent message upsert.
		threadRootMessageID := ""
		relationTargetMessageID := ""
		if nm.ParentExternalID != "" {
			parentMsg, found, err := deps.Store.FindMessageBySource(ctx, deps.AccountID, nm.ParentExternalID)
			if err != nil {
				deps.Logger.Error("cliq: resolve replied message", "err", err,
					"source_message_id", nm.SourceMessageID,
					"parent_external_id", nm.ParentExternalID)
				writeErr(w, http.StatusInternalServerError, "db error")
				deps.IncInbound("inbound", "db_error")
				deps.ObserveLatency("inbound", "db_error", time.Since(start).Seconds())
				return
			}
			if found {
				relationTargetMessageID = parentMsg.ID.String()
				threadRootMessageID = parentMsg.ThreadRootMessageID.String()
			} else {
				deps.Logger.Warn("cliq: replied message parent not found",
					"source_message_id", nm.SourceMessageID,
					"parent_external_id", nm.ParentExternalID,
					"account_id", deps.AccountID)
			}
		}

		// Step 7: idempotent message upsert.
		msgID := uuid.New()
		dbMsgID, fresh, err := deps.Store.EnsureUniqueMessage(ctx,
			msgID,
			deps.TenantID, deps.AccountID,
			conv.ID.String(),
			stringPtrIfNotEmpty(threadRootMessageID),
			nm.SourceMessageID,
			nm.SenderExternalID,
			nm.Text,
			nm.Attributes,
		)
		if err != nil {
			deps.Logger.Error("cliq: ensure unique message", "err", err)
			writeErr(w, http.StatusInternalServerError, "db error")
			deps.IncInbound("inbound", "db_error")
			deps.ObserveLatency("inbound", "db_error", time.Since(start).Seconds())
			return
		}

		if !fresh {
			// Duplicate — idempotency dedup fires.
			deps.Logger.Info("cliq: duplicate message suppressed",
				"source_message_id", nm.SourceMessageID,
				"account_id", deps.AccountID)
			deps.IncDedup()
			writeOK(w)
			deps.IncInbound("inbound", "dedup")
			deps.ObserveLatency("inbound", "dedup", time.Since(start).Seconds())
			return
		}

		// Step 8: publish to MESSAGES_INBOUND (before 200 response, inside deadline).
		protoMsg := buildInboundProtoMessage(
			nm,
			dbMsgID,
			deps.TenantID,
			deps.AccountID,
			conv.ID,
			threadRootMessageID,
			relationTargetMessageID,
		)

		if err := deps.SDK.PublishInbound(ctx, protoMsg); err != nil {
			deps.Logger.Error("cliq: publish inbound", "err", err,
				"msg_id", dbMsgID, "conv_id", conv.ID)
			writeErr(w, http.StatusInternalServerError, "publish error")
			deps.IncInbound("inbound", "publish_error")
			deps.ObserveLatency("inbound", "publish_error", time.Since(start).Seconds())
			return
		}

		// Step 9: respond 200 to Cliq (inside deadline).
		writeOK(w)
		deps.IncInbound("inbound", "success")
		deps.ObserveLatency("inbound", "success", time.Since(start).Seconds())

		deps.Logger.Info("cliq: message published",
			"msg_id", dbMsgID,
			"conv_id", conv.ID,
			"kind", nm.ConversationKind,
			"source_msg_id", nm.SourceMessageID,
			"sender", nm.SenderExternalID,
			"latency_ms", fmt.Sprintf("%.1f", time.Since(start).Seconds()*1000),
		)
	}
}

func buildInboundProtoMessage(
	nm *NormalizedMessage,
	dbMsgID uuid.UUID,
	tenantID, accountID string,
	convID uuid.UUID,
	threadRootMessageID, relationTargetMessageID string,
) *miov1.Message {
	protoMsg := &miov1.Message{
		Id:                     dbMsgID.String(),
		SchemaVersion:          1,
		TenantId:               tenantID,
		AccountId:              accountID,
		ChannelType:            channelType,
		ConversationId:         convID.String(),
		ConversationExternalId: nm.ConversationExternalID,
		ConversationKind:       kindStringToEnum(nm.ConversationKind),
		SourceMessageId:        nm.SourceMessageID,
		Sender: &miov1.Sender{
			ExternalId:  nm.SenderExternalID,
			DisplayName: nm.SenderDisplayName,
			IsBot:       nm.SenderIsBot,
		},
		Text:       nm.Text,
		ReceivedAt: timestamppb.Now(),
		Attributes: nm.Attributes,
	}

	// Convert normalized attachments to proto Attachments so the P9
	// media-vault sidecar can persist bytes and rewrite urls.
	for _, a := range nm.Attachments {
		protoMsg.Attachments = append(protoMsg.Attachments, &miov1.Attachment{
			Kind:     attachmentKindFromMime(a.MIME),
			Url:      a.URL,
			Mime:     a.MIME,
			Filename: a.Filename,
		})
	}

	applyReplyFields(protoMsg, nm.ParentExternalID, threadRootMessageID, relationTargetMessageID)

	return protoMsg
}

func applyReplyFields(
	msg *miov1.Message,
	parentExternalID, threadRootMessageID, relationTargetMessageID string,
) {
	if parentExternalID == "" {
		return
	}

	msg.ParentConversationId = parentExternalID // external id as proxy until UUID resolved
	msg.ThreadRootMessageId = threadRootMessageID
	msg.Relation = &miov1.MessageRelation{
		Kind:             miov1.MessageRelation_KIND_REPLY,
		TargetMessageId:  relationTargetMessageID,
		TargetExternalId: parentExternalID,
	}
}

func stringPtrIfNotEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func writeOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// kindStringToEnum converts a kind string to the proto enum value.
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
	default:
		return miov1.ConversationKind_CONVERSATION_KIND_UNSPECIFIED
	}
}
