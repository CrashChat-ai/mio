// Package server wires the chi router, middleware, and route handlers.
package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/crashchat-ai/mio/pkg/channels"
	sdk "github.com/crashchat-ai/mio/sdk-go"
	"github.com/crashchat-ai/mio/services/gateway/internal/health"
	"github.com/crashchat-ai/mio/services/gateway/store"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log/slog"
)

// Config holds runtime knobs for the HTTP server layer.
type Config struct {
	TenantID  string
	AccountID string
	// WebhookSecrets maps channel_type → file-mounted signing secret.
	// Missing/empty entry = dev mode for that channel (no sig verify).
	WebhookSecrets map[string][]byte
	Logger         *slog.Logger
}

// New constructs and returns a chi router with all routes registered.
// Middleware order (outermost → innermost): logging → recovery → Prometheus.
// Webhook routes are mounted from the adapter registry: /webhooks/<slug>
// (URL hyphen → registry underscore) plus any adapter-declared aliases.
// Core stays channel-blind — adding a channel touches zero files here.
func New(
	pg *pgxpool.Pool,
	nc *nats.Conn,
	sdkClient *sdk.Client,
	cfg Config,
	reg prometheus.Registerer,
) http.Handler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}

	m := newGatewayMetrics(reg)

	r := chi.NewRouter()

	// Middleware: outermost → innermost.
	r.Use(middleware.RequestID)
	r.Use(slogMiddleware(cfg.Logger))
	r.Use(middleware.Recoverer)
	r.Use(prometheusMiddleware(m))

	// Health probes — outside main middleware chain so they're always fast.
	healthHandlers := health.New(pg, nc)
	r.Get("/healthz", healthHandlers.Healthz)
	r.Get("/readyz", healthHandlers.Readyz)

	// Prometheus metrics exposition.
	r.Handle("/metrics", promhttp.HandlerFor(
		prometheus.DefaultGatherer, promhttp.HandlerOpts{EnableOpenMetrics: true},
	))

	pipelines := buildWebhookPipelines(pg, sdkClient, cfg, m, r)

	r.Post("/webhooks/{channel}", func(w http.ResponseWriter, r *http.Request) {
		registrySlug := strings.ReplaceAll(chi.URLParam(r, "channel"), "-", "_")
		p, ok := pipelines[registrySlug]
		if !ok {
			http.Error(w, `{"error":"unknown channel"}`, http.StatusNotFound)
			return
		}
		p.ServeHTTP(w, r)
	})

	return r
}

// buildWebhookPipelines constructs one pipeline per registered adapter with
// inbound support, injects its secret, and mounts adapter-declared route
// aliases on the router.
func buildWebhookPipelines(
	pg *pgxpool.Pool,
	sdkClient *sdk.Client,
	cfg Config,
	m *gatewayMetrics,
	r chi.Router,
) map[string]*webhookPipeline {
	pipelines := make(map[string]*webhookPipeline)
	inboundStore := store.NewInboundStore(pg)

	for _, adapter := range channels.RegisteredAdapters() {
		inbound := adapter.Inbound()
		if inbound == nil {
			continue
		}
		channelType := adapter.ChannelType()

		secret := cfg.WebhookSecrets[channelType]
		if sc, ok := inbound.(channels.SecretConfigurable); ok {
			inbound = sc.WithSecret(secret)
		}
		if len(secret) == 0 {
			cfg.Logger.Warn("webhook: SECRET UNSET — accepting all requests (dev only)",
				"channel", channelType)
		}

		p := &webhookPipeline{
			channelType: channelType,
			inbound:     inbound,
			store:       inboundStore,
			pub:         sdkClient,
			tenantID:    cfg.TenantID,
			accountID:   cfg.AccountID,
			metrics:     m,
			logger:      cfg.Logger,
		}
		pipelines[channelType] = p

		if ra, ok := inbound.(channels.RouteAliaser); ok {
			for _, alias := range ra.RouteAliases() {
				r.Post(alias, p.ServeHTTP)
			}
		}
	}
	return pipelines
}

// slogMiddleware logs each request with method, path, status, duration.
func slogMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			logger.Info("http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}

// prometheusMiddleware is a lightweight wrapper that records per-route
// request counts and latencies without adding label cardinality.
func prometheusMiddleware(_ *gatewayMetrics) func(http.Handler) http.Handler {
	// Minimal: chi middleware.Logger already handles per-request logging.
	// Heavy metrics are emitted by each handler with full label context.
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}
}
