// Package server — channel-agnostic inbound webhook pipeline.
//
// One pipeline serves every adapter: handshake → verify → normalize →
// idempotent persist → publish. Channel knowledge lives behind
// channels.InboundAdapter; this file must never mention a concrete channel
// (gateway-dispatch-lint enforces it).
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"github.com/google/uuid"
	"log/slog"
)

const maxWebhookBody = 1 << 20

// inboundPublisher is the publish seam (satisfied by *sdk.Client).
type inboundPublisher interface {
	PublishInbound(ctx context.Context, msg *miov1.Message) error
}

// webhookPipeline handles inbound webhooks for one channel adapter.
type webhookPipeline struct {
	channelType string
	inbound     channels.InboundAdapter
	store       channels.Store
	pub         inboundPublisher
	tenantID    string
	accountID   string
	metrics     *gatewayMetrics
	logger      *slog.Logger
}

func (p *webhookPipeline) finish(w http.ResponseWriter, start time.Time, outcome string, status int, errMsg string) {
	if status == http.StatusOK {
		writeOK(w)
	} else {
		writeErr(w, status, errMsg)
	}
	p.metrics.incInbound(p.channelType, "inbound", outcome)
	p.metrics.observeLatency(p.channelType, "inbound", outcome, time.Since(start).Seconds())
}

func (p *webhookPipeline) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	if p.inbound.HandleHandshake(w, r) {
		p.metrics.incInbound(p.channelType, "inbound", "handshake")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		p.logger.Error("webhook: read body", "channel", p.channelType, "err", err)
		p.finish(w, start, "parse_error", http.StatusBadRequest, "read error")
		return
	}

	if err := p.inbound.VerifySignature(r.Header, body); err != nil {
		p.logger.Warn("webhook: signature mismatch",
			"channel", p.channelType, "remote", r.RemoteAddr, "err", err)
		p.finish(w, start, "bad_signature", http.StatusUnauthorized, "invalid signature")
		return
	}

	msg, err := p.inbound.Normalize(body)
	if err != nil {
		if errors.Is(err, channels.ErrNormalizeSoft) {
			// 200 so the platform does not retry; metric captures the failure.
			p.logger.Warn("webhook: normalize", "channel", p.channelType, "err", err)
			p.finish(w, start, "normalize_error", http.StatusOK, "")
			return
		}
		p.logger.Error("webhook: parse", "channel", p.channelType, "err", err)
		p.finish(w, start, "parse_error", http.StatusBadRequest, "invalid json")
		return
	}

	ctx := r.Context()
	msg.TenantId = p.tenantID
	msg.AccountId = p.accountID

	conv, err := p.store.EnsureConversation(ctx,
		uuid.New(),
		p.tenantID, p.accountID, p.channelType,
		msg.GetConversationKind().String(),
		msg.GetConversationExternalId(),
		nil,
		stringPtrIfNotEmpty(msg.GetParentConversationId()),
		stringPtrIfNotEmpty(msg.GetAttributes()[channels.AttrConversationDisplayName]),
		nil,
	)
	if err != nil {
		p.logger.Error("webhook: ensure conversation", "channel", p.channelType, "err", err)
		p.finish(w, start, "db_error", http.StatusInternalServerError, "db error")
		return
	}
	msg.ConversationId = conv.ID.String()

	// Reply target resolution before the idempotent upsert: the adapter set
	// Relation.TargetExternalId; the durable ids come from the store.
	threadRootMessageID := ""
	if target := msg.GetRelation().GetTargetExternalId(); target != "" {
		parentMsg, found, err := p.store.FindMessageBySource(ctx, p.accountID, target)
		if err != nil {
			p.logger.Error("webhook: resolve replied message",
				"channel", p.channelType, "err", err,
				"source_message_id", msg.GetSourceMessageId(),
				"parent_external_id", target)
			p.finish(w, start, "db_error", http.StatusInternalServerError, "db error")
			return
		}
		if found {
			msg.Relation.TargetMessageId = parentMsg.ID.String()
			threadRootMessageID = parentMsg.ThreadRootMessageID.String()
			msg.ThreadRootMessageId = threadRootMessageID
		} else {
			p.logger.Warn("webhook: replied message parent not found",
				"channel", p.channelType,
				"source_message_id", msg.GetSourceMessageId(),
				"parent_external_id", target,
				"account_id", p.accountID)
		}
	}

	msgID := uuid.New()
	dbMsgID, fresh, err := p.store.EnsureUniqueMessage(ctx,
		msgID,
		p.tenantID, p.accountID,
		conv.ID.String(),
		stringPtrIfNotEmpty(threadRootMessageID),
		msg.GetSourceMessageId(),
		msg.GetSender().GetExternalId(),
		msg.GetText(),
		msg.GetAttributes(),
	)
	if err != nil {
		p.logger.Error("webhook: ensure unique message", "channel", p.channelType, "err", err)
		p.finish(w, start, "db_error", http.StatusInternalServerError, "db error")
		return
	}

	if !fresh {
		p.logger.Info("webhook: duplicate message suppressed",
			"channel", p.channelType,
			"source_message_id", msg.GetSourceMessageId(),
			"account_id", p.accountID)
		p.metrics.incDedup(p.channelType)
		p.finish(w, start, "dedup", http.StatusOK, "")
		return
	}

	msg.Id = dbMsgID.String()

	if err := p.pub.PublishInbound(ctx, msg); err != nil {
		p.logger.Error("webhook: publish inbound", "channel", p.channelType, "err", err,
			"msg_id", dbMsgID, "conv_id", conv.ID)
		p.finish(w, start, "publish_error", http.StatusInternalServerError, "publish error")
		return
	}

	p.finish(w, start, "success", http.StatusOK, "")
	p.logger.Info("webhook: message published",
		"channel", p.channelType,
		"msg_id", dbMsgID,
		"conv_id", conv.ID,
		"kind", msg.GetConversationKind().String(),
		"source_msg_id", msg.GetSourceMessageId(),
		"sender", msg.GetSender().GetExternalId(),
		"latency_ms", time.Since(start).Milliseconds(),
	)
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
