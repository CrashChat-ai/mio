package views

import (
	"strings"
	"testing"
	"time"

	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestTailModel_NoAccountSelectedHint(t *testing.T) {
	m := NewTail(&fakeAdmin{})
	if !strings.Contains(m.View(), "select an account") {
		t.Errorf("expected no-account hint")
	}
}

func TestTailModel_FoldsTailMsg(t *testing.T) {
	m := NewTail(&fakeAdmin{})
	m.SetAccount("acct-1")

	now := timestamppb.New(time.Unix(1_700_000_000, 0).UTC())
	updated, _ := m.Update(TailMsg{Resp: &adminv1.TailMessagesResponse{
		Id:             "m-1",
		AccountId:      "acct-1",
		ChannelType:    "zoho_cliq",
		ConversationId: "conv-1",
		SenderDisplay:  "Alice",
		Text:           "hello world",
		ReceivedAt:     now,
	}})
	view := updated.View()
	if !strings.Contains(view, "hello world") {
		t.Errorf("view missing text:\n%s", view)
	}
	if !strings.Contains(view, "Alice") {
		t.Errorf("view missing sender")
	}
	if !strings.Contains(view, "zoho_cliq") {
		t.Errorf("view missing channel_type")
	}
}

func TestTailModel_RingCapacityCaps(t *testing.T) {
	m := NewTail(&fakeAdmin{})
	m.SetAccount("acct-cap")

	// Push more than tailLogCapacity messages and assert the log is bounded.
	for i := 0; i < tailLogCapacity+50; i++ {
		m, _ = m.Update(TailMsg{Resp: &adminv1.TailMessagesResponse{
			Id: "m", Text: "t", ReceivedAt: timestamppb.Now(),
		}})
	}
	if got := len(m.log); got != tailLogCapacity {
		t.Errorf("log size %d, want capped at %d", got, tailLogCapacity)
	}
}

func TestTailModel_ErrorRendering(t *testing.T) {
	m := NewTail(&fakeAdmin{})
	m.SetAccount("acct-err")
	updated, _ := m.Update(TailMsg{Err: errOops})
	if !strings.Contains(updated.View(), "error:") {
		t.Errorf("expected error in view")
	}
}

func TestTailModel_StopIdempotent(t *testing.T) {
	m := NewTail(&fakeAdmin{})
	// Stop without Init is a no-op.
	m.Stop()
	m.Stop()
}
