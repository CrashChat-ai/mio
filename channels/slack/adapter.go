// Package slack implements the inbound Slack adapter and (from P3) the outbound
// sender. The channels.Adapter interface is satisfied by Adapter; init.go
// self-registers it so gateway binaries only need a blank import.
package slack

import (
	"context"
	"fmt"
	"os"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

const slackMaxDeliver = 5

// Adapter implements channels.Adapter for Slack. Constructed in init.go from
// env tokens (SLACK_BOT_TOKEN xoxb / SLACK_APP_TOKEN xapp), mirroring zohocliq's
// env-backed NewAdapter. The DB credentials table is the v2 multi-account path.
type Adapter struct {
	botToken string
	appToken string
}

// NewAdapter reads bot/app tokens from env. Both absent → an outbound-disabled
// adapter that still registers (keeps test imports working without env wiring);
// Send/Edit then return an explicit error. The Socket Mode runner (P2) reads
// the app token; Send/Edit (P3) read the bot token.
func NewAdapter() *Adapter {
	return &Adapter{
		botToken: os.Getenv("SLACK_BOT_TOKEN"),
		appToken: os.Getenv("SLACK_APP_TOKEN"),
	}
}

// ChannelType returns the registry slug.
func (a *Adapter) ChannelType() string { return channelType }

// MaxDeliver returns the max redelivery count for Slack messages.
func (a *Adapter) MaxDeliver() int { return slackMaxDeliver }

// RateLimitKey returns the per-conversation bucket key (chat.postMessage limits
// are per-channel): "{account_id}:{conversation_external_id}".
func (a *Adapter) RateLimitKey(cmd *miov1.SendCommand) string {
	conv := cmd.GetConversationExternalId()
	if conv == "" {
		return ""
	}
	return cmd.GetAccountId() + ":" + conv
}

// Send is implemented in P3 (sender.go). The P1 stub keeps the adapter
// registrable and the interface satisfied.
func (a *Adapter) Send(context.Context, *miov1.SendCommand) (string, error) {
	return "", fmt.Errorf("slack: Send not implemented (P3)")
}

// Edit is implemented in P3 (sender_edit.go).
func (a *Adapter) Edit(context.Context, *miov1.SendCommand) error {
	return fmt.Errorf("slack: Edit not implemented (P3)")
}
