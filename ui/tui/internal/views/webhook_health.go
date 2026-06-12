package views

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"

	"github.com/crashchat-ai/mio/ui/tui/internal/client"
	"github.com/crashchat-ai/mio/ui/tui/internal/styles"
)

type WebhookInfoLoadedMsg struct {
	Info *adminv1.GetWebhookInfoResponse
	Err  error
}

type StreamHealthLoadedMsg struct {
	Health *adminv1.GetStreamHealthResponse
	Err    error
}

type WebhookHealthModel struct {
	admin     client.Admin
	accountID string
	info      *adminv1.GetWebhookInfoResponse
	health    *adminv1.GetStreamHealthResponse
	infoErr   error
	healthErr error
	loading   bool
}

func NewWebhookHealth(admin client.Admin) WebhookHealthModel {
	return WebhookHealthModel{admin: admin}
}

func (m *WebhookHealthModel) SetAccount(id string) {
	m.accountID = id
	m.info = nil
	m.health = nil
	m.infoErr = nil
	m.healthErr = nil
	m.loading = true
}

func (m WebhookHealthModel) Init() tea.Cmd {
	return tea.Batch(m.fetchWebhookInfo(), m.fetchStreamHealth())
}

func (m WebhookHealthModel) fetchWebhookInfo() tea.Cmd {
	if m.accountID == "" {
		return nil
	}
	id := m.accountID
	return func() tea.Msg {
		info, err := m.admin.GetWebhookInfo(context.Background(), id)
		return WebhookInfoLoadedMsg{Info: info, Err: err}
	}
}

func (m WebhookHealthModel) fetchStreamHealth() tea.Cmd {
	return func() tea.Msg {
		health, err := m.admin.GetStreamHealth(context.Background())
		return StreamHealthLoadedMsg{Health: health, Err: err}
	}
}

func (m WebhookHealthModel) Update(msg tea.Msg) (WebhookHealthModel, tea.Cmd) {
	switch v := msg.(type) {
	case WebhookInfoLoadedMsg:
		m.loading = false
		m.info = v.Info
		m.infoErr = v.Err
	case StreamHealthLoadedMsg:
		m.health = v.Health
		m.healthErr = v.Err
	case tea.KeyMsg:
		if v.String() == "r" {
			m.loading = true
			return m, tea.Batch(m.fetchWebhookInfo(), m.fetchStreamHealth())
		}
	}
	return m, nil
}

func (m WebhookHealthModel) View() string {
	var b strings.Builder
	b.WriteString(styles.Title.Render("Onboarding") + "\n\n")

	b.WriteString(styles.TableHeader.Render("Webhook Info") + "\n")
	if m.accountID == "" {
		b.WriteString("  select an account first (accounts view)\n")
	} else if m.infoErr != nil {
		b.WriteString(fmt.Sprintf("  error: %v\n", m.infoErr))
	} else if m.loading || m.info == nil {
		b.WriteString("  loading…\n")
	} else {
		b.WriteString(fmt.Sprintf("  channel:   %s\n", m.info.GetChannelType()))
		b.WriteString(fmt.Sprintf("  auth_kind: %s\n", m.info.GetAuthKind()))
		url := m.info.GetWebhookUrl()
		if url == "" {
			url = "(not configured — set MIO_PUBLIC_BASE_URL)"
		}
		b.WriteString(fmt.Sprintf("  url:       %s\n", url))
		if aliases := m.info.GetRouteAliases(); len(aliases) > 0 {
			b.WriteString(fmt.Sprintf("  aliases:   %s\n", strings.Join(aliases, ", ")))
		}
		if hint := m.info.GetSetupHint(); hint != "" {
			b.WriteString(fmt.Sprintf("  hint:      %s\n", hint))
		}
	}

	b.WriteString("\n" + styles.TableHeader.Render("Stream Health") + "\n")
	if m.healthErr != nil {
		b.WriteString(fmt.Sprintf("  error: %v\n", m.healthErr))
	} else if m.health == nil {
		b.WriteString("  loading…\n")
	} else {
		consumers := m.health.GetConsumers()
		if len(consumers) == 0 {
			b.WriteString("  no consumers\n")
		} else {
			header := []string{"consumer", "stream", "pending", "ack_pending"}
			colWidths := make([]int, len(header))
			for i, h := range header {
				colWidths[i] = len(h)
			}
			rows := make([][]string, 0, len(consumers))
			for _, c := range consumers {
				row := []string{
					c.GetConsumerName(),
					c.GetStream(),
					fmt.Sprintf("%d", c.GetNumPending()),
					fmt.Sprintf("%d", c.GetNumAckPending()),
				}
				rows = append(rows, row)
				for i, cell := range row {
					if l := lipgloss.Width(cell); l > colWidths[i] {
						colWidths[i] = l
					}
				}
			}
			b.WriteString(renderRow(header, colWidths, true) + "\n")
			for _, r := range rows {
				b.WriteString(renderRow(r, colWidths, false) + "\n")
			}
		}
	}

	b.WriteString("\n" + styles.Subtle.Render("r to refresh · tab/shift-tab to switch views · q to quit"))
	return b.String()
}
