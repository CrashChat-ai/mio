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
	"sync"
	"time"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"github.com/crashchat-ai/mio/services/gateway/store"
	"log/slog"
)

const maxWebhookBody = 1 << 20

// InboundPublisher is the publish seam (satisfied by *sdk.Client). Exported so
// the Socket Mode runner can supply it to NewIngester.
type InboundPublisher interface {
	PublishInbound(ctx context.Context, msg *miov1.Message) error
}

// AccountResolver routes a webhook to an installed account. Satisfied by
// store.AccountResolver; nil disables DB resolution (env identity only).
// A non-nil error means the lookup failed — the pipeline responds 500 so
// the platform retries instead of misrouting via the env fallback.
type AccountResolver interface {
	Resolve(ctx context.Context, channelType, workspaceKey string) (store.ResolvedAccount, bool, error)
}

// webhookPipeline handles inbound webhooks for one channel adapter. The
// post-Normalize tail is single-sourced through *Ingester (shared with the
// Socket Mode runner); ingester is built lazily from these fields.
type webhookPipeline struct {
	channelType string
	inbound     channels.InboundAdapter
	store       channels.Store
	pub         InboundPublisher
	accounts    AccountResolver
	tenantID    string
	accountID   string
	metrics     *gatewayMetrics
	logger      *slog.Logger

	ingesterOnce sync.Once
	ingester     *Ingester
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

	outcome, _ := p.ingest(r.Context(), msg)
	status, errMsg := outcomeStatus(outcome)
	p.finish(w, start, outcome, status, errMsg)
}

// outcomeStatus maps an ingest outcome to the HTTP status + error body the
// webhook responds with. Socket Mode (P2) calls ingest directly and routes on
// the outcome string instead of HTTP.
func outcomeStatus(outcome string) (status int, errMsg string) {
	switch outcome {
	case "db_error":
		return http.StatusInternalServerError, "db error"
	case "publish_error":
		return http.StatusInternalServerError, "publish error"
	default:
		return http.StatusOK, ""
	}
}

// ingest delegates to the shared *Ingester (single-sourced with the Socket
// Mode runner). The Ingester is built once from the pipeline's fields so each
// HTTP pipeline reuses one ingester instance.
func (p *webhookPipeline) ingest(ctx context.Context, msg *miov1.Message) (outcome string, err error) {
	p.ingesterOnce.Do(func() {
		p.ingester = &Ingester{
			channelType: p.channelType,
			inbound:     p.inbound,
			store:       p.store,
			pub:         p.pub,
			accounts:    p.accounts,
			tenantID:    p.tenantID,
			accountID:   p.accountID,
			metrics:     p.metrics,
			logger:      p.logger,
		}
	})
	return p.ingester.Ingest(ctx, msg)
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
