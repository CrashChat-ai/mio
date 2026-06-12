package channels

import (
	"errors"
	"strings"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// AttrConversationDisplayName is the attribute key adapters set on a
// normalized Message so the gateway can stamp conversations.display_name
// without channel-specific knowledge. Additive wire attribute; see
// docs/consumer-contract.md.
const AttrConversationDisplayName = "conversation_display_name"

// ErrNormalizeSoft marks Normalize failures the platform must not retry:
// the webhook handler responds 200 and records a metric instead of 4xx/5xx.
// Parse failures (malformed body) stay unwrapped and map to 400.
var ErrNormalizeSoft = errors.New("channels: normalize soft failure")

// SecretConfigurable lets the gateway inject the file-mounted webhook secret
// into an InboundAdapter at mount time. Adapter.Inbound() returns an
// unconfigured instance; the gateway calls WithSecret once per route. An
// empty (non-nil) secret means deliberate dev mode.
type SecretConfigurable interface {
	WithSecret(secret []byte) InboundAdapter
}

// SecretNamer lets an inbound adapter declare which file-mounted secret
// names it accepts, first match wins. Adapters not implementing it get
// DefaultWebhookSecretName. Exists so renamed channels keep reading their
// legacy mount (zoho_cliq → cliq-webhook-secret).
type SecretNamer interface {
	WebhookSecretNames() []string
}

// RouteAliaser lets an inbound adapter declare extra webhook mount paths
// beyond /webhooks/<slug> — locked ingress routes that predate the generic
// router (e.g. Cliq's /cliq).
type RouteAliaser interface {
	RouteAliases() []string
}

// WorkspaceKeyer lets an inbound adapter expose the platform-side workspace
// identity of a normalized message (Cliq org id, Slack team id) so the
// gateway can route one webhook endpoint to the right account when several
// accounts of the same channel_type are installed. Empty = unknown.
type WorkspaceKeyer interface {
	WorkspaceKey(msg *miov1.Message) string
}

// DefaultWebhookSecretName maps a registry slug to its conventional secret
// file name: zoho_cliq → zoho-cliq-webhook-secret.
func DefaultWebhookSecretName(channelType string) string {
	return strings.ReplaceAll(channelType, "_", "-") + "-webhook-secret"
}
