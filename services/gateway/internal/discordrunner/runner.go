// Package discordrunner is the gateway's Discord gateway-WS ingest runner. It
// opens a long-lived websocket via bwmarrin/discordgo (which owns identify,
// heartbeat, resume, and reconnect), wraps each raw dispatch event as the
// channels/discord Envelope, and feeds it to the SAME channel-agnostic ingest
// tail (server.Ingester) the HTTP webhook uses. It is gateway-internal infra,
// not part of the pkg/channels contract; discordgo lives here and in
// channels/discord only.
package discordrunner

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"github.com/crashchat-ai/mio/services/gateway/internal/server"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	ingestQueueDepth = 256
	enqueueTimeout   = 3 * time.Second
)

// wantedEvents is the dispatch-type allowlist fed to Normalize. Everything
// else (presence, typing, guild sync) is dropped at the runner so the
// normalize seam only sees message-shaped traffic.
var wantedEvents = map[string]bool{
	"MESSAGE_CREATE":          true,
	"MESSAGE_UPDATE":          true,
	"MESSAGE_DELETE":          true,
	"MESSAGE_REACTION_ADD":    true,
	"MESSAGE_REACTION_REMOVE": true,
}

// Deps wires the runner to the gateway's shared infra. Inbound is the typed
// Discord Normalize seam (channels/discord Adapter.Inbound); the runner needs
// the concrete adapter only for this — everything past Normalize is
// channel-blind.
type Deps struct {
	ChannelType string
	BotToken    string
	Inbound     channels.InboundAdapter
	PG          *pgxpool.Pool
	Pub         server.InboundPublisher
	Accounts    server.AccountResolver
	TenantID    string
	AccountID   string
	Registerer  prometheus.Registerer
	Logger      *slog.Logger
}

// ingester is the seam Start drives; satisfied by *server.Ingester (real) and
// a fake in tests.
type ingester interface {
	Ingest(ctx context.Context, msg *miov1.Message) (string, error)
}

// envelope mirrors channels/discord.Envelope — the runner re-wraps discordgo's
// raw event into {"t":..., "d":...} bytes so Normalize stays a pure JSON seam.
type envelope struct {
	T string          `json:"t"`
	D json.RawMessage `json:"d"`
}

// Start connects to the Discord gateway and runs until ctx is cancelled. It
// blocks; callers launch it in a goroutine under the gateway's poolCtx.
// discordgo owns the reconnect loop (ShouldReconnectOnError default true);
// a failed initial Open (bad token) returns the error instead of retrying —
// a retry storm on revoked credentials never succeeds.
func Start(ctx context.Context, deps Deps) error {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if deps.BotToken == "" {
		return errors.New("discordrunner: bot token is required")
	}

	reg := deps.Registerer
	if reg == nil {
		reg = prometheus.NewRegistry()
	}
	ing := server.NewIngester(
		deps.ChannelType, deps.Inbound, deps.PG, deps.Pub, deps.Accounts,
		deps.TenantID, deps.AccountID, reg, logger,
	)

	s, err := discordgo.New("Bot " + deps.BotToken)
	if err != nil {
		return err
	}
	// Message content requires the privileged MESSAGE CONTENT intent, enabled
	// in the developer portal alongside these gateway intents.
	s.Identify.Intents = discordgo.IntentsGuildMessages |
		discordgo.IntentsDirectMessages |
		discordgo.IntentsGuildMessageReactions |
		discordgo.IntentsDirectMessageReactions |
		discordgo.IntentMessageContent

	r := &runner{ing: ing, inbound: deps.Inbound, logger: logger}
	queue := make(chan []byte, ingestQueueDepth)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.ingestLoop(ctx, queue)
	}()

	s.AddHandler(func(_ *discordgo.Session, evt *discordgo.Event) {
		r.enqueue(ctx, evt, queue)
	})

	if err := s.Open(); err != nil {
		close(queue)
		wg.Wait()
		return err
	}
	logger.Info("discordrunner: gateway connected")

	<-ctx.Done()
	if cerr := s.Close(); cerr != nil {
		logger.Warn("discordrunner: close", "err", cerr)
	}
	close(queue)
	wg.Wait()
	return ctx.Err()
}

// runner holds the loop state; constructed directly in tests with a fake
// ingester.
type runner struct {
	ing     ingester
	inbound channels.InboundAdapter
	logger  *slog.Logger
}

// enqueue filters raw dispatch events and hands wrapped payloads to the
// bounded ingest queue so a slow DB never blocks discordgo's event loop
// (blocking a handler stalls heartbeats).
func (r *runner) enqueue(ctx context.Context, evt *discordgo.Event, queue chan<- []byte) {
	if evt == nil || !wantedEvents[evt.Type] || len(evt.RawData) == 0 {
		return
	}
	payload, err := json.Marshal(envelope{T: evt.Type, D: append(json.RawMessage(nil), evt.RawData...)})
	if err != nil {
		r.logger.Warn("discordrunner: envelope marshal failed", "type", evt.Type, "err", err)
		return
	}
	select {
	case queue <- payload:
	case <-ctx.Done():
	case <-time.After(enqueueTimeout):
		r.logger.Warn("discordrunner: ingest queue full, dropping event (idempotency backstops redelivery)")
	}
}

// ingestLoop normalizes and ingests queued payloads serially.
// EnsureUniqueMessage dedups any reconnect redelivery on
// (account_id, source_message_id).
func (r *runner) ingestLoop(ctx context.Context, queue <-chan []byte) {
	for {
		select {
		case <-ctx.Done():
			// Drain nothing further; pending events redeliver via resume.
			return
		case payload, ok := <-queue:
			if !ok {
				return
			}
			r.ingestOne(ctx, payload)
		}
	}
}

func (r *runner) ingestOne(ctx context.Context, payload []byte) {
	msg, err := r.inbound.Normalize(payload)
	if err != nil {
		if errors.Is(err, channels.ErrNormalizeSoft) {
			return
		}
		r.logger.Warn("discordrunner: normalize failed", "err", err)
		return
	}
	outcome, ierr := r.ing.Ingest(ctx, msg)
	if ierr != nil && !errors.Is(ierr, server.ErrUnroutable) {
		r.logger.Error("discordrunner: ingest failed", "outcome", outcome, "err", ierr)
	}
}
