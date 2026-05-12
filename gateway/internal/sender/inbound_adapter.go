package sender

import (
	"net/http"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// InboundAdapter encapsulates the per-channel inbound webhook concerns:
// signature verification, payload normalization to mio.v1.Message, and
// optional handshake / URL-verification handling (Slack-style channels).
//
// Adapters return their InboundAdapter via Adapter.Inbound(). The HTTP
// layer (gateway/internal/server) wires the webhook route once and
// delegates to these methods — keeping dispatch.go free of any
// channel-specific branching (P9 litmus).
//
// Concrete implementations should be safe for concurrent use; the HTTP
// handler invokes these methods from multiple goroutines.
type InboundAdapter interface {
	// VerifySignature authenticates the request against the configured
	// shared secret. Returns nil on success; a non-nil error indicates
	// the caller should respond 401 and emit a `bad_signature` metric.
	// For dev / unsigned modes, implementations may return nil
	// unconditionally — the adapter is responsible for emitting a
	// "SECRET UNSET" warning at startup, not per-request.
	VerifySignature(headers http.Header, rawBody []byte) error

	// Normalize parses a buffered request body and produces the canonical
	// mio.v1.Message envelope. The returned Message is not yet persisted
	// or published; the caller (handler.go) handles idempotency and
	// publish.
	//
	// Returns a non-nil error only on unrecoverable parse failures.
	// Soft failures (unknown operation, missing fields) should be
	// reported via a typed sentinel so the handler can respond 200 to
	// the platform without retry.
	Normalize(rawBody []byte) (*miov1.Message, error)

	// HandleHandshake intercepts platform-specific URL verification or
	// connect handshakes (e.g. Slack's `url_verification`, Telegram's
	// setWebhook ping). Returns true when the request was consumed —
	// the caller MUST NOT continue with VerifySignature / Normalize on
	// the same request. Returns false for normal message webhooks.
	//
	// Adapters that do not have a handshake concept return false
	// unconditionally and never write to w.
	HandleHandshake(w http.ResponseWriter, r *http.Request) bool
}
