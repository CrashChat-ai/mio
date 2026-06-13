package socketrunner

import (
	"context"
	"os"

	"github.com/crashchat-ai/mio/pkg/channels"
	"github.com/crashchat-ai/mio/services/gateway/internal/server"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"log/slog"
)

const channelType = "slack"

// StartFromEnv launches the Slack Socket Mode runner when both SLACK_BOT_TOKEN
// and SLACK_APP_TOKEN are set; otherwise it is a no-op and returns nil so the
// gateway stays unaffected. The runtime calls this behind the env gate and in a
// goroutine — it blocks until ctx is cancelled. Keeping the registry lookup and
// channel slug here keeps gateway/runtime channel-blind (dispatch-lint clean).
func StartFromEnv(
	ctx context.Context,
	pg *pgxpool.Pool,
	pub server.InboundPublisher,
	accounts server.AccountResolver,
	tenantID, accountID string,
	reg prometheus.Registerer,
	logger *slog.Logger,
) error {
	botToken := os.Getenv("SLACK_BOT_TOKEN")
	appToken := os.Getenv("SLACK_APP_TOKEN")
	if botToken == "" || appToken == "" {
		return nil
	}

	inbound := lookupInbound()
	if inbound == nil {
		logger.Warn("socketrunner: tokens set but adapter not registered; runner not started")
		return nil
	}

	logger.Info("socketrunner: starting Slack Socket Mode ingest")
	return Start(ctx, Deps{
		ChannelType: channelType,
		BotToken:    botToken,
		AppToken:    appToken,
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
