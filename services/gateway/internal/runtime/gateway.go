// Package runtime exposes the gateway's bootstrap loop as a callable
// function. Both cmd/gateway (production binary) and cmd/all-in-one
// (embedded-NATS variant) call RunGateway with their own logger and
// pre-prepared config.
//
// The function blocks until SIGTERM or a fatal error. Shutdown drains
// the sender pool and HTTP server within cfg.GracefulShutdownSec.
//
// Channel adapter registration is the caller's responsibility — each
// binary blank-imports the relevant channel packages so init() runs
// before RunGateway sees the registry. Don't add blank imports here;
// they belong with main packages, not runtime code.
package runtime

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

	"github.com/crashchat-ai/mio/pkg/channels"
	"github.com/crashchat-ai/mio/services/gateway/internal/config"
	"github.com/crashchat-ai/mio/services/gateway/internal/ratelimit"
	"github.com/crashchat-ai/mio/services/gateway/internal/sender"
	"github.com/crashchat-ai/mio/services/gateway/internal/server"
	"github.com/crashchat-ai/mio/services/gateway/store"
	sdk "github.com/crashchat-ai/mio/sdk-go"
)

// RunGateway runs the gateway HTTP server + sender pool until SIGTERM.
// version is embedded in NATS client names + logs for observability.
func RunGateway(logger *slog.Logger, version string) error {
	logger.Info("gateway starting", "version", version)

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	ctx := context.Background()
	pg, err := store.NewPool(ctx, cfg.PostgresDSN, int32(cfg.PgxMaxConns))
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	defer pg.Close()
	logger.Info("postgres: pool ready", "max_conns", cfg.PgxMaxConns)

	if cfg.MigrateOnStart {
		logger.Info("postgres: running migrations")
		if err := store.MigrateUp(cfg.PostgresDSN); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		logger.Info("postgres: migrations complete")
	}

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

	sdkReg := prometheus.NewRegistry()
	sdkClient, err := sdk.New(natsURL,
		sdk.WithName("mio-gateway/sdk/"+version),
		sdk.WithMetricsRegistry(sdkReg),
		sdk.WithMaxAckPending(32),
		sdk.WithAckWait(30*time.Second),
	)
	if err != nil {
		return fmt.Errorf("sdk: %w", err)
	}
	defer sdkClient.Close()

	dispatcher := sender.New(channels.RegisteredAdapters())
	logger.Info("sender: dispatcher built",
		"adapters", len(channels.RegisteredAdapters()))

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

	shutdownDrain := time.Duration(cfg.GracefulShutdownSec) * time.Second
	poolCancel()

	shutCtx, cancel := context.WithTimeout(context.Background(), shutdownDrain)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	if err := <-poolErrCh; err != nil {
		logger.Warn("sender pool exit error", "err", err)
	}

	logger.Info("gateway: shutdown complete")
	return nil
}
