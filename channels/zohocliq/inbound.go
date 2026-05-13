package zohocliq

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// cliqInbound satisfies channels.InboundAdapter for Zoho Cliq webhook
// requests. It is a thin façade over the existing free functions
// VerifySignature, ParseWebhookPayload, and Normalize — no behaviour
// change vs the pre-Phase-2 handler.
//
// Cliq has no URL-verification or connect handshake (Zoho activates the
// webhook via the developer console, not via a runtime handshake); the
// HandleHandshake method returns false unconditionally so the HTTP
// handler proceeds to signature verification + normalization.
//
// The secret field is the HMAC-SHA256 shared secret. When empty, the
// adapter is in dev-mode and VerifySignature returns nil for all
// requests; callers should emit a startup warning (see handler.go).
type cliqInbound struct {
	secret []byte
}

// ErrBadSignature is returned by VerifySignature when the X-Webhook-Signature
// header is missing or does not match. Callers compare via errors.Is.
var ErrBadSignature = errors.New("zohocliq: invalid webhook signature")

// ErrSecretNotConfigured is returned by VerifySignature on a cliqInbound
// instance with no shared secret. The live webhook handler does NOT use
// this code path (it builds its own cliqInbound with deps.Secret); the
// instance returned by Adapter.Inbound() has nil secret because the
// adapter does not own the secret. Callers (e.g. P4 admin API webhook
// probing) must inject the secret via NewInbound([]byte) before calling.
var ErrSecretNotConfigured = errors.New("zohocliq: cliqInbound: secret not configured (use NewInbound)")

// NewInbound returns a cliqInbound configured with a shared secret.
// Exposed for callers that need to verify signatures via Adapter.Inbound()
// at request time (e.g. P4 admin server diagnostic endpoints).
func NewInbound(secret []byte) channels.InboundAdapter { return &cliqInbound{secret: secret} }

// VerifySignature validates the X-Webhook-Signature header against the body
// using HMAC-SHA256 (hex or base64).
//
// Behaviour matrix:
//   - secret == nil  → returns ErrSecretNotConfigured (Adapter.Inbound() path).
//     Discovery callers never get a silent dev-mode bypass.
//   - secret == ""   → dev-mode bypass (returns nil for all). The live
//     handler's HandlerDeps.Secret is []byte("") only when
//     config explicitly says so; that path still emits a
//     startup warning.
//   - secret != ""   → strict HMAC verify; signature mismatch → ErrBadSignature.
//
// Note: the "secret was deliberately empty" dev mode is differentiated from
// "adapter never gave us a secret" via the nil-vs-empty-slice distinction.
func (c *cliqInbound) VerifySignature(headers http.Header, rawBody []byte) error {
	if c.secret == nil {
		return ErrSecretNotConfigured
	}
	sigHeader := headers.Get("X-Webhook-Signature")
	if VerifySignature(c.secret, rawBody, sigHeader) {
		return nil
	}
	return ErrBadSignature
}

// Normalize parses a Cliq webhook body and produces the canonical
// mio.v1.Message envelope. The returned Message has Id / TenantId /
// AccountId left empty — the HTTP handler populates these from
// HandlerDeps and the freshly-allocated UUID before publishing.
//
// Soft-failures (unknown operation, missing fields) surface as errors;
// the handler currently maps these to 200 OK so Cliq does not retry.
func (c *cliqInbound) Normalize(rawBody []byte) (*miov1.Message, error) {
	payload, err := ParseWebhookPayload(rawBody)
	if err != nil {
		return nil, fmt.Errorf("zohocliq: parse: %w", err)
	}
	nm, err := Normalize(payload)
	if err != nil {
		return nil, fmt.Errorf("zohocliq: normalize: %w", err)
	}

	msg := &miov1.Message{
		SchemaVersion:          1,
		ChannelType:            channelType,
		ConversationExternalId: nm.ConversationExternalID,
		ConversationKind:       kindStringToEnum(nm.ConversationKind),
		SourceMessageId:        nm.SourceMessageID,
		Sender: &miov1.Sender{
			ExternalId:  nm.SenderExternalID,
			DisplayName: nm.SenderDisplayName,
			IsBot:       nm.SenderIsBot,
		},
		Text:       nm.Text,
		ReceivedAt: timestamppb.Now(),
		Attributes: nm.Attributes,
	}
	for _, a := range nm.Attachments {
		msg.Attachments = append(msg.Attachments, &miov1.Attachment{
			Kind:     attachmentKindFromMime(a.MIME),
			Url:      a.URL,
			Mime:     a.MIME,
			Filename: a.Filename,
		})
	}
	if nm.ParentExternalID != "" {
		msg.ParentConversationId = nm.ParentExternalID
	}
	return msg, nil
}

// HandleHandshake is a no-op for Cliq: there is no URL-verification or
// connect handshake at runtime. Always returns false (HTTP handler
// proceeds with VerifySignature + Normalize).
func (c *cliqInbound) HandleHandshake(w http.ResponseWriter, r *http.Request) bool {
	return false
}
