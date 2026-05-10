// Command gateway is the mio-gateway HTTP server.
// It receives channel webhooks, normalises payloads to mio.v1.Message,
// publishes to MESSAGES_INBOUND, and drains MESSAGES_OUTBOUND via the
// sender pool which dispatches to per-channel adapters.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
	sdk "github.com/crashchat-ai/mio/sdk-go"
	"github.com/crashchat-ai/mio/gateway/internal/config"
	"github.com/crashchat-ai/mio/gateway/internal/ratelimit"
	"github.com/crashchat-ai/mio/gateway/internal/sender"
	"github.com/crashchat-ai/mio/gateway/internal/server"
	"github.com/crashchat-ai/mio/gateway/internal/store"

	// Blank-import each channel package to trigger its init() which calls
	// sender.RegisterAdapter(). P9: add _ "…/channels/slack" here only.
	_ "github.com/crashchat-ai/mio/gateway/internal/channels/zohocliq"
)

// version is injected at build time via -ldflags="-X main.version=<ver>".
var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("gateway: fatal", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	logger.Info("gateway starting", "version", version)

	// ── 1. Config ──────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	// ── 2. Postgres pool ───────────────────────────────────────────────────
	ctx := context.Background()
	pg, err := store.NewPool(ctx, cfg.PostgresDSN, int32(cfg.PgxMaxConns))
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	defer pg.Close()
	logger.Info("postgres: pool ready", "max_conns", cfg.PgxMaxConns)

	// ── 3. Migrations ──────────────────────────────────────────────────────
	if cfg.MigrateOnStart {
		logger.Info("postgres: running migrations")
		if err := store.MigrateUp(cfg.PostgresDSN); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		logger.Info("postgres: migrations complete")
	}

	// ── 4. NATS connection ─────────────────────────────────────────────────
	natsURL := cfg.NatsURLs[0]
	if len(cfg.NatsURLs) > 1 {
		natsURL = ""
		for i, u := range cfg.NatsURLs {
			if i > 0 {
				natsURL += ","
			}
			natsURL += u
		}
	}
	nc, err := nats.Connect(natsURL,
		nats.Name("mio-gateway/"+version),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return fmt.Errorf("nats: connect: %w", err)
	}
	defer nc.Drain() //nolint:errcheck
	logger.Info("nats: connected", "url", natsURL)

	// ── 5. JetStream stream + consumer provisioning ────────────────────────
	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("nats: jetstream: %w", err)
	}
	natsReplicas := 1
	if err := store.EnsureStreams(ctx, js, natsReplicas); err != nil {
		return fmt.Errorf("jetstream: ensure streams: %w", err)
	}
	if err := store.EnsureSenderConsumer(ctx, js); err != nil {
		return fmt.Errorf("jetstream: ensure sender-pool consumer: %w", err)
	}
	logger.Info("jetstream: streams + consumers provisioned",
		"inbound", store.StreamInbound,
		"outbound", store.StreamOutbound)

	// ── 6. SDK client ──────────────────────────────────────────────────────
	sdkReg := prometheus.NewRegistry()
	sdkClient, err := sdk.New(natsURL,
		sdk.WithName("mio-gateway/sdk/"+version),
		sdk.WithMetricsRegistry(sdkReg),
		sdk.WithMaxAckPending(32), // sender pool: higher concurrency than inbound consumer
		sdk.WithAckWait(30*time.Second),
	)
	if err != nil {
		return fmt.Errorf("sdk: %w", err)
	}
	defer sdkClient.Close()

	// ── 7. Sender pool ─────────────────────────────────────────────────────
	// Build dispatcher AFTER all init() blocks have run (Go guarantees this).
	// Every blank-imported channel package registered its adapter already.
	dispatcher := sender.New(sender.RegisteredAdapters())
	logger.Info("sender: dispatcher built",
		"adapters", len(sender.RegisteredAdapters()))

	// Root context for the sender pool; cancelled on SIGTERM.
	poolCtx, poolCancel := context.WithCancel(ctx)
	defer poolCancel()

	rateLimiter := ratelimit.New(poolCtx, prometheus.DefaultRegisterer, logger)
	outboundState := store.NewOutboundState()

	senderWorkers := cfg.SenderWorkers
	pool := sender.NewPool(dispatcher, sdkClient, rateLimiter, outboundState,
		sender.PoolConfig{
			Workers:        senderWorkers,
			StreamOutbound: store.StreamOutbound,
			Logger:         logger,
		},
		prometheus.DefaultRegisterer,
	)

	poolErrCh := make(chan error, 1)
	go func() {
		if err := pool.Start(poolCtx); err != nil {
			poolErrCh <- err
		}
		close(poolErrCh)
	}()
	logger.Info("sender: pool started", "workers", senderWorkers)

	// ── 8. HTTP server ─────────────────────────────────────────────────────
	serverCfg := server.Config{
		TenantID:          cfg.TenantID,
		AccountID:         cfg.AccountID,
		CliqWebhookSecret: []byte(cfg.CliqWebhookSecret),
		Logger:            logger,
	}
	handler := server.New(pg, nc, sdkClient, serverCfg, prometheus.DefaultRegisterer)

	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// ── 9. Graceful shutdown ───────────────────────────────────────────────
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		logger.Info("gateway: listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("gateway: server error", "err", err)
			os.Exit(1)
		}
	}()

	select {
	case <-sigCh:
		logger.Info("gateway: SIGTERM received — draining")
	case err := <-poolErrCh:
		if err != nil {
			return fmt.Errorf("sender pool: %w", err)
		}
	}

	// Stop sender pool first — give in-flight workers time to finish.
	shutdownDrain := time.Duration(cfg.GracefulShutdownSec) * time.Second
	poolCancel()

	// Shut down HTTP server.
	shutCtx, cancel := context.WithTimeout(context.Background(), shutdownDrain)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	// Wait for pool goroutine to exit.
	if err := <-poolErrCh; err != nil {
		logger.Warn("sender pool exit error", "err", err)
	}

	logger.Info("gateway: shutdown complete")
	return nil
}
