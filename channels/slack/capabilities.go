package slack

import (
	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"google.golang.org/protobuf/proto"
)

// slackCapabilities is the hard-coded ChannelCapabilities advertised by the
// Slack adapter (brief §6). A regression test catches drift.
//
//   - supports_delete FALSE — Adapter contract has no Delete (YAGNI); inbound
//     deletes are still captured as KIND_DELETE relations.
//   - max_text_bytes 4000 — display-safe bound (API hard cap 40k); forces
//     producer-side chunking since Send returns one external id.
//   - rate_limit_per_second 1 / scope "conversation" — chat.postMessage tier.
//   - auth_kind "bot_token" — xoxb paste; no OAuth dance in v1.
var slackCapabilities = &miov1.ChannelCapabilities{
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
	MaxTextBytes:        4000,
	RateLimitPerSecond:  1,
	RateLimitScope:      "conversation",
	AuthKind:            "bot_token",
	EditWindowSeconds:   0,
	DeleteWindowSeconds: 0,
}

// Capabilities returns a defensive copy of the Slack adapter's capabilities.
func (a *Adapter) Capabilities() *miov1.ChannelCapabilities {
	return proto.Clone(slackCapabilities).(*miov1.ChannelCapabilities)
}

// Inbound returns the InboundAdapter for Slack event_callback payloads (Socket
// Mode in v1, webhook additive v2). The returned instance has a nil secret;
// v2 webhook mode injects one via WithSecret.
func (a *Adapter) Inbound() channels.InboundAdapter {
	return &slackInbound{}
}

// Credentials returns the bot-token CredentialAdapter (no OAuth dance in v1).
func (a *Adapter) Credentials() channels.CredentialAdapter {
	return &botTokenCredentials{}
}
