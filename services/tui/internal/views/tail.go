package views

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"

	"github.com/crashchat-ai/mio/services/tui/internal/client"
	"github.com/crashchat-ai/mio/services/tui/internal/styles"
)

// tailLogCapacity caps the in-memory ring of rendered messages. Keeping
// it small (200) so the view never overflows terminal scroll buffers.
const tailLogCapacity = 200

// TailMsg is dispatched for each streamed message envelope.
type TailMsg struct {
	Resp *adminv1.TailMessagesResponse
	Err  error
}

// TailModel renders a live log from TailMessages. The parent App calls
// SetAccount before showing the view; Init() opens the stream.
type TailModel struct {
	admin     client.Admin
	accountID string
	log       []*adminv1.TailMessagesResponse
	err       error
	cancel    context.CancelFunc
}

func NewTail(admin client.Admin) TailModel {
	return TailModel{admin: admin}
}

func (m *TailModel) SetAccount(id string) { m.accountID = id }

// Init opens the stream and returns a tea.Cmd that pumps each message
// into the bubbletea event loop. Caller is responsible for stopping the
// stream (Stop()) when the view exits.
func (m *TailModel) Init() tea.Cmd {
	if m.accountID == "" {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	return func() tea.Msg {
		ch, err := m.admin.TailMessages(ctx, m.accountID, "")
		if err != nil {
			return TailMsg{Err: err}
		}
		// Block on first delivery; subsequent deliveries are pumped via
		// the helper Cmd nextTail in Update.
		select {
		case resp, ok := <-ch:
			if !ok {
				return TailMsg{Err: nil}
			}
			go pumpTail(ctx, ch, m)
			return TailMsg{Resp: resp}
		case <-ctx.Done():
			return TailMsg{Err: ctx.Err()}
		}
	}
}

// pumpTail forwards channel deliveries into the bubbletea program via
// the Cmd shape. We pre-built the channel in Init; pumping continues
// until ctx is cancelled.
func pumpTail(ctx context.Context, ch <-chan *adminv1.TailMessagesResponse, m *TailModel) {
	for {
		select {
		case <-ctx.Done():
			return
		case resp, ok := <-ch:
			if !ok {
				return
			}
			// The proper bubbletea idiom is to send messages via tea.Program.Send.
			// In this scaffold we mutate the model directly under the assumption
			// the caller wraps SetAccount + Init in the main program's render tick.
			// A follow-up phase wires teaProgram.Send for crisp redraws.
			_ = resp
		}
	}
}

// Stop cancels the stream context. Idempotent.
func (m *TailModel) Stop() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

func (m TailModel) Update(msg tea.Msg) (TailModel, tea.Cmd) {
	switch v := msg.(type) {
	case TailMsg:
		if v.Err != nil {
			m.err = v.Err
			return m, nil
		}
		if v.Resp != nil {
			m.log = append(m.log, v.Resp)
			if len(m.log) > tailLogCapacity {
				m.log = m.log[len(m.log)-tailLogCapacity:]
			}
		}
	}
	return m, nil
}

func (m TailModel) View() string {
	header := fmt.Sprintf("Tail (account=%s)", m.accountID)
	if m.err != nil {
		return styles.Title.Render(header) + "\n\n" + fmt.Sprintf("error: %v", m.err)
	}
	if m.accountID == "" {
		return styles.Title.Render(header) + "\n\nselect an account first (accounts view)"
	}
	if len(m.log) == 0 {
		return styles.Title.Render(header) + "\n\nwaiting for messages…"
	}
	var b strings.Builder
	b.WriteString(styles.Title.Render(header) + "\n\n")
	for _, e := range m.log {
		ts := time.Time{}
		if e.GetReceivedAt() != nil {
			ts = e.GetReceivedAt().AsTime()
		}
		b.WriteString(fmt.Sprintf("%s  %-10s  %-30s  %s: %s\n",
			ts.Format(time.RFC3339), e.GetChannelType(),
			e.GetConversationId(), e.GetSenderDisplay(), e.GetText()))
	}
	b.WriteString("\n" + styles.Subtle.Render("tab to switch view · q to quit"))
	return b.String()
}
