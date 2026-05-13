// Package worker drives the consume → process → ack loop against the
// MESSAGES_INBOUND JetStream consumer.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"

	"github.com/crashchat-ai/mio/services/media-vault/internal/metrics"
)

// Processor is the per-message processing strategy. Phase-03 ships a no-op
// "ack everything" implementation; phase-04 swaps it for the real fetch +
// store + republish flow.
type Processor interface {
	// Process handles a single inbound message. Return nil to ack, a sentinel
	// from this package (ErrTerm) to terminate, or any other error to nak.
	Process(ctx context.Context, msg *miov1.Message) error
}

// ErrTerm signals that a message is undecodable / unprocessable and should
// be Term'd rather than Naked (which would cause infinite redelivery).
var ErrTerm = errors.New("worker: terminate message")

// Config is the runtime knobs used by Run.
type Config struct {
	Stream        string // e.g. MESSAGES_INBOUND
	Durable       string // e.g. media-vault
	FilterSubject string // e.g. mio.inbound.>
	AckWait       time.Duration
	MaxAckPending int
	MaxDeliver    int

	Logger *slog.Logger
}

// Worker connects to NATS+JetStream and runs the message loop.
type Worker struct {
	nc        *nats.Conn
	js        jetstream.JetStream
	cfg       Config
	processor Processor
	log       *slog.Logger
}

// New constructs a Worker. Caller owns the *nats.Conn lifecycle.
func New(nc *nats.Conn, js jetstream.JetStream, cfg Config, p Processor) *Worker {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.AckWait == 0 {
		cfg.AckWait = 90 * time.Second
	}
	if cfg.MaxAckPending == 0 {
		cfg.MaxAckPending = 8
	}
	if cfg.MaxDeliver == 0 {
		cfg.MaxDeliver = 5
	}
	if cfg.FilterSubject == "" {
		cfg.FilterSubject = "mio.inbound.>"
	}
	return &Worker{nc: nc, js: js, cfg: cfg, processor: p, log: cfg.Logger}
}

// Run attaches the durable consumer and processes messages until ctx is done.
func (w *Worker) Run(ctx context.Context) error {
	stream, err := w.js.Stream(ctx, w.cfg.Stream)
	if err != nil {
		return fmt.Errorf("worker: lookup stream %q: %w", w.cfg.Stream, err)
	}

	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       w.cfg.Durable,
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       w.cfg.AckWait,
		MaxAckPending: w.cfg.MaxAckPending,
		MaxDeliver:    w.cfg.MaxDeliver,
		ReplayPolicy:  jetstream.ReplayInstantPolicy,
		FilterSubject: w.cfg.FilterSubject,
	})
	if err != nil {
		return fmt.Errorf("worker: ensure consumer %q: %w", w.cfg.Durable, err)
	}

	msgCh := make(chan jetstream.Msg, w.cfg.MaxAckPending)
	cc, err := cons.Consume(func(m jetstream.Msg) {
		select {
		case msgCh <- m:
		case <-ctx.Done():
			_ = m.Nak()
		}
	})
	if err != nil {
		return fmt.Errorf("worker: start consume: %w", err)
	}
	defer cc.Stop()

	w.log.Info("worker: consumer attached",
		"stream", w.cfg.Stream, "durable", w.cfg.Durable, "filter", w.cfg.FilterSubject)

	for {
		select {
		case <-ctx.Done():
			w.log.Info("worker: context done, draining inflight")
			return nil
		case raw, ok := <-msgCh:
			if !ok {
				return nil
			}
			w.handle(ctx, raw)
		}
	}
}

func (w *Worker) handle(ctx context.Context, raw jetstream.Msg) {
	metrics.Inflight.Inc()
	defer metrics.Inflight.Dec()

	var msg miov1.Message
	if err := proto.Unmarshal(raw.Data(), &msg); err != nil {
		w.log.Error("worker: unmarshal proto, terming", "err", err, "subject", raw.Subject())
		_ = raw.Term()
		return
	}

	// Every inbound message must be republished to MESSAGES_INBOUND_ENRICHED
	// (downstream AI consumers read only from there). The processor handles
	// per-attachment work (no-op when none) and publishes exactly once.
	// Per-attachment fast paths (already-enriched, no fetcher) live inside
	// processAttachment so a single message with mixed attachments still
	// flows correctly.
	if err := w.processor.Process(ctx, &msg); err != nil {
		if errors.Is(err, ErrTerm) {
			w.log.Error("worker: terming message", "err", err, "subject", raw.Subject())
			_ = raw.Term()
			return
		}
		w.log.Warn("worker: process failed, naking", "err", err, "subject", raw.Subject())
		_ = raw.NakWithDelay(jitterBackoff())
		return
	}
	_ = raw.Ack()
}

// jitterBackoff returns a small random-ish backoff for Nak. JetStream's own
// backoff would be configurable too, but a small immediate redelivery hint is
// enough for POC.
func jitterBackoff() time.Duration { return 5 * time.Second }

// NoopProcessor is the phase-03 stub: ack-pass everything without modification.
// Phase-04 replaces this with the real Cliq-fetch + storage-write + publish flow.
type NoopProcessor struct{}

func (NoopProcessor) Process(_ context.Context, _ *miov1.Message) error { return nil }
