package config

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

type plainInbound struct{}

func (plainInbound) VerifySignature(http.Header, []byte) error               { return nil }
func (plainInbound) Normalize([]byte) (*miov1.Message, error)                { return nil, nil }
func (plainInbound) HandleHandshake(http.ResponseWriter, *http.Request) bool { return false }

type namerInbound struct {
	plainInbound
	names []string
}

func (n *namerInbound) WebhookSecretNames() []string { return n.names }

type secretsTestAdapter struct {
	slug    string
	inbound channels.InboundAdapter
}

func (a *secretsTestAdapter) Send(context.Context, *miov1.SendCommand) (string, error) {
	return "", nil
}
func (a *secretsTestAdapter) Edit(context.Context, *miov1.SendCommand) error { return nil }
func (a *secretsTestAdapter) ChannelType() string                            { return a.slug }
func (a *secretsTestAdapter) MaxDeliver() int                                { return 5 }
func (a *secretsTestAdapter) RateLimitKey(*miov1.SendCommand) string         { return "" }
func (a *secretsTestAdapter) Capabilities() *miov1.ChannelCapabilities {
	return &miov1.ChannelCapabilities{}
}
func (a *secretsTestAdapter) Inbound() channels.InboundAdapter        { return a.inbound }
func (a *secretsTestAdapter) Credentials() channels.CredentialAdapter { return nil }

func TestLoadWebhookSecrets(t *testing.T) {
	dir := t.TempDir()
	orig := secretsDir
	secretsDir = dir
	t.Cleanup(func() { secretsDir = orig })

	// legacy name wins over convention when listed first
	channels.RegisterAdapter(&secretsTestAdapter{
		slug:    "cfg_legacy",
		inbound: &namerInbound{names: []string{"legacy-secret", "cfg-legacy-webhook-secret"}},
	})
	// default convention name (no SecretNamer)
	channels.RegisterAdapter(&secretsTestAdapter{
		slug:    "cfg_plain",
		inbound: plainInbound{},
	})
	// outbound-only adapters are skipped
	channels.RegisterAdapter(&secretsTestAdapter{slug: "cfg_outonly", inbound: nil})

	writeFile := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("legacy-secret", "legacy-val\n")
	writeFile("cfg-plain-webhook-secret", "plain-val")

	secrets, err := loadWebhookSecrets()
	if err != nil {
		t.Fatal(err)
	}
	if secrets["cfg_legacy"] != "legacy-val" {
		t.Errorf("cfg_legacy = %q, want legacy-val (trimmed)", secrets["cfg_legacy"])
	}
	if secrets["cfg_plain"] != "plain-val" {
		t.Errorf("cfg_plain = %q", secrets["cfg_plain"])
	}
	if _, ok := secrets["cfg_outonly"]; ok {
		t.Error("outbound-only adapter must not load secrets")
	}
}
