// Package views holds the bubbletea Model/Update/View triples. Each file
// is one view; the App model in cmd/mio-tui owns the current view.
package views

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"

	"github.com/crashchat-ai/mio/tui/internal/client"
	"github.com/crashchat-ai/mio/tui/internal/styles"
)

// ChannelsLoadedMsg is dispatched into the bubbletea event loop once the
// async ListChannelTypes call returns. Carrying the proto envelope keeps
// the view's mutation step in the standard Update() path.
type ChannelsLoadedMsg struct {
	Channels []*adminv1.ChannelTypeInfo
	Err      error
}

// ChannelsModel renders the capability matrix from ListChannelTypes.
type ChannelsModel struct {
	admin    client.Admin
	channels []*adminv1.ChannelTypeInfo
	err      error
	loading  bool
}

// NewChannels returns a fresh ChannelsModel; call Init() to kick off the
// first fetch.
func NewChannels(admin client.Admin) ChannelsModel {
	return ChannelsModel{admin: admin, loading: true}
}

// Init dispatches the ListChannelTypes request asynchronously.
func (m ChannelsModel) Init() tea.Cmd {
	return func() tea.Msg {
		list, err := m.admin.ListChannelTypes(context.Background())
		return ChannelsLoadedMsg{Channels: list, Err: err}
	}
}

// Update folds messages into the model.
func (m ChannelsModel) Update(msg tea.Msg) (ChannelsModel, tea.Cmd) {
	switch v := msg.(type) {
	case ChannelsLoadedMsg:
		m.loading = false
		m.channels = v.Channels
		m.err = v.Err
	}
	return m, nil
}

// View renders the capability matrix as a lipgloss table.
// Rows = channel_type; columns = edit/react/thread/typing/presence/auth_kind.
// Bool fields render as ✓ / ✗ to stay terminal-friendly (single Unicode
// per cell, no decorative emoji).
func (m ChannelsModel) View() string {
	if m.err != nil {
		return styles.Title.Render("Channels") + "\n\n" + fmt.Sprintf("error: %v", m.err)
	}
	if m.loading {
		return styles.Title.Render("Channels") + "\n\nloading…"
	}
	if len(m.channels) == 0 {
		return styles.Title.Render("Channels") + "\n\nno channel types registered"
	}

	header := []string{"slug", "edit", "react", "thread", "typing", "presence", "auth_kind", "max_bytes"}
	colWidths := make([]int, len(header))
	for i, h := range header {
		colWidths[i] = len(h)
	}
	rows := make([][]string, 0, len(m.channels))
	for _, c := range m.channels {
		cap := c.GetCapabilities()
		row := []string{
			c.GetSlug(),
			boolGlyph(cap.GetSupportsEdit()),
			boolGlyph(cap.GetSupportsReactions()),
			boolGlyph(cap.GetSupportsThreads()),
			boolGlyph(cap.GetSupportsTyping()),
			boolGlyph(cap.GetSupportsPresence()),
			cap.GetAuthKind(),
			fmt.Sprintf("%d", cap.GetMaxTextBytes()),
		}
		rows = append(rows, row)
		for i, cell := range row {
			if l := lipgloss.Width(cell); l > colWidths[i] {
				colWidths[i] = l
			}
		}
	}

	var b strings.Builder
	b.WriteString(styles.Title.Render("Channels") + "\n\n")
	b.WriteString(renderRow(header, colWidths, true) + "\n")
	for _, r := range rows {
		b.WriteString(renderRow(r, colWidths, false) + "\n")
	}
	b.WriteString("\n" + styles.Subtle.Render("tab/shift-tab to switch views · q to quit"))
	return b.String()
}

func boolGlyph(b bool) string {
	if b {
		return "✓"
	}
	return "✗"
}

func renderRow(cells []string, widths []int, header bool) string {
	var b strings.Builder
	for i, c := range cells {
		cell := lipgloss.NewStyle().Width(widths[i] + 2).Render(c)
		if header {
			cell = styles.TableHeader.Render(cell)
		} else {
			cell = styles.TableCell.Render(cell)
		}
		b.WriteString(cell)
	}
	return b.String()
}

// Channels exposes the loaded slice for tests + sibling views.
func (m ChannelsModel) Channels() []*adminv1.ChannelTypeInfo { return m.channels }
