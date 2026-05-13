// Command mio-tui is a minimal bubbletea TUI for inspecting an mio admin
// server. Connects to ADMIN_URL (default http://127.0.0.1:9090); navigate
// across tenants / accounts / channels / tail with tab / shift-tab; q to quit.
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/crashchat-ai/mio/services/tui/internal/client"
	"github.com/crashchat-ai/mio/services/tui/internal/views"
)

type viewKind int

const (
	viewTenants viewKind = iota
	viewAccounts
	viewChannels
	viewTail
)

type appModel struct {
	admin    client.Admin
	current  viewKind
	tenants  views.TenantsModel
	accounts views.AccountsModel
	channels views.ChannelsModel
	tail     views.TailModel
}

func newApp(admin client.Admin) appModel {
	return appModel{
		admin:    admin,
		current:  viewTenants,
		tenants:  views.NewTenants(admin),
		accounts: views.NewAccounts(admin),
		channels: views.NewChannels(admin),
		tail:     views.NewTail(admin),
	}
}

func (a appModel) Init() tea.Cmd {
	return tea.Batch(a.tenants.Init(), a.channels.Init())
}

func (a appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.KeyMsg:
		switch v.String() {
		case "q", "ctrl+c", "esc":
			a.tail.Stop()
			return a, tea.Quit
		case "tab":
			a.current = (a.current + 1) % 4
			if a.current == viewAccounts {
				if t := a.tenants.Selected(); t != nil {
					a.accounts.SetTenant(t.GetId())
					return a, a.accounts.Init()
				}
			}
			if a.current == viewTail {
				if acct := a.accounts.Selected(); acct != nil {
					a.tail.SetAccount(acct.GetId())
					return a, a.tail.Init()
				}
			}
			return a, nil
		case "shift+tab":
			a.current = (a.current + 3) % 4
			return a, nil
		}
	}

	// Always feed loaded-data messages into every view; only the relevant
	// model's Update will react. Keystrokes go only to the active view.
	var cmds []tea.Cmd

	switch msg.(type) {
	case views.TenantsLoadedMsg, views.AccountsLoadedMsg,
		views.ChannelsLoadedMsg, views.TailMsg:
		var c tea.Cmd
		a.tenants, c = a.tenants.Update(msg)
		cmds = append(cmds, c)
		a.accounts, c = a.accounts.Update(msg)
		cmds = append(cmds, c)
		a.channels, c = a.channels.Update(msg)
		cmds = append(cmds, c)
		a.tail, c = a.tail.Update(msg)
		cmds = append(cmds, c)
	default:
		var c tea.Cmd
		switch a.current {
		case viewTenants:
			a.tenants, c = a.tenants.Update(msg)
		case viewAccounts:
			a.accounts, c = a.accounts.Update(msg)
		case viewChannels:
			a.channels, c = a.channels.Update(msg)
		case viewTail:
			a.tail, c = a.tail.Update(msg)
		}
		cmds = append(cmds, c)
	}
	return a, tea.Batch(cmds...)
}

func (a appModel) View() string {
	switch a.current {
	case viewTenants:
		return a.tenants.View()
	case viewAccounts:
		return a.accounts.View()
	case viewChannels:
		return a.channels.View()
	case viewTail:
		return a.tail.View()
	}
	return ""
}

func main() {
	url := os.Getenv("ADMIN_URL")
	if url == "" {
		url = "http://127.0.0.1:9090"
	}
	app := newApp(client.New(url))
	p := tea.NewProgram(app)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "mio-tui: %v\n", err)
		os.Exit(1)
	}
}
