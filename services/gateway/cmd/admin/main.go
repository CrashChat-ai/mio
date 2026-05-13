// Command admin runs the control-plane connect-go server.
//
// Binds 127.0.0.1:9090 by default (loopback-only). Non-loopback deploys
// must wrap this with a reverse-proxy that terminates TLS and enforces
// real authn/authz — the AllowedCIDRs middleware is a sanity check, not a
// security boundary.
//
// IMPORTANT: cmd/admin does NOT run database migrations. It asserts the
// admin schema is present (credentials table) and exits non-zero if not.
// Operators must boot cmd/gateway first to apply migrations.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	// Side-effect import: registers zoho_cliq adapter via init().
	_ "github.com/crashchat-ai/mio/channels/all"

	"github.com/crashchat-ai/mio/pkg/channels"
	"github.com/crashchat-ai/mio/services/gateway/internal/admin"
	"github.com/crashchat-ai/mio/services/gateway/internal/crypto"
	"github.com/crashchat-ai/mio/services/gateway/store"
	"github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1/adminv1connect"
	sdk "github.com/crashchat-ai/mio/sdk-go"
)

type flags struct {
	addr      string
	dbDSN     string
	natsURL   string
	ageKey    string
	publicURL string
	env       string
}

func parseFlags() flags {
	var f flags
	flag.StringVar(&f.addr, "addr", envDefault("MIO_ADMIN_ADDR", "127.0.0.1:9090"),
		"listener address; loopback by default")
	flag.StringVar(&f.dbDSN, "db-dsn", os.Getenv("MIO_POSTGRES_DSN"), "Postgres DSN")
	flag.StringVar(&f.natsURL, "nats-url", envDefault("MIO_NATS_URLS", "nats://localhost:4222"),
		"NATS URL (first of CSV)")
	flag.StringVar(&f.ageKey, "age-key", os.Getenv("MIO_AGE_KEY_FILE"), "age identity file path")
	flag.StringVar(&f.publicURL, "public-url", envDefault("MIO_ADMIN_PUBLIC_URL", "http://127.0.0.1:9090"),
		"externally-reachable URL for OAuth callbacks")
	flag.StringVar(&f.env, "env", envDefault("MIO_ENV", "dev"), "deploy environment (dev|staging|prod)")
	flag.Parse()
	return f
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	f := parseFlags()
	if f.dbDSN == "" {
		logger.Error("admin: MIO_POSTGRES_DSN / --db-dsn required")
		os.Exit(2)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Postgres pool.
	poolCtx, poolCancel := context.WithTimeout(ctx, 10*time.Second)
	pool, err := store.NewPool(poolCtx, f.dbDSN, 4)
	poolCancel()
	if err != nil {
		logger.Error("admin: postgres pool", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Schema-presence check.
	if err := admin.SchemaPresenceCheck(ctx, pool); err != nil {
		logger.Error("admin: schema presence check failed", "err", err)
		os.Exit(1)
	}
	logger.Info("admin: schema present")

	// Cipher: AgeFile if MIO_AGE_KEY_FILE set, otherwise Noop (dev only).
	cipher, err := buildCipher(f, logger)
	if err != nil {
		logger.Error("admin: cipher init", "err", err)
		os.Exit(1)
	}

	// SDK client (NATS) — optional. TailMessages returns CodeUnimplemented if absent.
	var sdkClient *sdk.Client
	if f.natsURL != "" {
		c, err := sdk.New(f.natsURL)
		if err != nil {
			logger.Warn("admin: SDK connect failed; TailMessages disabled", "err", err)
		} else {
			sdkClient = c
			defer sdkClient.Close()
		}
	}

	allowed, err := admin.ParseAllowedCIDRs(os.Getenv("MIO_ADMIN_ALLOW_CIDRS"))
	if err != nil {
		logger.Error("admin: parse allow CIDRs", "err", err)
		os.Exit(2)
	}

	metrics := admin.NewAdminMetrics(prometheus.DefaultRegisterer)
	registry := channels.RegisteredAdapters()
	logger.Info("admin: registered adapters", "count", len(registry))

	srv := admin.NewServer(admin.Deps{
		Pool:      pool,
		Cipher:    cipher,
		SDK:       sdkClient,
		Registry:  registry,
		Metrics:   metrics,
		Logger:    logger,
		PublicURL: f.publicURL,
	})
	srv.StartBackground(ctx)

	// Wire HTTP mux: connect-go path + /oauth/callback + /metrics + /healthz.
	mux := http.NewServeMux()
	path, handler := adminv1connect.NewAdminServiceHandler(srv)
	mux.Handle(path, handler)
	mux.HandleFunc("/oauth/callback", admin.OAuthCallbackHandler(srv, logger))
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap connect + callback in auth middleware. /metrics + /healthz stay
	// unauthed so the operator can scrape; auth is per-route.
	authed := http.NewServeMux()
	authed.Handle(path, admin.AuthMiddleware(allowed, logger)(handler))
	authed.HandleFunc("/oauth/callback",
		admin.AuthMiddleware(allowed, logger)(http.HandlerFunc(admin.OAuthCallbackHandler(srv, logger))).ServeHTTP)
	authed.Handle("/metrics", promhttp.Handler())
	authed.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	server := &http.Server{
		Addr:         f.addr,
		Handler:      authed,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0, // streams may run long
		IdleTimeout:  120 * time.Second,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("admin: shutdown requested")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownCtx)
		cancel()
	}()

	logger.Info("admin: listening", "addr", f.addr, "env", f.env, "public_url", f.publicURL)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("admin: serve", "err", err)
		os.Exit(1)
	}
}

// buildCipher selects AgeFileCipher when an age key path is configured,
// falls back to NoopCipher (panics in non-dev). version=1 for initial deploy.
func buildCipher(f flags, logger *slog.Logger) (crypto.Cipher, error) {
	if f.ageKey != "" {
		c, err := crypto.NewAgeFileCipher(f.ageKey, 1)
		if err != nil {
			return nil, fmt.Errorf("age cipher: %w", err)
		}
		logger.Info("admin: age cipher loaded", "key_version", c.KeyVersion())
		return c, nil
	}
	if f.env != "dev" {
		return nil, fmt.Errorf("MIO_AGE_KEY_FILE required outside dev (env=%q)", f.env)
	}
	logger.Warn("admin: no MIO_AGE_KEY_FILE set — using NoopCipher (dev only)")
	return crypto.NewNoopCipher(f.env), nil
}
