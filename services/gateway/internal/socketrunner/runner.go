// Package socketrunner is the gateway's Slack Socket Mode ingest runner. It
// opens a long-lived WebSocket via slack-go/slack/socketmode, ACKs each
// envelope immediately, and feeds the event_callback payload to the SAME
// channel-agnostic ingest tail (server.Ingester) the HTTP webhook uses. It is
// gateway-internal infra, not part of the pkg/channels contract; slack-go lives
// here and in channels/slack only.
package socketrunner

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"github.com/crashchat-ai/mio/services/gateway/internal/server"
	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	ingestQueueDepth = 256
	ackTimeout       = 3 * time.Second
)

// Deps wires the runner to the gateway's shared infra. inbound is the typed
// Slack Normalize seam (channels/slack Adapter.Inbound); the runner needs the
// concrete adapter only for this — everything past Normalize is channel-blind.
type Deps struct {
	ChannelType string
	BotToken    string
	AppToken    string
	Inbound     channels.InboundAdapter
	PG          *pgxpool.Pool
	Pub         server.InboundPublisher
	Accounts    server.AccountResolver
	TenantID    string
	AccountID   string
	Registerer  prometheus.Registerer
	Logger      *slog.Logger
}

// ingester is the seam Start drives; satisfied by *server.Ingester (real) and a
// fake in tests.
type ingester interface {
	Ingest(ctx context.Context, msg *miov1.Message) (string, error)
}

// socketClient abstracts *socketmode.Client so the connect/ack/dispatch loop is
// unit-testable against a faked events channel.
type socketClient interface {
	Run(ctx context.Context) error
	Events() <-chan socketmode.Event
	Ack(req socketmode.Request)
}

// Start connects to Slack Socket Mode and runs until ctx is cancelled. It
// blocks; callers launch it in a goroutine under the gateway's poolCtx.
func Start(ctx context.Context, deps Deps) error {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if deps.BotToken == "" || deps.AppToken == "" {
		return errors.New("socketrunner: bot and app tokens are required")
	}

	api := slackapi.New(deps.BotToken, slackapi.OptionAppLevelToken(deps.AppToken))
	if _, err := api.AuthTestContext(ctx); err != nil {
		if isPermanentAuthError(err.Error()) {
			logger.Error("socketrunner: permanent auth failure on connect, not starting", "err", err)
			return err
		}
		return err
	}

	reg := deps.Registerer
	if reg == nil {
		reg = prometheus.NewRegistry()
	}
	ing := server.NewIngester(
		deps.ChannelType, deps.Inbound, deps.PG, deps.Pub, deps.Accounts,
		deps.TenantID, deps.AccountID, reg, logger,
	)

	sm := socketmode.New(api)
	r := &runner{
		client:  &liveClient{sm: sm},
		inbound: deps.Inbound,
		ing:     ing,
		logger:  logger,
		backoff: reconnectBackoff,
	}
	return r.run(ctx)
}

// runner holds the loop state; constructed directly in tests with a fake client.
type runner struct {
	client  socketClient
	inbound channels.InboundAdapter
	ing     ingester
	logger  *slog.Logger
	backoff time.Duration
}

// run starts the ingest workers, the connect/reconnect loop, and dispatches
// events until ctx is cancelled.
func (r *runner) run(ctx context.Context) error {
	queue := make(chan []byte, ingestQueueDepth)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.ingestLoop(ctx, queue)
	}()

	go r.connectLoop(ctx)

	r.dispatchLoop(ctx, queue)
	close(queue)
	wg.Wait()
	return ctx.Err()
}

// dispatchLoop reads the Events channel, ACKs each events_api envelope
// immediately (within the ~3s budget), and hands the payload to the bounded
// ingest queue so a slow DB never blows the ACK deadline.
func (r *runner) dispatchLoop(ctx context.Context, queue chan<- []byte) {
	events := r.client.Events()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			r.handle(ctx, evt, queue)
		}
	}
}

func (r *runner) handle(ctx context.Context, evt socketmode.Event, queue chan<- []byte) {
	switch evt.Type {
	case socketmode.EventTypeEventsAPI:
		if evt.Request == nil {
			return
		}
		payload := append([]byte(nil), evt.Request.Payload...)
		r.client.Ack(*evt.Request)
		select {
		case queue <- payload:
		case <-ctx.Done():
		case <-time.After(ackTimeout):
			r.logger.Warn("socketrunner: ingest queue full, dropping event (idempotency backstops redelivery)")
		}
	case socketmode.EventTypeDisconnect:
		r.logger.Info("socketrunner: disconnect (auto-reconnect)")
	case socketmode.EventTypeInvalidAuth:
		r.logger.Error("socketrunner: invalid auth event")
	}
}

// ingestLoop normalizes and ingests queued payloads serially. EnsureUniqueMessage
// dedups any reconnect redelivery on (account_id, source_message_id).
func (r *runner) ingestLoop(ctx context.Context, queue <-chan []byte) {
	for {
		select {
		case <-ctx.Done():
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
		r.logger.Warn("socketrunner: normalize failed", "err", err)
		return
	}
	outcome, ierr := r.ing.Ingest(ctx, msg)
	if ierr != nil && !errors.Is(ierr, server.ErrUnroutable) {
		r.logger.Error("socketrunner: ingest failed", "outcome", outcome, "err", ierr)
	}
}
