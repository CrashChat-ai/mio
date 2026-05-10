// Package sender defines the Adapter interface and its supporting types.
// Each channel adapter implements Adapter and self-registers via init().
// dispatch.go builds a lookup table from RegisteredAdapters(); no adapter-
// specific branches ever appear in dispatch.go (P9 litmus: zero-edit).
package sender

import (
	"context"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// Adapter is the minimal interface every channel adapter must satisfy.
// Deliberately minimal: only Send, Edit, ChannelType, MaxDeliver, RateLimitKey.
// Delete/React/Typing are deferred until two channels need them (YAGNI).
type Adapter interface {
	// Send delivers a new outbound message and returns the platform's external
	// message id so the pool can store it in outbound_state for later edits.
	// Idempotency across re-deliveries is the pool's responsibility via
	// outbound_state + NATS dedup, not the adapter's.
	Send(ctx context.Context, cmd *miov1.SendCommand) (externalID string, err error)

	// Edit updates an existing platform message in-place.
	// cmd.EditOfExternalId carries the platform id (resolved by the pool
	// from outbound_state before calling Edit).
	Edit(ctx context.Context, cmd *miov1.SendCommand) error

	// ChannelType returns the registry slug this adapter handles, e.g. "zoho_cliq".
	// Must match proto/channels.yaml entry exactly (underscore, lowercase).
	ChannelType() string

	// MaxDeliver overrides the consumer's max_deliver for this channel.
	// Cliq returns 5 (default); flaky channels return higher values.
	MaxDeliver() int

	// RateLimitKey returns the bucket key for this command.
	// Empty string means "use account_id default".
	// Slack-style adapters return "account_id:conversation_external_id" for
	// per-conversation fairness — no wire-format change required.
	RateLimitKey(cmd *miov1.SendCommand) string
}
