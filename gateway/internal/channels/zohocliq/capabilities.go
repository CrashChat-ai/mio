package zohocliq

import (
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"github.com/crashchat-ai/mio/gateway/internal/sender"
	"google.golang.org/protobuf/proto"
)

// cliqCapabilities is the hard-coded ChannelCapabilities advertised by the
// Cliq adapter. Hard-coding (instead of computing) keeps the contract
// explicit; the regression test in capabilities_test.go catches drift.
//
// Values reflect documented Cliq limits + the adapter's current support:
//   - supports_edit: TRUE — sender_edit.go implements message_edited
//   - supports_delete / supports_typing / supports_presence: FALSE — adapter
//     does not implement these yet; flipping the bool here is the only
//     change a future adapter PR needs.
//   - supports_reactions / supports_threads: TRUE — wire format supports
//     them (Cliq inbound captures reaction + thread payloads); outbound
//     reactions/thread-reply are post-MVP but advertised so consumers can
//     enqueue them.
//   - max_text_bytes: 32_000 — Cliq channel-message hard cap (32 KB), per
//     Cliq REST docs at /api/v2/channelsbyname/{name}/message.
//   - rate_limit_per_second: 10 — bot-endpoint documented limit.
//   - rate_limit_scope: "account" — bot limits apply per workspace.
//   - auth_kind: "oauth2_refresh" — see token.go.
//   - edit_window_seconds / delete_window_seconds: 0 — Cliq imposes no
//     time-bounded edit window (admin/owners can edit any time).
//   - allowed_attachments: image/file/audio/video/link — matches what the
//     inbound normalizer maps and what the outbound channels-by-name
//     endpoint accepts in the `file` field.
var cliqCapabilities = &miov1.ChannelCapabilities{
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
	MaxTextBytes:        32_000,
	RateLimitPerSecond:  10,
	RateLimitScope:      "account",
	AuthKind:            "oauth2_refresh",
	EditWindowSeconds:   0,
	DeleteWindowSeconds: 0,
}

// Capabilities returns a defensive copy of the Cliq adapter's hard-coded
// ChannelCapabilities. Copy avoids callers (e.g. UI libraries that
// defensively zero fields) corrupting the singleton for every subsequent
// ListChannelTypes response. The clone cost is negligible (single allocation
// per admin RPC); the regression-test gate continues to compare via
// proto.Equal so the copy doesn't hide drift.
func (a *Adapter) Capabilities() *miov1.ChannelCapabilities {
	return proto.Clone(cliqCapabilities).(*miov1.ChannelCapabilities)
}

// Inbound returns the InboundAdapter for Cliq webhook requests.
// Built on the existing signature.go + normalize.go primitives — no
// behaviour change vs the pre-Phase-2 handler.
//
// The returned cliqInbound has a nil secret: the live webhook path
// (handler.go) still configures its own cliqInbound via HandlerDeps.Secret
// from the file-mount path. Adapter.Inbound() exists so the P4 admin API
// (ListChannelTypes / install discovery) can introspect the adapter; that
// path does not call VerifySignature. Configurable injection happens in
// P4 when admin actually issues webhook requests on behalf of operators.
func (a *Adapter) Inbound() sender.InboundAdapter {
	return &cliqInbound{}
}

// Credentials returns the CredentialAdapter wrapping the OAuth token
// source. Always non-nil — when OAuth env vars are absent, AuthorizeURL /
// ExchangeCode return typed errors but the wrapper itself is constructed.
func (a *Adapter) Credentials() sender.CredentialAdapter {
	return &tokenCredentials{adapter: a}
}
