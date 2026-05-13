package sdk

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"google.golang.org/protobuf/proto"
)

// PublishInbound validates and publishes a Message to the inbound stream.
//
// Idempotency key: Nats-Msg-Id = "inb:<account_id>:<source_message_id>"
// Namespace by account_id so two installs of the same channel cannot collide.
// Note: the 2-minute dedup window is a best-effort guard; defense-in-depth is
// provided by the Postgres (account_id, source_message_id) unique constraint.
//
// Publish pipeline:
//  1. Verify(msg) — schema version, required IDs, known channel_type (publish-only).
//  2. Build Nats-Msg-Id header for dedup.
//  3. Inject W3C traceparent (PRODUCER span).
//  4. Build subject via subjects.go (4-token inbound form).
//  5. Marshal proto, call js.PublishMsg (v2 API).
//  6. On ErrDuplicateID ack: record outcome=dedup, return nil (idempotent success).
func (c *Client) PublishInbound(ctx context.Context, msg *miov1.Message) error {
	// Step 1: schema + field verification (publish-side only).
	if err := Verify(msg); err != nil {
		c.metrics.incPublish(msg.GetChannelType(), "inbound", OutcomeInvalid)
		return fmt.Errorf("sdk: publish_inbound verify: %w", err)
	}

	// Step 2: idempotency key — namespace by account_id to isolate tenants.
	msgID := fmt.Sprintf("inb:%s:%s", msg.AccountId, msg.SourceMessageId)

	// Step 3: build subject.
	subject, err := Inbound(msg.ChannelType, msg.AccountId, msg.ConversationId)
	if err != nil {
		c.metrics.incPublish(msg.ChannelType, "inbound", OutcomeInvalid)
		return fmt.Errorf("sdk: publish_inbound subject: %w", err)
	}

	// Step 4: marshal proto payload.
	payload, err := proto.Marshal(msg)
	if err != nil {
		c.metrics.incPublish(msg.ChannelType, "inbound", OutcomeError)
		return fmt.Errorf("sdk: publish_inbound marshal: %w", err)
	}

	// Step 5: build NATS message with headers.
	natsmsg := &nats.Msg{
		Subject: subject,
		Header:  nats.Header{},
		Data:    payload,
	}
	natsmsg.Header.Set("Nats-Msg-Id", msgID)

	// Step 6: inject OTel traceparent (PRODUCER span).
	traceCtx, span := injectTrace(ctx, c.tp, subject, msgID, natsmsg.Header)
	_ = traceCtx

	// Step 7: publish and measure latency.
	start := time.Now()
	ack, err := c.js.PublishMsg(ctx, natsmsg)
	elapsed := time.Since(start).Seconds()

	if err != nil {
		span.RecordError(err)
		endSpanOnError(span, err)
		c.metrics.incPublish(msg.ChannelType, "inbound", OutcomeError)
		c.metrics.observePublish(msg.ChannelType, "inbound", elapsed)
		return fmt.Errorf("sdk: publish_inbound: %w", err)
	}

	// Step 8: dedup detection — JetStream signals duplicate via ack.Duplicate flag.
	if ack.Duplicate {
		endSpanOnError(span, nil)
		c.metrics.incPublish(msg.ChannelType, "inbound", OutcomeDedup)
		c.metrics.observePublish(msg.ChannelType, "inbound", elapsed)
		return nil // idempotent success
	}

	endSpanOnError(span, nil)
	c.metrics.incPublish(msg.ChannelType, "inbound", OutcomeSuccess)
	c.metrics.observePublish(msg.ChannelType, "inbound", elapsed)

	// Suppress unused variable warning for ack in non-dedup path.
	_ = ack
	return nil
}

// buildInboundMsgID builds the Nats-Msg-Id for inbound dedup.
// Exported for testing; callers should use PublishInbound, not this directly.
func buildInboundMsgID(accountID, sourceMessageID string) string {
	return fmt.Sprintf("inb:%s:%s", accountID, sourceMessageID)
}

// isDuplicateAck checks whether a JetStream publish ack signals deduplication.
// Centralised here so the logic is easy to find and test.
func isDuplicateAck(ack *jetstream.PubAck) bool {
	return ack != nil && ack.Duplicate
}
