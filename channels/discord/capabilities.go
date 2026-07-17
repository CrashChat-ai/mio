package discord

import (
	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"google.golang.org/protobuf/proto"
)

// discordCapabilities is the hard-coded ChannelCapabilities advertised by the
// Discord adapter. A regression test catches drift.
//
//   - supports_delete FALSE — Adapter contract has no Delete (YAGNI); inbound
//     deletes are still captured as KIND_DELETE relations.
//   - supports_threads TRUE via reply references (message_reference); real
//     thread channels need channel-object lookups — deferred.
//   - max_text_bytes 2000 — Discord's hard cap for bot messages; forces
//     producer-side chunking since Send returns one external id.
//   - rate_limit_per_second 1 / scope "conversation" — the per-channel
//     message-create bucket is 5 req/5s; 1/s is the safe steady rate.
//   - auth_kind "bot_token" — developer-portal bot token paste; no OAuth
//     dance in v1.
var discordCapabilities = &miov1.ChannelCapabilities{
	SupportsEdit:      true,
	SupportsDelete:    false,
	SupportsReactions: true,
	SupportsThreads:   true,
	SupportsTyping:    false,
	SupportsPresence:  false,
	AllowedAttachments: []miov1.Attachment_Kind{
		miov1.Attachment_KIND_IMAGE,
		miov1.Attachment_KIND_FILE,
		miov1.Attachment_KIND_AUDIO,
		miov1.Attachment_KIND_VIDEO,
		miov1.Attachment_KIND_LINK,
	},
	MaxTextBytes:        2000,
	RateLimitPerSecond:  1,
	RateLimitScope:      "conversation",
	AuthKind:            "bot_token",
	EditWindowSeconds:   0,
	DeleteWindowSeconds: 0,
}

// Capabilities returns a defensive copy of the Discord adapter's capabilities.
func (a *Adapter) Capabilities() *miov1.ChannelCapabilities {
	return proto.Clone(discordCapabilities).(*miov1.ChannelCapabilities)
}

// Inbound returns the InboundAdapter for gateway dispatch envelopes (WS
// runner in v1, interactions webhook additive v2). The returned instance has
// a nil secret; v2 webhook mode injects one via WithSecret.
func (a *Adapter) Inbound() channels.InboundAdapter {
	return &discordInbound{}
}

// Credentials returns the bot-token CredentialAdapter (no OAuth dance in v1).
func (a *Adapter) Credentials() channels.CredentialAdapter {
	return &botTokenCredentials{}
}
