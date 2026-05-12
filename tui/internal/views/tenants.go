package views

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"

	"github.com/crashchat-ai/mio/tui/internal/client"
	"github.com/crashchat-ai/mio/tui/internal/styles"
)

// TenantsLoadedMsg is dispatched once ListTenants returns.
type TenantsLoadedMsg struct {
	Tenants []*adminv1.Tenant
	Err     error
}

// TenantsModel renders the list of tenants with a cursor.
type TenantsModel struct {
	admin   client.Admin
	tenants []*adminv1.Tenant
	cursor  int
	err     error
	loading bool
}

func NewTenants(admin client.Admin) TenantsModel {
	return TenantsModel{admin: admin, loading: true}
}

func (m TenantsModel) Init() tea.Cmd {
	return func() tea.Msg {
		list, err := m.admin.ListTenants(context.Background())
		return TenantsLoadedMsg{Tenants: list, Err: err}
	}
}

func (m TenantsModel) Update(msg tea.Msg) (TenantsModel, tea.Cmd) {
	switch v := msg.(type) {
	case TenantsLoadedMsg:
		m.loading = false
		m.tenants = v.Tenants
		m.err = v.Err
	case tea.KeyMsg:
		switch v.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.tenants)-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

func (m TenantsModel) View() string {
	if m.err != nil {
		return styles.Title.Render("Tenants") + "\n\n" + fmt.Sprintf("error: %v", m.err)
	}
	if m.loading {
		return styles.Title.Render("Tenants") + "\n\nloading…"
	}
	if len(m.tenants) == 0 {
		return styles.Title.Render("Tenants") + "\n\nno tenants — create one via the admin RPC"
	}
	var b strings.Builder
	b.WriteString(styles.Title.Render("Tenants") + "\n\n")
	for i, t := range m.tenants {
		row := fmt.Sprintf("%-36s  %-30s  %s",
			t.GetId(), t.GetSlug(), t.GetDisplayName())
		if i == m.cursor {
			row = styles.SelectedRow.Render(row)
		}
		b.WriteString(row + "\n")
	}
	b.WriteString("\n" + styles.Subtle.Render("↑/↓ to move · tab to switch view · q to quit"))
	return b.String()
}

// Selected returns the currently-highlighted tenant, or nil if empty.
func (m TenantsModel) Selected() *adminv1.Tenant {
	if len(m.tenants) == 0 || m.cursor < 0 || m.cursor >= len(m.tenants) {
		return nil
	}
	return m.tenants[m.cursor]
}
