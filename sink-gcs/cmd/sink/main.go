// Command sink is the mio-sink-gcs archival consumer.
//
// It pulls from the MESSAGES_INBOUND JetStream stream via the "gcs-archiver"
// durable consumer, encodes messages as NDJSON, and writes them to GCS (or
// MinIO for local dev) partitioned as:
//
//	gs://mio-messages/channel_type=<slug>/date=YYYY-MM-DD/<consumer-id>-<seqStart>-<seqEnd>.ndjson
//
// Flush triggers (whichever fires first):
//   - Buffer ≥ 16 MB
//   - Writer age ≥ 1 min
//   - SIGTERM → flush all writers, ack, exit
//
// Ack is deferred until the final object exists (copy-then-delete complete).
// This guarantees at-least-once delivery with idempotent overwrites on restart.
//
// Environment variables:
//
//	NATS_URL          — NATS server URL (default: nats://localhost:4222)
//	SINK_BACKEND      — "gcs" or "minio" (default: minio)
//	SINK_BUCKET       — GCS/MinIO bucket name (default: mio-messages)
//	SINK_PREFIX       — optional path prepended to object keys; e.g. "mio/" so
//	                    objects land at gs://<bucket>/mio/channel_type=…/…
//	                    Empty (default) writes at bucket root.
//	SINK_ENDPOINT     — MinIO endpoint (default: http://localhost:9000)
//	SINK_ACCESS_KEY   — MinIO access key (default: minioadmin)
//	SINK_SECRET_KEY   — MinIO secret key (default: minioadmin)
//	GOOGLE_APPLICATION_CREDENTIALS — GCS service-account JSON path (empty → ADC)
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/crashchat-ai/mio/sink-gcs/internal/archiver"
	"github.com/crashchat-ai/mio/sink-gcs/internal/writer"
)

const (
	streamName    = "MESSAGES_INBOUND"
	durableName   = "gcs-archiver"
	defaultNATS   = "nats://localhost:4222"
	defaultBucket = "mio-messages"
	healthAddr    = ":8080"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	natsURL := envOr("NATS_URL", defaultNATS)

	// Writer config from environment.
	if os.Getenv("SINK_BUCKET") == "" {
		_ = os.Setenv("SINK_BUCKET", defaultBucket)
	}
	if os.Getenv("SINK_ENDPOINT") == "" {
		_ = os.Setenv("SINK_ENDPOINT", "http://localhost:9000")
	}

	writerCfg, err := writer.ConfigFromEnv()
	if err != nil {
		log.Error("invalid writer config", "err", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	// Health endpoint — matches the Helm chart's liveness/readiness probe
	// at :8080/healthz. Returns 200 once the archiver loop is running below
	// (indirect signal: if this goroutine is alive, the process is up; the
	// archiver's own NATS-handle errors propagate up via Run() and crash the
	// process, which is what we want kubernetes to see).
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	healthSrv := &http.Server{Addr: healthAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := healthSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("health: ListenAndServe", "err", err)
		}
	}()
	defer func() {
		shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = healthSrv.Shutdown(shutdownCtx)
	}()

	// Ensure bucket exists for MinIO (local dev).
	if writerCfg.Backend == writer.BackendMinIO {
		if err := writer.EnsureBucket(ctx, writerCfg); err != nil {
			log.Error("minio: ensure bucket", "bucket", writerCfg.Bucket, "err", err)
			os.Exit(1)
		}
		log.Info("minio: bucket ready", "bucket", writerCfg.Bucket)
	}

	arc, err := archiver.New(archiver.Config{
		NatsURL:     natsURL,
		Stream:      streamName,
		Durable:     durableName,
		WriterCfg:   writerCfg,
		FlushSize:   16 * 1024 * 1024, // 16 MB
		FlushAge:    time.Minute,
		MaxInflight: 64,
		Logger:      log,
	})
	if err != nil {
		log.Error("archiver: init", "err", err)
		os.Exit(1)
	}

	log.Info("sink-gcs: starting", "stream", streamName, "durable", durableName,
		"backend", writerCfg.Backend, "bucket", writerCfg.Bucket)

	if err := arc.Run(ctx); err != nil {
		log.Error("archiver: run", "err", err)
		os.Exit(1)
	}
	log.Info("sink-gcs: shutdown complete")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
