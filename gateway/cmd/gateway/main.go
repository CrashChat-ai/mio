// Command gateway is the mio-gateway HTTP server.
// It receives channel webhooks, normalises payloads to mio.v1.Message,
// publishes to MESSAGES_INBOUND, and drains MESSAGES_OUTBOUND via the
// sender pool which dispatches to per-channel adapters.
package main

import (
	"log/slog"
	"os"

	"github.com/crashchat-ai/mio/gateway/internal/runtime"

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

	if err := runtime.RunGateway(logger, version); err != nil {
		logger.Error("gateway: fatal", "err", err)
		os.Exit(1)
	}
}
