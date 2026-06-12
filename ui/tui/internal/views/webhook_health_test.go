package views

import (
	"strings"
	"testing"

	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"
)

func TestWebhookHealthModel_NoAccount(t *testing.T) {
	m := NewWebhookHealth(&fakeAdmin{})
	view := m.View()
	if !strings.Contains(view, "select an account") {
		t.Errorf("expected prompt to select account, got:\n%s", view)
	}
}

func TestWebhookHealthModel_FoldsInfoMsg(t *testing.T) {
	m := NewWebhookHealth(&fakeAdmin{})
	m.SetAccount("acct-1")
	m, _ = m.Update(WebhookInfoLoadedMsg{Info: &adminv1.GetWebhookInfoResponse{
		AccountId:   "acct-1",
		ChannelType: "zoho_cliq",
		AuthKind:    "oauth2_refresh",
		WebhookUrl:  "https://mio.example.com/webhooks/zoho-cliq",
		SetupHint:   "Click Start Install.",
	}})
	view := m.View()
	if !strings.Contains(view, "zoho_cliq") {
		t.Errorf("view missing channel_type:\n%s", view)
	}
	if !strings.Contains(view, "https://mio.example.com") {
		t.Errorf("view missing webhook url:\n%s", view)
	}
}

func TestWebhookHealthModel_FoldsHealthMsg(t *testing.T) {
	m := NewWebhookHealth(&fakeAdmin{})
	m.SetAccount("acct-1")
	m, _ = m.Update(StreamHealthLoadedMsg{Health: &adminv1.GetStreamHealthResponse{
		Consumers: []*adminv1.ConsumerHealth{
			{ConsumerName: "sender-pool", Stream: "MESSAGES_OUTBOUND", NumPending: 3},
		},
	}})
	view := m.View()
	if !strings.Contains(view, "sender-pool") {
		t.Errorf("view missing consumer name:\n%s", view)
	}
}
