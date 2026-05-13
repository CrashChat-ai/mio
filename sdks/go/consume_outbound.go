package sdk

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"
)

// CommandDelivery wraps a raw JetStream message with typed SendCommand accessors.
// Callers must call Ack(), Nak(), or Term() exactly once per delivery.
//
// Schema verification is intentionally SKIPPED on the consume path (same asymmetry
// as inbound — see version.go). The gateway sender pool is the consumer here.
type CommandDelivery struct {
	cmd  *miov1.SendCommand
	raw  jetstream.Msg
	span trace.Span
}

// Cmd returns the decoded proto SendCommand. Never nil for a valid CommandDelivery.
func (d *CommandDelivery) Cmd() *miov1.SendCommand { return d.cmd }

// Ack acknowledges successful processing.
func (d *CommandDelivery) Ack() error {
	d.span.End()
	return d.raw.Ack()
}

// Nak negatively acknowledges with an optional redelivery delay.
func (d *CommandDelivery) Nak(delay time.Duration) error {
	d.span.End()
	return d.raw.NakWithDelay(delay)
}

// Term permanently terminates this message (no redelivery).
func (d *CommandDelivery) Term() error {
	d.span.End()
	return d.raw.Term()
}

// ConsumeOutbound attaches to a durable pull consumer on the outbound stream
// and returns a channel of CommandDelivery.
//
// Caller must supply a non-empty durable name — the SDK never auto-generates one.
// The consumer must already exist (gateway startup / P3 is the authoritative provisioner).
//
// Schema verification is SKIPPED on consume (publish-only asymmetry per version.go).
// OTel context is extracted from each message before yielding.
//
// Lifecycle: cancel ctx to stop consumption; the returned channel is closed on stop.
func (c *Client) ConsumeOutbound(ctx context.Context, stream, durable string) (<-chan CommandDelivery, error) {
	if durable == "" {
		return nil, fmt.Errorf("sdk: consume_outbound: durable name must not be empty; caller must supply an explicit durable")
	}

	cons, err := c.js.Consumer(ctx, stream, durable)
	if err != nil {
		return nil, fmt.Errorf("sdk: consume_outbound: lookup consumer %q on stream %q: %w", durable, stream, err)
	}

	ch := make(chan CommandDelivery, c.maxAckPending)

	cc, err := cons.Consume(func(raw jetstream.Msg) {
		start := time.Now()

		// Decode proto — skip VerifyCommand (consume-side asymmetry).
		var cmd miov1.SendCommand
		if err := proto.Unmarshal(raw.Data(), &cmd); err != nil {
			_ = raw.Term()
			c.metrics.incConsume(unknownChannelType(raw), "outbound", OutcomeError)
			return
		}

		// Extract OTel context (CONSUMER span).
		_, span := extractTrace(ctx, c.tp, raw.Subject(), raw.Headers())

		elapsed := time.Since(start).Seconds()
		c.metrics.incConsume(cmd.ChannelType, "outbound", OutcomeSuccess)
		c.metrics.observeConsume(cmd.ChannelType, "outbound", elapsed)

		select {
		case ch <- CommandDelivery{cmd: &cmd, raw: raw, span: span}:
		case <-ctx.Done():
			span.End()
			_ = raw.Nak()
		}
	})
	if err != nil {
		close(ch)
		return nil, fmt.Errorf("sdk: consume_outbound: start consume: %w", err)
	}

	go func() {
		<-ctx.Done()
		cc.Stop()
		close(ch)
	}()

	return ch, nil
}
