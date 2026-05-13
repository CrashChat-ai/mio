package sdk

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"google.golang.org/protobuf/proto"
)

// PublishOutbound validates and publishes a SendCommand to the outbound stream.
//
// Idempotency key: Nats-Msg-Id = "out:<cmd.id>"
// The SendCommand.Id is a ULID (globally unique); no extra namespacing needed.
//
// Subject form:
//   - No edit: mio.outbound.<ct>.<acct>.<conv>
//   - Edit/delete: mio.outbound.<ct>.<acct>.<conv>.<edit_of_message_id>
//
// Publish pipeline mirrors PublishInbound; see inline comments.
func (c *Client) PublishOutbound(ctx context.Context, cmd *miov1.SendCommand) error {
	// Step 1: schema + field verification (publish-side only).
	if err := VerifyCommand(cmd); err != nil {
		c.metrics.incPublish(cmd.GetChannelType(), "outbound", OutcomeInvalid)
		return fmt.Errorf("sdk: publish_outbound verify: %w", err)
	}

	// Step 2: idempotency key — ULID is globally unique; no namespacing needed.
	msgID := buildOutboundMsgID(cmd.Id)

	// Step 3: build subject — include message_id segment only for edit/delete.
	var subject string
	var err error
	if cmd.EditOfMessageId != "" {
		subject, err = Outbound(cmd.ChannelType, cmd.AccountId, cmd.ConversationId, cmd.EditOfMessageId)
	} else {
		subject, err = Outbound(cmd.ChannelType, cmd.AccountId, cmd.ConversationId)
	}
	if err != nil {
		c.metrics.incPublish(cmd.ChannelType, "outbound", OutcomeInvalid)
		return fmt.Errorf("sdk: publish_outbound subject: %w", err)
	}

	// Step 4: marshal proto payload.
	payload, err := proto.Marshal(cmd)
	if err != nil {
		c.metrics.incPublish(cmd.ChannelType, "outbound", OutcomeError)
		return fmt.Errorf("sdk: publish_outbound marshal: %w", err)
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
		endSpanOnError(span, err)
		c.metrics.incPublish(cmd.ChannelType, "outbound", OutcomeError)
		c.metrics.observePublish(cmd.ChannelType, "outbound", elapsed)
		return fmt.Errorf("sdk: publish_outbound: %w", err)
	}

	// Step 8: dedup detection.
	if ack.Duplicate {
		endSpanOnError(span, nil)
		c.metrics.incPublish(cmd.ChannelType, "outbound", OutcomeDedup)
		c.metrics.observePublish(cmd.ChannelType, "outbound", elapsed)
		return nil
	}

	endSpanOnError(span, nil)
	c.metrics.incPublish(cmd.ChannelType, "outbound", OutcomeSuccess)
	c.metrics.observePublish(cmd.ChannelType, "outbound", elapsed)
	_ = ack
	return nil
}

// buildOutboundMsgID builds the Nats-Msg-Id for outbound dedup.
func buildOutboundMsgID(cmdID string) string {
	return fmt.Sprintf("out:%s", cmdID)
}
