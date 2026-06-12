// Package credrefresh proactively rotates expiring OAuth credentials so
// outbound sends never hit a dead token. Runs inside cmd/admin — the only
// binary holding the credential cipher.
package credrefresh

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/crashchat-ai/mio/pkg/channels"
	"github.com/crashchat-ai/mio/services/gateway/internal/crypto"
	"github.com/crashchat-ai/mio/services/gateway/store"
)

const (
	DefaultInterval = 10 * time.Minute
	DefaultLead     = 30 * time.Minute
)

type Refresher struct {
	pool     *pgxpool.Pool
	cipher   crypto.Cipher
	adapters map[string]channels.Adapter
	interval time.Duration
	lead     time.Duration
	logger   *slog.Logger

	refreshTotal *prometheus.CounterVec
}

func New(
	pool *pgxpool.Pool,
	cipher crypto.Cipher,
	registry []channels.Adapter,
	interval, lead time.Duration,
	logger *slog.Logger,
	reg prometheus.Registerer,
) *Refresher {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if lead <= 0 {
		lead = DefaultLead
	}
	if logger == nil {
		logger = slog.Default()
	}
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	adapters := make(map[string]channels.Adapter, len(registry))
	for _, a := range registry {
		adapters[a.ChannelType()] = a
	}
	return &Refresher{
		pool:     pool,
		cipher:   cipher,
		adapters: adapters,
		interval: interval,
		lead:     lead,
		logger:   logger,
		refreshTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "mio_credential_refresh_total",
			Help: "Proactive credential refresh attempts by channel_type and result.",
		}, []string{"channel_type", "result"}),
	}
}

// Run ticks until ctx is done. Sequential per tick — one admin instance,
// no cross-account ordering requirements.
func (r *Refresher) Run(ctx context.Context) {
	t := time.NewTicker(r.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.RefreshExpiring(ctx)
		}
	}
}

// RefreshExpiring scans and refreshes every credential expiring within lead.
func (r *Refresher) RefreshExpiring(ctx context.Context) {
	expiring, err := store.ListExpiringCredentials(ctx, r.pool, r.lead)
	if err != nil {
		r.logger.Error("credrefresh: scan", "err", err)
		return
	}
	for _, e := range expiring {
		r.refreshOne(ctx, e)
	}
}

func (r *Refresher) refreshOne(ctx context.Context, e store.ExpiringCredential) {
	adapter, ok := r.adapters[e.ChannelType]
	if !ok {
		r.refreshTotal.WithLabelValues(e.ChannelType, "no_adapter").Inc()
		r.logger.Warn("credrefresh: no adapter registered",
			"channel", e.ChannelType, "account_id", e.AccountID)
		return
	}

	row, err := store.GetCredential(ctx, r.pool, r.cipher, e.AccountID, r.logger)
	if err != nil {
		r.refreshTotal.WithLabelValues(e.ChannelType, "read_error").Inc()
		r.logger.Error("credrefresh: read", "account_id", e.AccountID, "err", err)
		return
	}

	cur := channels.Credential{
		AccessToken:  row.Plaintext.AccessToken,
		RefreshToken: row.Plaintext.RefreshToken,
		ExpiresAt:    row.Plaintext.ExpiresAt,
		Extras:       row.Plaintext.Extras,
	}
	fresh, err := adapter.Credentials().RefreshCredential(ctx, cur)
	if err != nil {
		r.refreshTotal.WithLabelValues(e.ChannelType, "refresh_error").Inc()
		r.logger.Warn("credrefresh: refresh failed — retrying next tick",
			"channel", e.ChannelType, "account_id", e.AccountID, "err", err)
		return
	}
	if fresh.AccessToken == cur.AccessToken && fresh.ExpiresAt.Equal(cur.ExpiresAt) {
		r.refreshTotal.WithLabelValues(e.ChannelType, "noop").Inc()
		return
	}

	payload := store.CredentialPayload{
		AccessToken:  fresh.AccessToken,
		RefreshToken: fresh.RefreshToken,
		ExpiresAt:    fresh.ExpiresAt,
		Extras:       fresh.Extras,
	}
	if err := store.PutCredential(ctx, r.pool, r.cipher, e.AccountID, row.AuthKind, payload); err != nil {
		r.refreshTotal.WithLabelValues(e.ChannelType, "write_error").Inc()
		r.logger.Error("credrefresh: persist", "account_id", e.AccountID, "err", err)
		return
	}
	r.refreshTotal.WithLabelValues(e.ChannelType, "refreshed").Inc()
	r.logger.Info("credrefresh: rotated",
		"channel", e.ChannelType, "account_id", e.AccountID,
		"new_expiry", fresh.ExpiresAt)
}
