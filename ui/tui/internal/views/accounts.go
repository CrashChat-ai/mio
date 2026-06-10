package views

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"

	"github.com/crashchat-ai/mio/ui/tui/internal/client"
	"github.com/crashchat-ai/mio/ui/tui/internal/styles"
)

// AccountsLoadedMsg is dispatched once ListAccounts returns.
type AccountsLoadedMsg struct {
	Accounts []*adminv1.Account
	Err      error
}

// AccountsModel renders accounts for a given tenant. The parent App
// supplies tenantID via SetTenant before showing the view.
type AccountsModel struct {
	admin    client.Admin
	tenantID string
	accounts []*adminv1.Account
	cursor   int
	err      error
	loading  bool
}

func NewAccounts(admin client.Admin) AccountsModel {
	return AccountsModel{admin: admin}
}

// SetTenant assigns the tenant filter; call Init() after to refetch.
func (m *AccountsModel) SetTenant(id string) { m.tenantID = id; m.loading = true }

func (m AccountsModel) Init() tea.Cmd {
	if m.tenantID == "" {
		return nil
	}
	return func() tea.Msg {
		list, err := m.admin.ListAccounts(context.Background(), m.tenantID)
		return AccountsLoadedMsg{Accounts: list, Err: err}
	}
}

func (m AccountsModel) Update(msg tea.Msg) (AccountsModel, tea.Cmd) {
	switch v := msg.(type) {
	case AccountsLoadedMsg:
		m.loading = false
		m.accounts = v.Accounts
		m.err = v.Err
	case tea.KeyMsg:
		switch v.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.accounts)-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

func (m AccountsModel) View() string {
	header := fmt.Sprintf("Accounts (tenant=%s)", m.tenantID)
	if m.err != nil {
		return styles.Title.Render(header) + "\n\n" + fmt.Sprintf("error: %v", m.err)
	}
	if m.tenantID == "" {
		return styles.Title.Render(header) + "\n\nselect a tenant first (tenants view)"
	}
	if m.loading {
		return styles.Title.Render(header) + "\n\nloading…"
	}
	if len(m.accounts) == 0 {
		return styles.Title.Render(header) + "\n\nno accounts in this tenant"
	}
	var b strings.Builder
	b.WriteString(styles.Title.Render(header) + "\n\n")
	for i, a := range m.accounts {
		row := fmt.Sprintf("%-36s  %-12s  %-10s  %-30s  %s",
			a.GetId(), a.GetChannelType(), a.GetProvider(),
			a.GetExternalId(), a.GetDisplayName())
		if i == m.cursor {
			row = styles.SelectedRow.Render(row)
		}
		b.WriteString(row + "\n")
	}
	b.WriteString("\n" + styles.Subtle.Render("↑/↓ · tab to switch view · q to quit"))
	return b.String()
}

func (m AccountsModel) Selected() *adminv1.Account {
	if len(m.accounts) == 0 || m.cursor < 0 || m.cursor >= len(m.accounts) {
		return nil
	}
	return m.accounts[m.cursor]
}
