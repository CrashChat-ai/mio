package slack

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/crashchat-ai/mio/pkg/channels"
)

func TestHandleHandshakeChallenge(t *testing.T) {
	inb := &slackInbound{}
	body := `{"type":"url_verification","challenge":"abc123challenge"}`
	r := httptest.NewRequest(http.MethodPost, "/webhooks/slack", strings.NewReader(body))
	w := httptest.NewRecorder()

	if !inb.HandleHandshake(w, r) {
		t.Fatal("url_verification must be consumed")
	}
	if w.Body.String() != "abc123challenge" {
		t.Errorf("challenge echo = %q", w.Body.String())
	}
}

func TestHandleHandshakeNonChallengePassesThrough(t *testing.T) {
	inb := &slackInbound{}
	body := `{"type":"event_callback","event":{"type":"message"}}`
	r := httptest.NewRequest(http.MethodPost, "/webhooks/slack", strings.NewReader(body))
	w := httptest.NewRecorder()

	if inb.HandleHandshake(w, r) {
		t.Fatal("event_callback must NOT be consumed by handshake")
	}
	// Body must be restored for the pipeline to re-read.
	got, _ := io.ReadAll(r.Body)
	if string(got) != body {
		t.Errorf("body not restored: got %q", got)
	}
}

func TestVerifySignatureNilSecretIntrospection(t *testing.T) {
	inb := &slackInbound{}
	if err := inb.VerifySignature(http.Header{}, []byte(`{}`)); !errors.Is(err, ErrSecretNotConfigured) {
		t.Errorf("nil-secret introspection instance must return ErrSecretNotConfigured, got %v", err)
	}
}

func TestInboundNormalizeFacade(t *testing.T) {
	inb := &slackInbound{}
	body := []byte(`{"type":"event_callback","team_id":"T01","event":{"type":"message","channel":"C01","channel_type":"channel","user":"U01","text":"hi","ts":"1.1"}}`)
	msg, err := inb.Normalize(body)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if msg.GetSourceMessageId() != "C01:1.1" {
		t.Errorf("source_message_id = %q", msg.GetSourceMessageId())
	}
}

func TestInboundNormalizeSoftError(t *testing.T) {
	inb := &slackInbound{}
	body := []byte(`{"type":"event_callback","event":{"type":"team_join"}}`)
	if _, err := inb.Normalize(body); !errors.Is(err, channels.ErrNormalizeSoft) {
		t.Errorf("unhandled event must wrap ErrNormalizeSoft, got %v", err)
	}
}

func TestInboundImplementsContractSeams(t *testing.T) {
	var inb channels.InboundAdapter = &slackInbound{}
	if _, ok := inb.(channels.SecretConfigurable); !ok {
		t.Error("must implement SecretConfigurable")
	}
	if _, ok := inb.(channels.SecretNamer); !ok {
		t.Error("must implement SecretNamer")
	}
	if _, ok := inb.(channels.WorkspaceKeyer); !ok {
		t.Error("must implement WorkspaceKeyer")
	}
	sn := inb.(channels.SecretNamer)
	if names := sn.WebhookSecretNames(); len(names) == 0 || names[0] != "slack-webhook-secret" {
		t.Errorf("secret names = %v, want [slack-webhook-secret]", sn.WebhookSecretNames())
	}
}
