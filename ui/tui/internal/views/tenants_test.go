package views

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"
)

func TestTenantsModel_FoldsLoadedMsg(t *testing.T) {
	t.Parallel()
	fake := &fakeAdmin{}
	m := NewTenants(fake)
	loaded := TenantsLoadedMsg{
		Tenants: []*adminv1.Tenant{
			{Id: "11111111-1111-4111-8111-111111111111", Slug: "acme", DisplayName: "Acme Co"},
			{Id: "22222222-2222-4222-8222-222222222222", Slug: "globex", DisplayName: "Globex Inc"},
		},
	}
	updated, _ := m.Update(loaded)
	view := updated.View()
	if !strings.Contains(view, "acme") {
		t.Errorf("view missing acme:\n%s", view)
	}
	if !strings.Contains(view, "Globex Inc") {
		t.Errorf("view missing Globex Inc")
	}
	if got := updated.Selected(); got == nil || got.GetSlug() != "acme" {
		t.Errorf("default Selected should point at row 0; got %+v", got)
	}
}

func TestTenantsModel_CursorMovesWithinBounds(t *testing.T) {
	t.Parallel()
	fake := &fakeAdmin{}
	m := NewTenants(fake)
	loaded := TenantsLoadedMsg{
		Tenants: []*adminv1.Tenant{{Slug: "a"}, {Slug: "b"}, {Slug: "c"}},
	}
	m, _ = m.Update(loaded)

	// Down twice → cursor at index 2 ("c").
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if got := m.Selected().GetSlug(); got != "c" {
		t.Errorf("after 2× down: %q want c", got)
	}
	// One more down should saturate at last row.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if got := m.Selected().GetSlug(); got != "c" {
		t.Errorf("saturated cursor moved off end: %q", got)
	}
	// Up twice → back to first.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if got := m.Selected().GetSlug(); got != "a" {
		t.Errorf("after 2× up: %q want a", got)
	}
}

func TestTenantsModel_EmptyAndErrorStates(t *testing.T) {
	t.Parallel()
	fake := &fakeAdmin{}
	m := NewTenants(fake)

	// Pre-load (loading=true) view.
	if !strings.Contains(m.View(), "loading") {
		t.Errorf("expected loading state in default view")
	}

	// Empty result.
	loadedEmpty := TenantsLoadedMsg{}
	em, _ := m.Update(loadedEmpty)
	if !strings.Contains(em.View(), "no tenants") {
		t.Errorf("expected empty-state copy")
	}
	if em.Selected() != nil {
		t.Errorf("Selected on empty must be nil")
	}

	// Error path.
	mErr, _ := NewTenants(fake).Update(TenantsLoadedMsg{Err: errOops})
	if !strings.Contains(mErr.View(), "error:") {
		t.Errorf("expected error rendering")
	}
}

// errOops is a sentinel used by the views tests.
var errOops = &simpleError{"oops"}

type simpleError struct{ msg string }

func (e *simpleError) Error() string { return e.msg }
