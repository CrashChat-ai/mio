package discord

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifySignatureEd25519(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	inbound := (&discordInbound{}).WithSecret([]byte(hex.EncodeToString(pub))).(*discordInbound)

	body := []byte(`{"type":1}`)
	ts := "1700000000"
	sig := ed25519.Sign(priv, append([]byte(ts), body...))

	h := http.Header{}
	h.Set("X-Signature-Ed25519", hex.EncodeToString(sig))
	h.Set("X-Signature-Timestamp", ts)
	if err := inbound.VerifySignature(h, body); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}

	h.Set("X-Signature-Timestamp", "1700000001") // signed material changed
	if err := inbound.VerifySignature(h, body); !errors.Is(err, ErrBadSignature) {
		t.Errorf("tampered timestamp accepted: %v", err)
	}

	if err := (&discordInbound{}).VerifySignature(h, body); !errors.Is(err, ErrSecretNotConfigured) {
		t.Errorf("nil-secret instance must refuse, got %v", err)
	}
}

func TestHandleHandshakePing(t *testing.T) {
	inbound := &discordInbound{}

	r := httptest.NewRequest(http.MethodPost, "/webhooks/discord", bytes.NewReader([]byte(`{"type":1}`)))
	w := httptest.NewRecorder()
	if !inbound.HandleHandshake(w, r) {
		t.Fatal("interactions PING must be consumed")
	}
	if w.Body.String() != `{"type":1}` {
		t.Errorf("pong body = %q", w.Body.String())
	}

	// A normal dispatch payload must pass through with the body restored.
	payload := []byte(`{"t":"MESSAGE_CREATE","d":{}}`)
	r = httptest.NewRequest(http.MethodPost, "/webhooks/discord", bytes.NewReader(payload))
	w = httptest.NewRecorder()
	if inbound.HandleHandshake(w, r) {
		t.Fatal("dispatch payload must not be consumed")
	}
	restored := make([]byte, len(payload))
	n, _ := r.Body.Read(restored)
	if string(restored[:n]) != string(payload) {
		t.Errorf("body not restored after handshake probe: %q", restored[:n])
	}
}

func TestWorkspaceKeyIsGuild(t *testing.T) {
	msg := mustNormalize(t, "message_create.json")
	if key := (&discordInbound{}).WorkspaceKey(msg); key != "1190000000000000001" {
		t.Errorf("workspace key = %q, want guild id", key)
	}
}
