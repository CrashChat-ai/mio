// Package sender — outbound sender pool.
//
// Pool drains MESSAGES_OUTBOUND via the SDK ConsumeOutbound channel, applies
// per-account rate limiting, dispatches by channel_type, and calls the
// appropriate adapter's Send or Edit method.
//
// Ack semantics:
//   - 2xx             → Ack, write outbound_state
//   - 429             → Nak(max(Retry-After, jitter)) — respect platform backoff
//   - 5xx / network   → Nak(jitter) — transient, rely on MaxDeliver
//   - 4xx (non-429)   → Term — permanent, metrics-only (no DLQ for POC)
//   - unregistered ct → Term(reason="other")
//   - rate-limit deny → Nak(jitter) — in-process token exhausted
package sender

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/crashchat-ai/mio/pkg/channels"
	"github.com/crashchat-ai/mio/services/gateway/internal/ratelimit"
	"github.com/crashchat-ai/mio/services/gateway/store"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	sdk "github.com/crashchat-ai/mio/sdk-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	defaultWorkers = 8
	durableName    = "sender-pool"

	// Jitter range for Nak delays: [minJitter, maxJitter).
	minJitter = 500 * time.Millisecond
	maxJitter = 2 * time.Second
)

// PoolConfig holds Pool construction parameters.
type PoolConfig struct {
	// Workers is the number of concurrent sender goroutines. Default: 8.
	Workers int

	// StreamOutbound is the JetStream stream name for outbound messages.
	// Defaults to "MESSAGES_OUTBOUND".
	StreamOutbound string

	Logger *slog.Logger
}

// Pool is the outbound sender pool. It consumes from MESSAGES_OUTBOUND,
// applies rate limiting, and dispatches to channel adapters.
type Pool struct {
	dispatcher *Dispatcher
	sdkClient  *sdk.Client
	limiter    *ratelimit.Limiter
	state      *store.OutboundState
	cfg        PoolConfig
	logger     *slog.Logger

	sentTotal    *prometheus.CounterVec
	retryTotal   *prometheus.CounterVec
	termTotal    *prometheus.CounterVec
	editFallback *prometheus.CounterVec
}

// NewPool constructs a Pool. Call Start to begin consuming.
func NewPool(
	dispatcher *Dispatcher,
	sdkClient *sdk.Client,
	limiter *ratelimit.Limiter,
	state *store.OutboundState,
	cfg PoolConfig,
	reg prometheus.Registerer,
) *Pool {
	if cfg.Workers <= 0 {
		cfg.Workers = defaultWorkers
	}
	if cfg.StreamOutbound == "" {
		cfg.StreamOutbound = "MESSAGES_OUTBOUND"
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}

	factory := promauto.With(reg)
	return &Pool{
		dispatcher: dispatcher,
		sdkClient:  sdkClient,
		limiter:    limiter,
		state:      state,
		cfg:        cfg,
		logger:     cfg.Logger,

		sentTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mio_gateway_outbound_sent_total",
			Help: "Outbound messages delivered, by channel_type and outcome.",
		}, []string{"channel_type", "outcome"}),

		retryTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mio_gateway_outbound_retry_total",
			Help: "Outbound Nak'd for retry, by channel_type and http_status bucket.",
		}, []string{"channel_type", "http_status"}),

		termTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mio_gateway_outbound_terminated_total",
			Help: "Outbound terminated (no redelivery), by channel_type and reason.",
		}, []string{"channel_type", "reason"}),

		editFallback: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mio_gateway_outbound_edit_fallback_total",
			Help: "Edits that fell back to fresh Send because outbound_state was missing.",
		}, []string{"channel_type", "reason"}),
	}
}

// Start launches the worker pool. Blocks until ctx is cancelled.
// Call from a goroutine alongside the HTTP server.
func (p *Pool) Start(ctx context.Context) error {
	deliveries, err := p.sdkClient.ConsumeOutbound(ctx, p.cfg.StreamOutbound, durableName)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	for range p.cfg.Workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for d := range deliveries {
				p.handle(ctx, d)
			}
		}()
	}
	wg.Wait()
	return nil
}

// handle processes a single CommandDelivery.
func (p *Pool) handle(ctx context.Context, d sdk.CommandDelivery) {
	cmd := d.Cmd()
	channelType := cmd.GetChannelType()

	// ── 1. Dispatch — unregistered channel_type → Term ────────────────────
	adapter := p.dispatcher.ForCommand(cmd)
	if adapter == nil {
		p.logger.Warn("sender: unregistered channel_type",
			"channel_type", channelType, "cmd_id", cmd.GetId())
		p.termTotal.WithLabelValues(channelType, "other").Inc()
		_ = d.Term()
		return
	}

	// ── 2. Rate-limit gate ─────────────────────────────────────────────────
	key := adapter.RateLimitKey(cmd)
	if key == "" {
		key = cmd.GetAccountId()
	}
	if !p.limiter.Allow(key) {
		_ = d.Nak(jitter())
		return
	}

	// ── 3. Resolve edit target from outbound_state ─────────────────────────
	isEdit := p.resolveEdit(cmd, channelType)

	// ── 4. Dispatch to adapter ─────────────────────────────────────────────
	var externalID string
	var callErr error

	if isEdit {
		callErr = adapter.Edit(ctx, cmd)
	} else {
		externalID, callErr = adapter.Send(ctx, cmd)
	}

	// ── 5. HTTP outcome routing ────────────────────────────────────────────
	if callErr == nil {
		if !isEdit && externalID != "" {
			p.state.Set(cmd.GetId(), externalID)
		}
		p.sentTotal.WithLabelValues(channelType, "ok").Inc()
		_ = d.Ack()
		return
	}

	// Try to classify via channels.DeliveryError interface.
	var de channels.DeliveryError
	if asDeliveryError(callErr, &de) {
		p.routeDeliveryError(d, cmd, channelType, de)
		return
	}

	// Network / unknown error → Nak (transient).
	p.logger.Error("sender: network/unknown error",
		"channel_type", channelType, "cmd_id", cmd.GetId(), "err", callErr)
	p.retryTotal.WithLabelValues(channelType, "network").Inc()
	_ = d.Nak(jitter())
}

