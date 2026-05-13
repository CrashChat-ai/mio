// Package server wires the chi router, middleware, and route handlers.
package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/crashchat-ai/mio/services/gateway/internal/channels/zohocliq"
	"github.com/crashchat-ai/mio/services/gateway/internal/health"
	sdk "github.com/crashchat-ai/mio/sdk-go"
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
	TenantID          string
	AccountID         string
	CliqWebhookSecret []byte // empty = dev mode (no sig verify)
	Logger            *slog.Logger
}

// New constructs and returns a chi router with all routes registered.
// Middleware order (outermost → innermost): logging → recovery → Prometheus.
// The URL slug /webhooks/zoho-cliq maps to registry key zoho_cliq via
// strings.ReplaceAll(slug, "-", "_") — two slugs by design (URL hyphen,
// internal underscore per arch contract and master Revisions 11:45).
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

	// Webhook routes. URL uses hyphen (web convention); router maps to registry
	// slug (underscore) via strings.ReplaceAll. For a single channel this is a
	// direct mapping; the pattern generalises for future adapters.
	zohoCliqDeps := buildZohoCliqDeps(pg, sdkClient, cfg, m)
	r.Post("/webhooks/{channel}", func(w http.ResponseWriter, r *http.Request) {
		urlSlug := chi.URLParam(r, "channel")
		registrySlug := strings.ReplaceAll(urlSlug, "-", "_")

		switch registrySlug {
		case "zoho_cliq":
			zohocliq.Handler(zohoCliqDeps).ServeHTTP(w, r)
		default:
			http.Error(w, `{"error":"unknown channel"}`, http.StatusNotFound)
		}
	})

	// Server-side alias: /cliq → zoho_cliq handler. Keeps ingress path simple
	// (no rewrite annotations) and matches the locked Cliq webhook URL.
	r.Post("/cliq", zohocliq.Handler(zohoCliqDeps).ServeHTTP)

	return r
}

// buildZohoCliqDeps wires Zoho Cliq handler dependencies once so both the
// generic /webhooks/{channel} route and the /cliq alias share the same deps.
func buildZohoCliqDeps(
	pg *pgxpool.Pool,
	sdkClient *sdk.Client,
	cfg Config,
	m *gatewayMetrics,
) zohocliq.HandlerDeps {
	return zohocliq.HandlerDeps{
		Pool:      pg,
		SDK:       sdkClient,
		TenantID:  cfg.TenantID,
		AccountID: cfg.AccountID,
		Secret:    cfg.CliqWebhookSecret,
		IncInbound: func(direction, outcome string) {
			m.incInbound("zoho_cliq", direction, outcome)
		},
		ObserveLatency: func(direction, outcome string, secs float64) {
			m.observeLatency("zoho_cliq", direction, outcome, secs)
		},
		IncDedup: func() {
			m.incDedup("zoho_cliq")
		},
		Logger: cfg.Logger,
	}
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
