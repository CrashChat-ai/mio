package views

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"
)

func TestAccountsModel_FoldsLoadedMsg(t *testing.T) {
	m := NewAccounts(&fakeAdmin{})
	m.SetTenant("tenant-abc")
	loaded := AccountsLoadedMsg{
		Accounts: []*adminv1.Account{
			{Id: "a1", ChannelType: "zoho_cliq", Provider: "default",
				ExternalId: "ext-1", DisplayName: "Cliq Prod"},
			{Id: "a2", ChannelType: "slack", Provider: "default",
				ExternalId: "ext-2", DisplayName: "Slack Dev"},
		},
	}
	updated, _ := m.Update(loaded)
	view := updated.View()
	if !strings.Contains(view, "zoho_cliq") {
		t.Errorf("view missing zoho_cliq:\n%s", view)
	}
	if !strings.Contains(view, "Slack Dev") {
		t.Errorf("view missing Slack Dev")
	}
	if got := updated.Selected(); got == nil || got.GetId() != "a1" {
		t.Errorf("default Selected: %+v", got)
	}
}

func TestAccountsModel_CursorBounded(t *testing.T) {
	m := NewAccounts(&fakeAdmin{})
	m.SetTenant("t")
	m, _ = m.Update(AccountsLoadedMsg{Accounts: []*adminv1.Account{
		{Id: "a"}, {Id: "b"},
	}})
	// down + down past last
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if got := m.Selected().GetId(); got != "b" {
		t.Errorf("cursor saturate: %q", got)
	}
	// up past first
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if got := m.Selected().GetId(); got != "a" {
		t.Errorf("cursor at top: %q", got)
	}
}

func TestAccountsModel_NoTenantSelectedHint(t *testing.T) {
	m := NewAccounts(&fakeAdmin{})
	if !strings.Contains(m.View(), "select a tenant") {
		t.Errorf("expected no-tenant hint")
	}
}

func TestAccountsModel_EmptyAndErrorStates(t *testing.T) {
	m := NewAccounts(&fakeAdmin{})
	m.SetTenant("t")

	// Loading.
	if !strings.Contains(m.View(), "loading") {
		t.Errorf("expected loading state")
	}

	// Empty.
	em, _ := m.Update(AccountsLoadedMsg{})
	if !strings.Contains(em.View(), "no accounts") {
		t.Errorf("expected empty hint")
	}

	// Error.
	mErr, _ := m.Update(AccountsLoadedMsg{Err: errOops})
	if !strings.Contains(mErr.View(), "error:") {
		t.Errorf("expected error rendering")
	}
}
