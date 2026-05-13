// Package archiver implements the GCS archival consume-encode-write loop.
//
// Design contracts (P6 plan):
//  1. One buffered writer per (partitionPath, fileName) key.
//  2. seqStart tracked on first record; seqEnd updated on every append.
//  3. Filename is finalised at flush time: <consumer-id>-<seqStart>-<seqEnd>.ndjson
//  4. Ack is deferred until the final object exists (after Flush returns nil).
//  5. Flush triggers: size ≥ 16 MB  OR  writer age ≥ 1 min  OR  SIGTERM.
//  6. SIGTERM drains all open writers before exit.
//
// Metrics emitted:
//   - mio_sink_gcs_bytes_written_total{channel_type}
//   - mio_sink_gcs_inflight_files        (gauge)
//   - mio_sink_gcs_flush_total{outcome}  (counter)
package archiver

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"github.com/crashchat-ai/mio/services/sink-gcs/internal/encode"
	"github.com/crashchat-ai/mio/services/sink-gcs/internal/filename"
	"github.com/crashchat-ai/mio/services/sink-gcs/internal/partition"
	"github.com/crashchat-ai/mio/services/sink-gcs/internal/writer"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/protobuf/proto"
)

// Config holds all tunables for the Archiver.
type Config struct {
	NatsURL     string
	Stream      string
	Subject     string // JetStream consumer filter subject (default: "mio.inbound.>")
	Durable     string
	WriterCfg   *writer.Config
	FlushSize   int           // bytes; flush when buffer reaches this size
	FlushAge    time.Duration // flush when writer has been open this long
	MaxInflight int           // JetStream MaxAckPending
	Logger      *slog.Logger
}

// bufEntry tracks one open (partition, file) writer.
type bufEntry struct {
	w        writer.Writer
	seqStart uint64
	seqEnd   uint64
	openedAt time.Time
	// pendingMsgs holds the raw JetStream messages waiting for ack.
	pendingMsgs []jetstream.Msg
}

// Archiver connects to NATS, pulls from the durable consumer, and writes
// encoded messages to GCS/MinIO.
type Archiver struct {
	cfg Config
	log *slog.Logger
	nc  *nats.Conn
	js  jetstream.JetStream

	mu      sync.Mutex
	buffers map[string]*bufEntry // key = partitionPath

	// metrics
	bytesTotal    *prometheus.CounterVec
	inflightGauge prometheus.Gauge
	flushTotal    *prometheus.CounterVec
}

// New creates an Archiver, connecting to NATS.
func New(cfg Config) (*Archiver, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.FlushSize == 0 {
		cfg.FlushSize = 16 * 1024 * 1024
	}
	if cfg.FlushAge == 0 {
		cfg.FlushAge = time.Minute
	}
	if cfg.MaxInflight == 0 {
		cfg.MaxInflight = 64
	}
	if cfg.Subject == "" {
		cfg.Subject = "mio.inbound.>"
	}

	nc, err := nats.Connect(cfg.NatsURL, nats.Name("mio-sink-gcs"))
	if err != nil {
		return nil, fmt.Errorf("archiver: nats connect: %w", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("archiver: jetstream.New: %w", err)
	}

	a := &Archiver{
		cfg:     cfg,
		log:     cfg.Logger,
		nc:      nc,
		js:      js,
		buffers: make(map[string]*bufEntry),
	}
	a.registerMetrics()
	return a, nil
}

func (a *Archiver) registerMetrics() {
	a.bytesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mio_sink_gcs_bytes_written_total",
		Help: "Total bytes written to GCS/MinIO by channel_type.",
	}, []string{"channel_type"})

	a.inflightGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "mio_sink_gcs_inflight_files",
		Help: "Number of open (not yet flushed) writer buffers.",
	})

	a.flushTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mio_sink_gcs_flush_total",
		Help: "Total flush attempts by outcome (success|error).",
	}, []string{"outcome"})
}

// Run starts the consume-encode-write loop and blocks until ctx is done.
func (a *Archiver) Run(ctx context.Context) error {
	cons, err := a.ensureConsumer(ctx)
	if err != nil {
		return err
	}

	// Background ticker for age-based flush.
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	msgCh := make(chan jetstream.Msg, a.cfg.MaxInflight)

	// Consumer goroutine: push messages into msgCh.
	cc, err := cons.Consume(func(msg jetstream.Msg) {
		select {
		case msgCh <- msg:
		case <-ctx.Done():
			_ = msg.Nak()
		}
	})
	if err != nil {
		return fmt.Errorf("archiver: start consume: %w", err)
	}
	defer cc.Stop()

	for {
		select {
		case <-ctx.Done():
			a.log.Info("archiver: context done, flushing all writers")
			return a.flushAll(context.WithoutCancel(ctx))

		case <-ticker.C:
			if err := a.flushAged(ctx); err != nil {
				a.log.Error("archiver: age-flush error", "err", err)
			}

		case msg, ok := <-msgCh:
			if !ok {
				return nil
			}
			if err := a.handleMsg(ctx, msg); err != nil {
				a.log.Error("archiver: handle msg", "err", err)
				_ = msg.Nak()
			}
		}
	}
}

// ensureConsumer attaches to (or creates) the durable pull consumer.
// The target stream is provisioned externally; sink only creates its consumer.
func (a *Archiver) ensureConsumer(ctx context.Context) (jetstream.Consumer, error) {
	stream, err := a.js.Stream(ctx, a.cfg.Stream)
	if err != nil {
		return nil, fmt.Errorf("archiver: lookup stream %q: %w", a.cfg.Stream, err)
	}

	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       a.cfg.Durable,
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       60 * time.Second,
		MaxAckPending: a.cfg.MaxInflight,
		MaxDeliver:    -1, // never give up; archival never drops
		ReplayPolicy:  jetstream.ReplayInstantPolicy,
		FilterSubject: a.cfg.Subject,
	})
	if err != nil {
		return nil, fmt.Errorf("archiver: ensure consumer %q: %w", a.cfg.Durable, err)
	}
	return cons, nil
}

