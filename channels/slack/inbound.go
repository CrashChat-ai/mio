package slack

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// handshakeBodyCap mirrors the gateway's inbound LimitReader so the dormant
// handshake probe cannot read an unbounded body.
const handshakeBodyCap = 1 << 20

// slackInbound satisfies channels.InboundAdapter. Normalize is the shared seam
// the Socket Mode runner (P2) feeds payload bytes to; VerifySignature and
// HandleHandshake are dormant in v1 (Socket Mode) but fully specified for the
// additive-v2 webhook path. secret is the v0 signing secret; nil = the
// introspection instance from Adapter.Inbound(), empty = deliberate dev mode.
type slackInbound struct {
	secret []byte
}

// ErrSecretNotConfigured is returned by VerifySignature on the introspection
// instance (nil secret) so discovery callers never silently dev-bypass.
var ErrSecretNotConfigured = errors.New("slack: slackInbound: secret not configured (use WithSecret)")

// ErrBadSignature is returned when the X-Slack-Signature header is absent,
// stale, or does not match.
var ErrBadSignature = errors.New("slack: invalid request signature")

// WithSecret satisfies channels.SecretConfigurable for v2 webhook mounting.
func (s *slackInbound) WithSecret(secret []byte) channels.InboundAdapter {
	return &slackInbound{secret: secret}
}

// WebhookSecretNames declares the file-mount name for v2 webhook signing.
func (s *slackInbound) WebhookSecretNames() []string {
	return []string{channels.DefaultWebhookSecretName(channelType)}
}

// WorkspaceKey returns the Slack team id for multi-workspace account routing.
func (s *slackInbound) WorkspaceKey(msg *miov1.Message) string {
	return msg.GetAttributes()[attrSlackTeamID]
}

// VerifySignature validates the v0 request signature (additive-v2 webhook).
func (s *slackInbound) VerifySignature(headers http.Header, rawBody []byte) error {
	if s.secret == nil {
		return ErrSecretNotConfigured
	}
	sig := headers.Get("X-Slack-Signature")
	ts := headers.Get("X-Slack-Request-Timestamp")
	if verifySignature(s.secret, rawBody, sig, ts, time.Now()) {
		return nil
	}
	return ErrBadSignature
}

// Normalize parses an event_callback payload into a canonical mio.v1.Message.
// Soft failures wrap channels.ErrNormalizeSoft so the caller acks without retry.
func (s *slackInbound) Normalize(rawBody []byte) (*miov1.Message, error) {
	env, err := ParseEnvelope(rawBody)
	if err != nil {
		return nil, fmt.Errorf("slack: parse: %w", err)
	}
	return Normalize(env)
}

// HandleHandshake echoes Slack's url_verification challenge (additive-v2
// webhook). Returns false for normal event_callback payloads so the pipeline
// proceeds to VerifySignature + Normalize.
func (s *slackInbound) HandleHandshake(w http.ResponseWriter, r *http.Request) bool {
	var probe struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
	}
	body, err := readHandshakeBody(r)
	if err != nil {
		return false
	}
	if json.Unmarshal(body, &probe) != nil || probe.Type != "url_verification" {
		return false
	}
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(probe.Challenge))
	return true
}

// readHandshakeBody buffers r.Body and restores it so the pipeline can re-read
// when this is not a handshake.
func readHandshakeBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, handshakeBodyCap))
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}
