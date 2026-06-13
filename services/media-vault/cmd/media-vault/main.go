// Command media-vault is the mio attachment-persistence sidecar.
//
// It subscribes to MESSAGES_INBOUND, downloads each attachment's bytes within
// the platform-side TTL, persists them to object storage (GCS by default,
// pluggable via MIO_STORAGE_BACKEND), and republishes a Message enriched with
// a stable storage_key + signed URL to MESSAGES_INBOUND_ENRICHED.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/crashchat-ai/mio/services/media-vault/internal/config"
	"github.com/crashchat-ai/mio/services/media-vault/internal/fetcher/slack"
	"github.com/crashchat-ai/mio/services/media-vault/internal/fetcher/zohocliq"
	"github.com/crashchat-ai/mio/services/media-vault/internal/publisher"
	"github.com/crashchat-ai/mio/services/media-vault/internal/storage"
	_ "github.com/crashchat-ai/mio/services/media-vault/internal/storage/gcs" // register gcs backend
	_ "github.com/crashchat-ai/mio/services/media-vault/internal/storage/s3"  // register s3 backend
	"github.com/crashchat-ai/mio/services/media-vault/internal/worker"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("media-vault: fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}

	log := newLogger(cfg.LogLevel)
	slog.SetDefault(log)
	log.Info("media-vault: starting",
		"version", version,
		"backend", cfg.StorageBackend,
		"bucket", cfg.StorageBucket,
		"durable", cfg.DurableName)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	store, err := storage.New(ctx)
	if err != nil {
		return fmt.Errorf("init storage: %w", err)
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		defer func() { _ = closer.Close() }()
	}

	nc, err := nats.Connect(cfg.NATSURLs, nats.Name("mio-media-vault"))
	if err != nil {
		return fmt.Errorf("nats connect: %w", err)
	}
	defer func() { _ = nc.Drain() }()

	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("jetstream new: %w", err)
	}

	// Metrics endpoint.
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	metricsSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.MetricsPort),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Info("metrics: listening", "port", cfg.MetricsPort)
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("metrics: server error", "err", err)
		}
	}()
	defer func() {
		shCtx, sh := context.WithTimeout(context.Background(), 5*time.Second)
		defer sh()
		_ = metricsSrv.Shutdown(shCtx)
	}()

	// Register channel fetchers.
	zohocliq.MustRegister(cfg.CliqClientID, cfg.CliqClientSecret, cfg.CliqRefreshToken, cfg.DownloadMaxBytes, cfg.DownloadTimeout)
	slack.MustRegister(cfg.SlackBotToken, cfg.DownloadMaxBytes, cfg.DownloadTimeout)

	// Provision MESSAGES_INBOUND_ENRICHED on first boot (idempotent).
	if err := publisher.EnsureStream(ctx, js, 1); err != nil {
		return fmt.Errorf("ensure enriched stream: %w", err)
	}
	pub := publisher.New(js)

	processor := &worker.EnrichingProcessor{
		Storage:       store,
		Publisher:     pub,
		StorageBucket: cfg.StorageBucket,
		StoragePrefix: cfg.StoragePrefix,
		Log:           log,
	}

	w := worker.New(nc, js, worker.Config{
		Stream:        "MESSAGES_INBOUND",
		Durable:       cfg.DurableName,
		FilterSubject: "mio.inbound.>",
		AckWait:       cfg.DownloadTimeout + 30*time.Second,
		Logger:        log,
	}, processor)

	if err := w.Run(ctx); err != nil {
		return fmt.Errorf("worker: %w", err)
	}
	log.Info("media-vault: shut down cleanly")
	return nil
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}
