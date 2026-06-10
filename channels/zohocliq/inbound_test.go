package zohocliq

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// signedHeaders builds a valid X-Webhook-Signature header for the given
// secret + body, using the hex digest the live handler accepts.
func signedHeaders(t *testing.T, secret, body []byte) http.Header {
	t.Helper()
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	h := http.Header{}
	h.Set("X-Webhook-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	return h
}

// TestCliqInbound_VerifySignature_Matrix covers the four documented modes
// of cliqInbound.VerifySignature in a single table:
//
//	nil-secret   → ErrSecretNotConfigured (Adapter.Inbound() default)
//	empty secret → nil (dev-mode bypass)
//	valid sig    → nil
//	wrong sig    → ErrBadSignature
func TestCliqInbound_VerifySignature_Matrix(t *testing.T) {
	t.Parallel()

	const (
		realSecret = "hunter2"
		body       = `{"operation":"message_sent"}`
	)

	tests := []struct {
		name    string
		secret  []byte
		headers func(t *testing.T) http.Header
		wantErr error // matched via errors.Is; nil = no error expected
	}{
		{
			name:    "nil secret returns ErrSecretNotConfigured",
			secret:  nil,
			headers: func(_ *testing.T) http.Header { return http.Header{} },
			wantErr: ErrSecretNotConfigured,
		},
		{
			name:    "empty secret accepts in dev mode",
			secret:  []byte(""),
			headers: func(_ *testing.T) http.Header { return http.Header{} },
			wantErr: nil,
		},
		{
			name:   "valid signature accepted",
			secret: []byte(realSecret),
			headers: func(t *testing.T) http.Header {
				return signedHeaders(t, []byte(realSecret), []byte(body))
			},
			wantErr: nil,
		},
		{
			name:   "wrong signature returns ErrBadSignature",
			secret: []byte(realSecret),
			headers: func(_ *testing.T) http.Header {
				h := http.Header{}
				h.Set("X-Webhook-Signature", "sha256=deadbeef")
				return h
			},
			wantErr: ErrBadSignature,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inbound := &cliqInbound{secret: tt.secret}
			err := inbound.VerifySignature(tt.headers(t), []byte(body))
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("want %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestCliqInbound_Normalize_ChannelText(t *testing.T) {
	t.Parallel()
	body := loadFixture(t, "2026-05-03T10-38-07-channel-text.json")
	inbound := &cliqInbound{}
	msg, err := inbound.Normalize(body)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if msg.GetChannelType() != channelType {
		t.Errorf("channel_type: got %q want %q", msg.GetChannelType(), channelType)
	}
	if msg.GetSourceMessageId() == "" {
		t.Error("source_message_id empty")
	}
	if msg.GetSender() == nil || msg.GetSender().GetExternalId() == "" {
		t.Error("sender.external_id empty")
	}
	if msg.GetText() != "halo" {
		t.Errorf("text: %q", msg.GetText())
	}
	if msg.GetReceivedAt() == nil {
		t.Error("received_at not stamped")
	}
}

func TestCliqInbound_Normalize_ThreadReplyRelation(t *testing.T) {
	t.Parallel()

	body := loadFixture(t, "2026-05-07T22-06-22-channel-thread-reply.json")
	inbound := &cliqInbound{}
	msg, err := inbound.Normalize(body)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}

	if msg.GetThreadRootMessageId() != "" {
		t.Fatalf("thread_root_message_id = %q, want empty before handler DB resolution", msg.GetThreadRootMessageId())
	}
	relation := msg.GetRelation()
	if relation == nil {
		t.Fatal("relation is nil")
	}
	if relation.GetKind() != miov1.MessageRelation_KIND_REPLY {
		t.Errorf("relation.kind = %s, want KIND_REPLY", relation.GetKind())
	}
	if relation.GetTargetExternalId() == "" {
		t.Error("relation.target_external_id must be set for replied message")
	}
	if relation.GetTargetMessageId() != "" {
		t.Errorf("relation.target_message_id = %q, want empty before handler DB resolution", relation.GetTargetMessageId())
	}
}

func TestCliqInbound_Normalize_ParseError(t *testing.T) {
	t.Parallel()
	inbound := &cliqInbound{}
	if _, err := inbound.Normalize([]byte("not json {")); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestCliqInbound_HandleHandshake_AlwaysFalse(t *testing.T) {
	t.Parallel()
	inbound := &cliqInbound{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/cliq", nil)
	if inbound.HandleHandshake(w, r) {
		t.Fatal("HandleHandshake must return false for Cliq")
	}
	if w.Code != http.StatusOK {
		t.Errorf("unexpected response written: status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestNewInbound_BuildsConfiguredInbound(t *testing.T) {
	t.Parallel()
	secret := []byte("hunter2")
	body := []byte(`{"operation":"message_sent"}`)
	inbound := NewInbound(secret)
	if err := inbound.VerifySignature(signedHeaders(t, secret, body), body); err != nil {
		t.Fatalf("NewInbound did not wire the secret through: %v", err)
	}
}
