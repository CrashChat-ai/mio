// Package discord implements the Discord channel adapter: gateway-WS inbound
// normalization (fed by services/gateway/internal/discordrunner) and the REST
// outbound sender. The channels.Adapter interface is satisfied by Adapter;
// init.go self-registers it so gateway binaries only need a blank import.
package discord

import (
	"os"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

const discordMaxDeliver = 5

// Adapter implements channels.Adapter for Discord. Constructed in init.go from
// the DISCORD_BOT_TOKEN env, mirroring slack's env-backed NewAdapter. The DB
// credentials table is the v2 multi-account path.
type Adapter struct {
	botToken string
}

// NewAdapter reads the bot token from env. Absent → an outbound-disabled
// adapter that still registers (keeps test imports working without env
// wiring); Send/Edit then return an explicit error. The gateway-WS runner
// reads the same token.
func NewAdapter() *Adapter {
	return &Adapter{botToken: os.Getenv("DISCORD_BOT_TOKEN")}
}

// ChannelType returns the registry slug.
func (a *Adapter) ChannelType() string { return channelType }

// MaxDeliver returns the max redelivery count for Discord messages.
func (a *Adapter) MaxDeliver() int { return discordMaxDeliver }

// RateLimitKey returns the per-conversation bucket key (Discord message-create
// limits are per-channel): "{account_id}:{conversation_external_id}".
func (a *Adapter) RateLimitKey(cmd *miov1.SendCommand) string {
	conv := cmd.GetConversationExternalId()
	if conv == "" {
		return ""
	}
	return cmd.GetAccountId() + ":" + conv
}
