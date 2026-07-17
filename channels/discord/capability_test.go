package discord

import (
	"net/http"
	"testing"

	"github.com/bwmarrin/discordgo"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// TestCapabilitiesDrift pins the advertised capabilities loudly — the admin
// API serves these as-is, and silent drift breaks install flows.
func TestCapabilitiesDrift(t *testing.T) {
	a := NewAdapter()
	c := a.Capabilities()

	if !c.GetSupportsEdit() || c.GetSupportsDelete() || !c.GetSupportsReactions() || !c.GetSupportsThreads() {
		t.Errorf("capability flags drifted: edit=%v delete=%v reactions=%v threads=%v",
			c.GetSupportsEdit(), c.GetSupportsDelete(), c.GetSupportsReactions(), c.GetSupportsThreads())
	}
	if c.GetMaxTextBytes() != 2000 {
		t.Errorf("max_text_bytes = %d, want 2000 (Discord bot hard cap)", c.GetMaxTextBytes())
	}
	if c.GetRateLimitScope() != "conversation" || c.GetRateLimitPerSecond() != 1 {
		t.Errorf("rate limit drifted: %d/%s", c.GetRateLimitPerSecond(), c.GetRateLimitScope())
	}
	if c.GetAuthKind() != "bot_token" {
		t.Errorf("auth_kind = %q", c.GetAuthKind())
	}

	// Defensive copy: mutating the returned struct must not leak.
	c.MaxTextBytes = 1
	if a.Capabilities().GetMaxTextBytes() != 2000 {
		t.Error("Capabilities() must return a defensive copy")
	}
}

func TestCompositeRoundTrip(t *testing.T) {
	id := composite("1200000000000000001", "1210000000000000001")
	ch, msg, ok := splitComposite(id)
	if !ok || ch != "1200000000000000001" || msg != "1210000000000000001" {
		t.Errorf("round trip failed: %q → %q %q %v", id, ch, msg, ok)
	}
	if composite("", "x") != "" || composite("x", "") != "" {
		t.Error("half-formed composites must be empty")
	}
	if _, _, ok := splitComposite("bare"); ok {
		t.Error("bare id must not split")
	}
}

func TestClassifyDeliveryError(t *testing.T) {
	rl := &discordgo.RateLimitError{RateLimit: &discordgo.RateLimit{
		TooManyRequests: &discordgo.TooManyRequests{RetryAfter: 2500000000},
	}}
	de := classifyDeliveryError(rl).(*DeliveryError)
	if !de.IsRateLimited() || de.RetryAfterSeconds() != 2 {
		t.Errorf("429: rateLimited=%v retryAfter=%d", de.IsRateLimited(), de.RetryAfterSeconds())
	}

	perm := &discordgo.RESTError{Response: &http.Response{StatusCode: http.StatusForbidden}}
	de = classifyDeliveryError(perm).(*DeliveryError)
	if de.IsRetryable() || de.IsRateLimited() {
		t.Errorf("403 must Term: retryable=%v", de.IsRetryable())
	}

	srv := &discordgo.RESTError{Response: &http.Response{StatusCode: http.StatusBadGateway}}
	de = classifyDeliveryError(srv).(*DeliveryError)
	if !de.IsRetryable() {
		t.Error("502 must Nak (retryable)")
	}

	de = classifyDeliveryError(errTransient{}).(*DeliveryError)
	if !de.IsRetryable() {
		t.Error("unknown errors must default to retryable")
	}
	if classifyDeliveryError(nil) != nil {
		t.Error("nil must stay nil")
	}
}

type errTransient struct{}

func (errTransient) Error() string { return "connection reset by peer" }

// TestAdapterSatisfiesContract locks the interface at compile time and the
// registry slug at run time.
func TestAdapterSatisfiesContract(t *testing.T) {
	a := NewAdapter()
	if a.ChannelType() != "discord" {
		t.Errorf("channel_type = %q", a.ChannelType())
	}
	if a.MaxDeliver() != 5 {
		t.Errorf("max_deliver = %d", a.MaxDeliver())
	}
	key := a.RateLimitKey(&miov1.SendCommand{AccountId: "acc1", ConversationExternalId: "1200000000000000001"})
	if key != "acc1:1200000000000000001" {
		t.Errorf("rate limit key = %q", key)
	}
	if a.Inbound() == nil {
		t.Error("Inbound() must be non-nil")
	}
	if a.Credentials() == nil {
		t.Error("Credentials() must be non-nil")
	}
}
