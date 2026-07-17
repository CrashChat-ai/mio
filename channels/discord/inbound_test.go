package discord

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestVerifySignatureEd25519(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	inbound := (&discordInbound{}).WithSecret([]byte(hex.EncodeToString(pub))).(*discordInbound)

	body := []byte(`{"type":1}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := ed25519.Sign(priv, append([]byte(ts), body...))

	h := http.Header{}
	h.Set("X-Signature-Ed25519", hex.EncodeToString(sig))
	h.Set("X-Signature-Timestamp", ts)
	if err := inbound.VerifySignature(h, body); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}

	h.Set("X-Signature-Timestamp", strconv.FormatInt(time.Now().Unix()+1, 10)) // signed material changed
	if err := inbound.VerifySignature(h, body); !errors.Is(err, ErrBadSignature) {
		t.Errorf("tampered timestamp accepted: %v", err)
	}

	if err := (&discordInbound{}).VerifySignature(h, body); !errors.Is(err, ErrSecretNotConfigured) {
		t.Errorf("nil-secret instance must refuse, got %v", err)
	}
}

func TestVerifySignatureReplayWindow(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	now := time.Unix(1_700_000_000, 0)
	sign := func(ts string) string {
		return hex.EncodeToString(ed25519.Sign(priv, append([]byte(ts), []byte("body")...)))
	}

	fresh := strconv.FormatInt(now.Add(-time.Minute).Unix(), 10)
	if err := verifyEd25519(pub, []byte("body"), sign(fresh), fresh, now); err != nil {
		t.Errorf("fresh signature rejected: %v", err)
	}

	// A validly-signed but stale request must be rejected — Discord signs
	// timestamp||body, so without the window a capture replays verbatim.
	stale := strconv.FormatInt(now.Add(-signatureMaxSkew-time.Second).Unix(), 10)
	if err := verifyEd25519(pub, []byte("body"), sign(stale), stale, now); !errors.Is(err, ErrBadSignature) {
		t.Errorf("stale signature accepted: %v", err)
	}

	future := strconv.FormatInt(now.Add(signatureMaxSkew+time.Second).Unix(), 10)
	if err := verifyEd25519(pub, []byte("body"), sign(future), future, now); !errors.Is(err, ErrBadSignature) {
		t.Errorf("future signature accepted: %v", err)
	}

	if err := verifyEd25519(pub, []byte("body"), sign("junk"), "junk", now); !errors.Is(err, ErrBadSignature) {
		t.Errorf("non-numeric timestamp accepted: %v", err)
	}
}

func TestVerifySignatureBadSecretShapes(t *testing.T) {
	h := http.Header{}
	short := (&discordInbound{}).WithSecret([]byte("abcd")).(*discordInbound) // hex but wrong length
	if err := short.VerifySignature(h, nil); err == nil || errors.Is(err, ErrBadSignature) {
		t.Errorf("wrong-length key must fail with a config error, got %v", err)
	}
	nonhex := (&discordInbound{}).WithSecret([]byte("zz")).(*discordInbound)
	if err := nonhex.VerifySignature(h, nil); err == nil {
		t.Error("non-hex key must fail")
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
