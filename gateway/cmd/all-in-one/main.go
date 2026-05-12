// Command all-in-one runs an embedded NATS JetStream server plus the
// gateway in a single process. Intended for laptop / single-node demos.
// PRODUCTION DEPLOYS should keep using cmd/gateway + an external NATS
// cluster — embedded JetStream is single-node and has no replication.
//
// Flags:
//   --storage   "memory" | "file"     (default: memory)
//   --store-dir <path>                (default: ./var/jetstream; ignored for memory)
//   --nats-port <int>                 (default: 4222)
//
// Guard rail: MIO_ENV=prod + --storage=memory refuses to start.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/crashchat-ai/mio/gateway/internal/nats"
	"github.com/crashchat-ai/mio/gateway/internal/runtime"

	// Blank-import each channel package so init() registers the adapter.
	_ "github.com/crashchat-ai/mio/gateway/internal/channels/zohocliq"
)

var version = "dev"

func main() {
	storage := flag.String("storage", "memory", "JetStream storage: memory|file")
	storeDir := flag.String("store-dir", "./var/jetstream", "file-mode JetStream directory")
	port := flag.Int("nats-port", 4222, "embedded NATS port")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	env := os.Getenv("MIO_ENV")
	if env == "" {
		env = "dev"
	}
	if env == "prod" && *storage == "memory" {
		logger.Error("all-in-one: memory storage forbidden in prod — use --storage file or external NATS")
		os.Exit(2)
	}
	if env == "prod" && *storage == "file" {
		logger.Warn("running all-in-one in prod — single-node durability only; use external NATS for multi-replica deploys")
	}

	ns, url, err := nats.StartEmbedded(nats.EmbeddedOpts{
		Storage:  *storage,
		StoreDir: *storeDir,
		Host:     "127.0.0.1",
		Port:     *port,
	})
	if err != nil {
		logger.Error("all-in-one: embedded NATS failed", "err", err)
		os.Exit(1)
	}
	defer ns.Shutdown()
	logger.Info("all-in-one: embedded NATS ready", "url", url, "storage", *storage)

	// Force the gateway runtime to use the embedded URL even if MIO_NATS_URLS
	// is set externally. Set BEFORE config.Load() runs inside RunGateway.
	if err := os.Setenv("MIO_NATS_URLS", url); err != nil {
		logger.Error("all-in-one: setenv MIO_NATS_URLS", "err", err)
		os.Exit(1)
	}

	if err := runtime.RunGateway(logger, fmt.Sprintf("all-in-one/%s", version)); err != nil {
		logger.Error("all-in-one: gateway run", "err", err)
		os.Exit(1)
	}
}
