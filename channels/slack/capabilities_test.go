package slack

import (
	"testing"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"google.golang.org/protobuf/proto"
)

// TestCapabilities_Verbatim is a regression gate against silent capability
// drift; adding a NEW field requires deliberately updating this struct.
func TestCapabilities_Verbatim(t *testing.T) {
	got := (&Adapter{}).Capabilities()
	want := &miov1.ChannelCapabilities{
		SupportsEdit:      true,
		SupportsDelete:    false,
		SupportsReactions: true,
		SupportsThreads:   true,
		SupportsTyping:    false,
		SupportsPresence:  false,
		AllowedAttachments: []miov1.Attachment_Kind{
			miov1.Attachment_KIND_IMAGE,
			miov1.Attachment_KIND_FILE,
			miov1.Attachment_KIND_AUDIO,
			miov1.Attachment_KIND_VIDEO,
			miov1.Attachment_KIND_LINK,
		},
		MaxTextBytes:        4000,
		RateLimitPerSecond:  1,
		RateLimitScope:      "conversation",
		AuthKind:            "bot_token",
		EditWindowSeconds:   0,
		DeleteWindowSeconds: 0,
	}
	if !proto.Equal(got, want) {
		t.Fatalf("ChannelCapabilities drift.\n got:  %+v\n want: %+v", got, want)
	}
}

func TestCapabilities_DefensiveCopy(t *testing.T) {
	a := &Adapter{}
	first := a.Capabilities()
	first.MaxTextBytes = 1
	if a.Capabilities().GetMaxTextBytes() != 4000 {
		t.Error("Capabilities() must return an independent copy")
	}
}

func TestAdapterInterface(t *testing.T) {
	a := NewAdapter()
	if a.ChannelType() != "slack" {
		t.Errorf("channel_type = %q", a.ChannelType())
	}
	if a.MaxDeliver() != 5 {
		t.Errorf("max_deliver = %d", a.MaxDeliver())
	}
	if a.Inbound() == nil {
		t.Error("Inbound() must be non-nil")
	}
	if a.Credentials() == nil {
		t.Error("Credentials() must be non-nil")
	}
	cmd := &miov1.SendCommand{AccountId: "acct-1", ConversationExternalId: "C01"}
	if got := a.RateLimitKey(cmd); got != "acct-1:C01" {
		t.Errorf("rate_limit_key = %q, want acct-1:C01", got)
	}
}

func TestWorkspaceKey(t *testing.T) {
	inb := (&Adapter{}).Inbound()
	wk, ok := inb.(channels.WorkspaceKeyer)
	if !ok {
		t.Fatal("inbound must implement WorkspaceKeyer")
	}
	msg := &miov1.Message{Attributes: map[string]string{attrSlackTeamID: "T01ABCD2EFG"}}
	if got := wk.WorkspaceKey(msg); got != "T01ABCD2EFG" {
		t.Errorf("workspace_key = %q", got)
	}
}

func TestCredentialsBotToken(t *testing.T) {
	cred := (&Adapter{}).Credentials()
	if cred.AuthorizeURL("state") != "" {
		t.Error("bot_token AuthorizeURL must be empty")
	}
	if _, err := cred.ExchangeCode(t.Context(), "code"); err == nil {
		t.Error("bot_token ExchangeCode must return an error")
	}
	in := channels.Credential{AccessToken: "xoxb-keep"}
	out, err := cred.RefreshCredential(t.Context(), in)
	if err != nil || out.AccessToken != "xoxb-keep" {
		t.Errorf("RefreshCredential must be a no-op: out=%+v err=%v", out, err)
	}
}
