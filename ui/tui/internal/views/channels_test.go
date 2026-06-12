package views

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// fakeAdmin is a minimal client.Admin for view tests.
type fakeAdmin struct {
	channels []*adminv1.ChannelTypeInfo
}

func (f *fakeAdmin) ListTenants(context.Context) ([]*adminv1.Tenant, error) { return nil, nil }
func (f *fakeAdmin) ListAccounts(context.Context, string) ([]*adminv1.Account, error) {
	return nil, nil
}
func (f *fakeAdmin) ListChannelTypes(context.Context) ([]*adminv1.ChannelTypeInfo, error) {
	return f.channels, nil
}
func (f *fakeAdmin) TailMessages(context.Context, string, string) (<-chan *adminv1.TailMessagesResponse, error) {
	ch := make(chan *adminv1.TailMessagesResponse)
	close(ch)
	return ch, nil
}
func (f *fakeAdmin) GetWebhookInfo(context.Context, string) (*adminv1.GetWebhookInfoResponse, error) {
	return &adminv1.GetWebhookInfoResponse{}, nil
}
func (f *fakeAdmin) GetStreamHealth(context.Context) (*adminv1.GetStreamHealthResponse, error) {
	return &adminv1.GetStreamHealthResponse{}, nil
}

func TestChannelsModel_FoldsLoadedMsg(t *testing.T) {
	fake := &fakeAdmin{channels: []*adminv1.ChannelTypeInfo{
		{Slug: "zoho_cliq", Status: "active", Capabilities: &miov1.ChannelCapabilities{
			AuthKind:     "oauth2_refresh",
			SupportsEdit: true,
		}},
	}}
	m := NewChannels(fake)
	updated, _ := m.Update(ChannelsLoadedMsg{Channels: fake.channels})
	if len(updated.Channels()) != 1 {
		t.Fatalf("expected 1 channel after load, got %d", len(updated.Channels()))
	}
	view := updated.View()
	if !strings.Contains(view, "zoho_cliq") {
		t.Errorf("rendered view should contain zoho_cliq:\n%s", view)
	}
	if !strings.Contains(view, "oauth2_refresh") {
		t.Errorf("rendered view should contain auth_kind value")
	}
}

func TestChannelsModel_InitDispatchesCmd(t *testing.T) {
	fake := &fakeAdmin{channels: []*adminv1.ChannelTypeInfo{
		{Slug: "x", Capabilities: &miov1.ChannelCapabilities{}},
	}}
	m := NewChannels(fake)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init returned nil cmd")
	}
	msg := cmd()
	loaded, ok := msg.(ChannelsLoadedMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want ChannelsLoadedMsg", msg)
	}
	if loaded.Err != nil {
		t.Errorf("err: %v", loaded.Err)
	}
	if len(loaded.Channels) != 1 {
		t.Errorf("len: %d", len(loaded.Channels))
	}
	_ = tea.KeyMsg{} // anchor import
}