// routeDeliveryError applies Nak/Term based on the channels.DeliveryError classification.
func (p *Pool) routeDeliveryError(
	d sdk.CommandDelivery,
	cmd *miov1.SendCommand,
	channelType string,
	de channels.DeliveryError,
) {
	switch {
	case de.IsRateLimited():
		delay := retryAfterDelay(de.RetryAfterSeconds())
		p.logger.Warn("sender: 429 — nak with Retry-After delay",
			"channel_type", channelType, "delay", delay, "cmd_id", cmd.GetId())
		p.retryTotal.WithLabelValues(channelType, "429").Inc()
		_ = d.Nak(delay)

	case de.IsRetryable():
		p.retryTotal.WithLabelValues(channelType, "5xx").Inc()
		_ = d.Nak(jitter())

	default:
		// 4xx (non-429) — permanent.
		reason := classify4xx(de)
		p.termTotal.WithLabelValues(channelType, reason).Inc()
		p.logger.Error("sender: 4xx — terminating",
			"channel_type", channelType, "reason", reason, "cmd_id", cmd.GetId(),
			"err", de.Error())
		_ = d.Term()
	}
}

// resolveEdit determines whether the command is an Edit or Send.
// Falls back to Send (and increments metric) when outbound_state is absent.
// Mutates cmd.EditOfExternalId when resolved from state.
func (p *Pool) resolveEdit(cmd *miov1.SendCommand, channelType string) bool {
	if cmd.GetEditOfExternalId() != "" {
		return true
	}

	correlator := ""
	if attrs := cmd.GetAttributes(); attrs != nil {
		correlator = attrs["replaces_send_id"]
	}
	if correlator == "" {
		correlator = cmd.GetEditOfMessageId()
	}
	if correlator == "" {
		return false
	}

	extID, ok := p.state.Get(correlator)
	if !ok {
		p.editFallback.WithLabelValues(channelType, "state_missing").Inc()
		p.logger.Warn("sender: outbound_state miss — falling back to Send",
			"correlator", correlator, "cmd_id", cmd.GetId())
		return false
	}

	cmd.EditOfExternalId = extID
	return true
}

// asDeliveryError checks if err (or any error in its chain) implements
// channels.DeliveryError, assigning it to *target. Returns true on success.
func asDeliveryError(err error, target *channels.DeliveryError) bool {
	// Walk the error chain manually since errors.As requires a concrete type.
	for err != nil {
		if de, ok := err.(channels.DeliveryError); ok { //nolint:errorlint
			*target = de
			return true
		}
		// Unwrap one level.
		type unwrapper interface{ Unwrap() error }
		uw, ok := err.(unwrapper)
		if !ok {
			break
		}
		err = uw.Unwrap()
	}
	return false
}

// retryAfterDelay computes the Nak delay for a 429 response.
// Uses max(retryAfterSecs, minJitter) then adds jitter spread.
func retryAfterDelay(retryAfterSecs int) time.Duration {
	base := time.Duration(retryAfterSecs) * time.Second
	j := jitter()
	if base > j {
		return base
	}
	return j
}

// classify4xx maps a channels.DeliveryError to a bounded reason label.
// Bounded set: auth, forbidden, not_found, bad_request, refresh_failed, other.
func classify4xx(de channels.DeliveryError) string {
	// Adapters can override the default status-code mapping by implementing
	// Reason() — useful for distinguishing OAuth-refresh-endpoint failures
	// from regular Cliq-API auth failures (operationally different fixes).
	type reasonProvider interface{ Reason() string }
	if rp, ok := de.(reasonProvider); ok {
		if r := rp.Reason(); r != "" {
			return r
		}
	}
	type statusCoder interface{ StatusCode() int }
	if sc, ok := de.(statusCoder); ok {
		switch sc.StatusCode() {
		case http.StatusUnauthorized:
			return "auth"
		case http.StatusForbidden:
			return "forbidden"
		case http.StatusNotFound:
			return "not_found"
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			return "bad_request"
		}
	}
	return "other"
}

// jitter returns a random delay in [minJitter, maxJitter).
func jitter() time.Duration {
	spread := maxJitter - minJitter
	return minJitter + time.Duration(rand.Int64N(int64(spread)))
}
