package zohocliq

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
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

func TestCliqInbound_VerifySignature_NilSecret(t *testing.T) {
	inbound := &cliqInbound{secret: nil}
	err := inbound.VerifySignature(http.Header{}, []byte(`{}`))
	if !errors.Is(err, ErrSecretNotConfigured) {
		t.Fatalf("want ErrSecretNotConfigured, got %v", err)
	}
}

func TestCliqInbound_VerifySignature_EmptySecret_DevMode(t *testing.T) {
	inbound := &cliqInbound{secret: []byte("")}
	if err := inbound.VerifySignature(http.Header{}, []byte(`{}`)); err != nil {
		t.Fatalf("dev-mode (empty secret) should accept all; got %v", err)
	}
}

func TestCliqInbound_VerifySignature_ValidSig(t *testing.T) {
	secret := []byte("hunter2")
	body := []byte(`{"operation":"message_sent"}`)
	inbound := &cliqInbound{secret: secret}
	if err := inbound.VerifySignature(signedHeaders(t, secret, body), body); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
}

func TestCliqInbound_VerifySignature_BadSig(t *testing.T) {
	secret := []byte("hunter2")
	body := []byte(`{"operation":"message_sent"}`)
	h := http.Header{}
	h.Set("X-Webhook-Signature", "sha256=deadbeef")
	inbound := &cliqInbound{secret: secret}
	err := inbound.VerifySignature(h, body)
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("want ErrBadSignature, got %v", err)
	}
}

func TestCliqInbound_Normalize_ChannelText(t *testing.T) {
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

func TestCliqInbound_Normalize_ParseError(t *testing.T) {
	inbound := &cliqInbound{}
	if _, err := inbound.Normalize([]byte("not json {")); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestCliqInbound_HandleHandshake_AlwaysFalse(t *testing.T) {
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
	secret := []byte("hunter2")
	body := []byte(`{"operation":"message_sent"}`)
	inbound := NewInbound(secret)
	if err := inbound.VerifySignature(signedHeaders(t, secret, body), body); err != nil {
		t.Fatalf("NewInbound did not wire the secret through: %v", err)
	}
}
