package discordrunner

import (
	"context"
	"os"

	"github.com/crashchat-ai/mio/pkg/channels"
	"github.com/crashchat-ai/mio/services/gateway/internal/server"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"log/slog"
)

const channelType = "discord"

// StartFromEnv launches the Discord gateway-WS runner when DISCORD_BOT_TOKEN
// is set; otherwise it is a no-op and returns nil so the gateway stays
// unaffected. The runtime calls this behind the env gate and in a goroutine —
// it blocks until ctx is cancelled. Keeping the registry lookup and channel
// slug here keeps gateway/runtime channel-blind (dispatch-lint clean).
func StartFromEnv(
	ctx context.Context,
	pg *pgxpool.Pool,
	pub server.InboundPublisher,
	accounts server.AccountResolver,
	tenantID, accountID string,
	reg prometheus.Registerer,
	logger *slog.Logger,
) error {
	botToken := os.Getenv("DISCORD_BOT_TOKEN")
	if botToken == "" {
		return nil
	}

	inbound := lookupInbound()
	if inbound == nil {
		logger.Warn("discordrunner: token set but adapter not registered; runner not started")
		return nil
	}

	logger.Info("discordrunner: starting Discord gateway-WS ingest")
	return Start(ctx, Deps{
		ChannelType: channelType,
		BotToken:    botToken,
		Inbound:     inbound,
		PG:          pg,
		Pub:         pub,
		Accounts:    accounts,
		TenantID:    tenantID,
		AccountID:   accountID,
		Registerer:  reg,
		Logger:      logger,
	})
}

func lookupInbound() channels.InboundAdapter {
	for _, a := range channels.RegisteredAdapters() {
		if a.ChannelType() == channelType {
			return a.Inbound()
		}
	}
	return nil
}
