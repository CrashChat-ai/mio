package sender_test

import (
	"context"
	"testing"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"github.com/crashchat-ai/mio/services/gateway/internal/sender"
)

// stubAdapter implements channels.Adapter for testing.
type stubAdapter struct {
	slug string
}

func (s *stubAdapter) Send(_ context.Context, _ *miov1.SendCommand) (string, error) {
	return "ext-id-123", nil
}
func (s *stubAdapter) Edit(_ context.Context, _ *miov1.SendCommand) error { return nil }
func (s *stubAdapter) ChannelType() string                                { return s.slug }
func (s *stubAdapter) MaxDeliver() int                                    { return 5 }
func (s *stubAdapter) RateLimitKey(_ *miov1.SendCommand) string           { return "" }
func (s *stubAdapter) Capabilities() *miov1.ChannelCapabilities           { return &miov1.ChannelCapabilities{} }
func (s *stubAdapter) Inbound() channels.InboundAdapter                   { return nil }
func (s *stubAdapter) Credentials() channels.CredentialAdapter            { return nil }

func TestDispatcher_ForCommand_Found(t *testing.T) {
	a := &stubAdapter{slug: "test_channel"}
	d := sender.New([]channels.Adapter{a})

	cmd := &miov1.SendCommand{ChannelType: "test_channel"}
	got := d.ForCommand(cmd)
	if got == nil {
		t.Fatal("expected adapter, got nil")
	}
	if got.ChannelType() != "test_channel" {
		t.Fatalf("expected test_channel, got %s", got.ChannelType())
	}
}

func TestDispatcher_ForCommand_NotFound(t *testing.T) {
	a := &stubAdapter{slug: "test_channel"}
	d := sender.New([]channels.Adapter{a})

	cmd := &miov1.SendCommand{ChannelType: "other_channel"}
	got := d.ForCommand(cmd)
	if got != nil {
		t.Fatalf("expected nil for unknown channel_type, got %v", got)
	}
}

func TestDispatcher_New_PanicOnDuplicate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate slug")
		}
	}()
	a1 := &stubAdapter{slug: "dup"}
	a2 := &stubAdapter{slug: "dup"}
	sender.New([]channels.Adapter{a1, a2}) // must panic
}

func TestDispatcher_Empty(t *testing.T) {
	d := sender.New(nil)
	cmd := &miov1.SendCommand{ChannelType: "anything"}
	if d.ForCommand(cmd) != nil {
		t.Fatal("expected nil from empty dispatcher")
	}
}

// TestDispatch_NoBranchOnChannelType verifies that dispatch.go contains
// no adapter-specific string literals (P9 litmus: zero channel branches).
// This is also verified via CI grep, but an explicit test makes it visible.
func TestDispatch_ZeroAdapterBranches(t *testing.T) {
	// This test is structural documentation; the grep CI target is the real gate.
	// If dispatch.go compiles and ForCommand works via table lookup, the design is correct.
	d := sender.New([]channels.Adapter{
		&stubAdapter{slug: "alpha"},
		&stubAdapter{slug: "beta"},
		&stubAdapter{slug: "gamma"},
	})
	for _, slug := range []string{"alpha", "beta", "gamma", "delta"} {
		cmd := &miov1.SendCommand{ChannelType: slug}
		got := d.ForCommand(cmd)
		if slug == "delta" {
			if got != nil {
				t.Errorf("expected nil for unregistered slug %q", slug)
			}
		} else {
			if got == nil {
				t.Errorf("expected adapter for slug %q", slug)
			}
		}
	}
}