// handleMsg decodes one JetStream message, appends to the correct buffer,
// and triggers a flush if the size threshold is crossed.
func (a *Archiver) handleMsg(ctx context.Context, raw jetstream.Msg) error {
	meta, err := raw.Metadata()
	if err != nil {
		return fmt.Errorf("archiver: msg metadata: %w", err)
	}
	seq := meta.Sequence.Stream

	var msg miov1.Message
	if err := proto.Unmarshal(raw.Data(), &msg); err != nil {
		// Undecodable proto: term so it doesn't block the consumer.
		_ = raw.Term()
		a.log.Error("archiver: proto unmarshal failed; message terminated", "seq", seq)
		return nil
	}

	line, err := encode.ToNDJSONLine(&msg)
	if err != nil {
		_ = raw.Term()
		a.log.Error("archiver: ndjson encode failed; message terminated", "seq", seq, "err", err)
		return nil
	}
	line = append(line, '\n')

	// Derive partition key from channel_type + received_at UTC date.
	var ts time.Time
	if msg.ReceivedAt != nil {
		ts = msg.ReceivedAt.AsTime()
	} else {
		ts = time.Now().UTC()
	}
	partPath := partition.Path(msg.ChannelType, ts)

	a.mu.Lock()
	entry, ok := a.buffers[partPath]
	if !ok {
		w, err := writer.New(ctx, a.cfg.WriterCfg)
		if err != nil {
			a.mu.Unlock()
			return fmt.Errorf("archiver: new writer for %q: %w", partPath, err)
		}
		entry = &bufEntry{
			w:        w,
			seqStart: seq,
			seqEnd:   seq,
			openedAt: time.Now(),
		}
		a.buffers[partPath] = entry
		a.inflightGauge.Inc()
	} else {
		entry.seqEnd = seq
	}
	_, _ = entry.w.Write(line)
	entry.pendingMsgs = append(entry.pendingMsgs, raw)
	bufLen := entry.w.Len()
	a.mu.Unlock()

	a.bytesTotal.WithLabelValues(msg.ChannelType).Add(float64(len(line)))

	// Trigger size-based flush outside the lock.
	if bufLen >= a.cfg.FlushSize {
		return a.flushPartition(ctx, partPath)
	}
	return nil
}

// flushPartition finalises one open buffer to its object path, then acks all
// pending JetStream messages. Must be called without holding a.mu.
func (a *Archiver) flushPartition(ctx context.Context, partPath string) error {
	a.mu.Lock()
	entry, ok := a.buffers[partPath]
	if !ok {
		a.mu.Unlock()
		return nil // already flushed by another trigger
	}
	delete(a.buffers, partPath)
	a.inflightGauge.Dec()
	a.mu.Unlock()

	// cfg.WriterCfg.Prefix is normalised (empty or ends in "/") so simple
	// concatenation is safe. Empty prefix → write at bucket root unchanged.
	objectPath := a.cfg.WriterCfg.Prefix + partPath + "/" + filename.Build(a.cfg.Durable, entry.seqStart, entry.seqEnd)

	if err := entry.w.Flush(ctx, objectPath); err != nil {
		a.flushTotal.WithLabelValues("error").Inc()
		// Put entry back so messages are not lost; they will be redelivered.
		a.mu.Lock()
		if _, exists := a.buffers[partPath]; !exists {
			a.buffers[partPath] = entry
			a.inflightGauge.Inc()
		}
		a.mu.Unlock()
		return fmt.Errorf("archiver: flush %q: %w", objectPath, err)
	}

	a.flushTotal.WithLabelValues("success").Inc()
	a.log.Info("archiver: flushed", "object", objectPath, "msgs", len(entry.pendingMsgs))

	// Ack AFTER the final object exists — at-least-once guarantee.
	for _, m := range entry.pendingMsgs {
		if err := m.Ack(); err != nil {
			a.log.Warn("archiver: ack failed", "err", err)
		}
	}
	return nil
}

// flushAged flushes all buffers whose open time exceeds FlushAge.
func (a *Archiver) flushAged(ctx context.Context) error {
	a.mu.Lock()
	var aged []string
	for partPath, entry := range a.buffers {
		if time.Since(entry.openedAt) >= a.cfg.FlushAge {
			aged = append(aged, partPath)
		}
	}
	a.mu.Unlock()

	var firstErr error
	for _, partPath := range aged {
		if err := a.flushPartition(ctx, partPath); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// flushAll flushes every open buffer. Called on SIGTERM.
func (a *Archiver) flushAll(ctx context.Context) error {
	a.mu.Lock()
	keys := make([]string, 0, len(a.buffers))
	for k := range a.buffers {
		keys = append(keys, k)
	}
	a.mu.Unlock()

	var firstErr error
	for _, partPath := range keys {
		if err := a.flushPartition(ctx, partPath); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			a.log.Error("archiver: flush-all error", "partition", partPath, "err", err)
		}
	}
	a.nc.Close()
	return firstErr
}

// bufferCount returns the number of open writers (for tests/health).
func (a *Archiver) bufferCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.buffers)
}

// unused suppresses import cycle lint when bytes is imported transitively.
var _ = bytes.NewBuffer
