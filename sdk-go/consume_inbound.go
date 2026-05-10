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

// Delivery wraps a raw JetStream message with typed accessors.
// Callers must call Ack(), Nak(), or Term() exactly once per delivery.
//
// Schema verification is intentionally SKIPPED on the consume path.
// Consumers must tolerate forward-compatible additions (schema_version=2, etc).
// This asymmetry is documented in version.go and the P2 plan.
type Delivery struct {
	msg  *miov1.Message
	raw  jetstream.Msg
	span trace.Span
}

// Msg returns the decoded proto Message. Never nil for a valid Delivery.
func (d *Delivery) Msg() *miov1.Message { return d.msg }

// Ack acknowledges successful processing.
func (d *Delivery) Ack() error {
	d.span.End()
	return d.raw.Ack()
}

// Nak negatively acknowledges with an optional redelivery delay.
func (d *Delivery) Nak(delay time.Duration) error {
	d.span.End()
	return d.raw.NakWithDelay(delay)
}

// Term permanently terminates this message (no redelivery).
func (d *Delivery) Term() error {
	d.span.End()
	return d.raw.Term()
}

// ConsumeInbound attaches to a durable pull consumer and returns a channel of Delivery.
//
// Caller must supply a non-empty durable name — the SDK never auto-generates one.
// The consumer must already exist (gateway startup / P3 is the authoritative provisioner).
//
// Schema verification is SKIPPED on consume (publish-only asymmetry per version.go).
// OTel context is extracted from each message before yielding.
//
// Lifecycle: cancel ctx to stop consumption; the returned channel is closed on stop.
// Channel buffer size equals c.maxAckPending (default 1 = ordering guarantee).
func (c *Client) ConsumeInbound(ctx context.Context, stream, durable string) (<-chan Delivery, error) {
	if durable == "" {
		return nil, fmt.Errorf("sdk: consume_inbound: durable name must not be empty; caller must supply an explicit durable")
	}

	cons, err := c.js.Consumer(ctx, stream, durable)
	if err != nil {
		return nil, fmt.Errorf("sdk: consume_inbound: lookup consumer %q on stream %q: %w", durable, stream, err)
	}

	ch := make(chan Delivery, c.maxAckPending)

	cc, err := cons.Consume(func(raw jetstream.Msg) {
		start := time.Now()

		// Decode proto — skip Verify (consume-side asymmetry).
		var msg miov1.Message
		if err := proto.Unmarshal(raw.Data(), &msg); err != nil {
			// Cannot decode: term the message so it doesn't block the consumer.
			_ = raw.Term()
			c.metrics.incConsume(unknownChannelType(raw), "inbound", OutcomeError)
			return
		}

		// Extract OTel context (CONSUMER span).
		_, span := extractTrace(ctx, c.tp, raw.Subject(), raw.Headers())

		elapsed := time.Since(start).Seconds()
		c.metrics.incConsume(msg.ChannelType, "inbound", OutcomeSuccess)
		c.metrics.observeConsume(msg.ChannelType, "inbound", elapsed)

		select {
		case ch <- Delivery{msg: &msg, raw: raw, span: span}:
		case <-ctx.Done():
			span.End()
			_ = raw.Nak()
		}
	})
	if err != nil {
		close(ch)
		return nil, fmt.Errorf("sdk: consume_inbound: start consume: %w", err)
	}

	// Stop the consumer when ctx is cancelled; close the channel.
	go func() {
		<-ctx.Done()
		cc.Stop()
		close(ch)
	}()

	return ch, nil
}

// unknownChannelType extracts channel_type from the NATS subject for metrics
// when proto decode fails (subject token 3 = channel_type).
func unknownChannelType(raw jetstream.Msg) string {
	// subject = mio.<dir>.<channel_type>.<acct>.<conv>[.<msg>]
	subj := raw.Subject()
	var ct string
	i, count := 0, 0
	for ; i < len(subj); i++ {
		if subj[i] == '.' {
			count++
			if count == 2 {
				start := i + 1
				end := start
				for end < len(subj) && subj[end] != '.' {
					end++
				}
				ct = subj[start:end]
				break
			}
		}
	}
	if ct == "" {
		return "unknown"
	}
	return ct
}
