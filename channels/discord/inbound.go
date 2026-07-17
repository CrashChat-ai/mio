package discord

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// handshakeBodyCap mirrors the gateway's inbound LimitReader so the dormant
// handshake probe cannot read an unbounded body.
const handshakeBodyCap = 1 << 20

// signatureMaxSkew rejects requests whose X-Signature-Timestamp is more than
// 5 minutes from now (replay protection, mirroring slack's window). Discord
// signs timestamp||body, so a stale-but-valid signature replays verbatim
// without this check.
const signatureMaxSkew = 5 * time.Minute

// discordInbound satisfies channels.InboundAdapter. Normalize is the shared
// seam the gateway-WS runner feeds envelope bytes to; VerifySignature and
// HandleHandshake are dormant in v1 (WS gateway) but fully specified for the
// additive-v2 interactions-webhook path. secret is the app's Ed25519 public
// key in hex; nil = the introspection instance from Adapter.Inbound().
type discordInbound struct {
	secret []byte
}

// ErrSecretNotConfigured is returned by VerifySignature on the introspection
// instance (nil secret) so discovery callers never silently dev-bypass.
var ErrSecretNotConfigured = errors.New("discord: discordInbound: secret not configured (use WithSecret)")

// ErrBadSignature is returned when the Ed25519 signature headers are absent
// or do not verify.
var ErrBadSignature = errors.New("discord: invalid request signature")

// WithSecret satisfies channels.SecretConfigurable for v2 webhook mounting.
func (d *discordInbound) WithSecret(secret []byte) channels.InboundAdapter {
	return &discordInbound{secret: secret}
}

// WebhookSecretNames declares the file-mount name for v2 webhook signing.
func (d *discordInbound) WebhookSecretNames() []string {
	return []string{channels.DefaultWebhookSecretName(channelType)}
}

// WorkspaceKey returns the Discord guild id for multi-guild account routing.
func (d *discordInbound) WorkspaceKey(msg *miov1.Message) string {
	return msg.GetAttributes()[attrDiscordGuildID]
}

// VerifySignature validates the interactions-endpoint Ed25519 signature
// (additive-v2 webhook): sig over timestamp||body against the app public key.
func (d *discordInbound) VerifySignature(headers http.Header, rawBody []byte) error {
	if d.secret == nil {
		return ErrSecretNotConfigured
	}
	pub, err := hex.DecodeString(string(d.secret))
	if err != nil {
		return fmt.Errorf("discord: webhook secret is not hex: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("discord: webhook secret decodes to %d bytes, want ed25519 public key size %d", len(pub), ed25519.PublicKeySize)
	}
	sigHex := headers.Get("X-Signature-Ed25519")
	ts := headers.Get("X-Signature-Timestamp")
	if sigHex == "" || ts == "" {
		return ErrBadSignature
	}
	return verifyEd25519(pub, rawBody, sigHex, ts, time.Now())
}

// verifyEd25519 checks freshness then the signature; split out with an
// injected clock so replay-window tests need no sleeping.
func verifyEd25519(pub ed25519.PublicKey, body []byte, sigHex, tsHeader string, now time.Time) error {
	ts, err := strconv.ParseInt(tsHeader, 10, 64)
	if err != nil {
		return ErrBadSignature
	}
	if delta := now.Sub(time.Unix(ts, 0)); delta > signatureMaxSkew || delta < -signatureMaxSkew {
		return ErrBadSignature
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return ErrBadSignature
	}
	if !ed25519.Verify(pub, append([]byte(tsHeader), body...), sig) {
		return ErrBadSignature
	}
	return nil
}

// Normalize parses a gateway dispatch envelope into a canonical mio.v1.Message.
// Soft failures wrap channels.ErrNormalizeSoft so the caller acks without retry.
func (d *discordInbound) Normalize(rawBody []byte) (*miov1.Message, error) {
	env, err := ParseEnvelope(rawBody)
	if err != nil {
		return nil, fmt.Errorf("discord: parse: %w", err)
	}
	return Normalize(env)
}

// HandleHandshake answers the interactions-endpoint PING (type 1 → type 1
// pong) that Discord sends to verify a webhook URL (additive-v2). Returns
// false for anything else so the pipeline proceeds to VerifySignature +
// Normalize.
func (d *discordInbound) HandleHandshake(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	body, err := readHandshakeBody(r)
	if err != nil {
		return false
	}
	var probe struct {
		Type int `json:"type"`
	}
	if json.Unmarshal(body, &probe) != nil || probe.Type != 1 {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"type":1}`))
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
