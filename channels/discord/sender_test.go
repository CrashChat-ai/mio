package discord

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// rewriteTransport redirects every discordgo REST call to the test server —
// no package-global endpoint mutation, so tests stay parallel-safe.
type rewriteTransport struct{ base *url.URL }

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = t.base.Scheme
	req.URL.Host = t.base.Host
	return http.DefaultTransport.RoundTrip(req)
}

func testAdapter(t *testing.T, handler http.HandlerFunc) (*Adapter, func(*discordgo.Session)) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	patch := func(s *discordgo.Session) {
		s.Client = &http.Client{Transport: rewriteTransport{base: u}, Timeout: 5 * time.Second}
	}
	return &Adapter{botToken: "test-token"}, patch
}

// sendVia mirrors Adapter.Send but injects the patched session — the adapter's
// session construction is one line (newDiscordSession) and the delivery logic
// under test lives in the shared helpers it calls.
func sendVia(ctx context.Context, a *Adapter, patch func(*discordgo.Session), cmd *miov1.SendCommand) (string, error) {
	s, err := newDiscordSession(a.botToken)
	if err != nil {
		return "", err
	}
	patch(s)

	channel := cmd.GetConversationExternalId()
	if rel := cmd.GetRelation(); rel.GetKind() == miov1.MessageRelation_KIND_REACTION {
		removed := cmd.GetAttributes()[attrDiscordReactionRemoved] == "true"
		if err := a.reactTo(ctx, s, channel, rel, removed); err != nil {
			return "", err
		}
		return "", nil
	}
	send := &discordgo.MessageSend{Content: cmd.GetText()}
	if ref := replyReference(cmd, channel); ref != nil {
		send.Reference = ref
	}
	m, err := s.ChannelMessageSendComplex(channel, send, discordgo.WithContext(ctx))
	if err != nil {
		return "", classifyDeliveryError(err)
	}
	return composite(channel, m.ID), nil
}

func TestSendReturnsCompositeID(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	a, patch := testAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1230000000000000001","channel_id":"1200000000000000001"}`))
	})

	cmd := &miov1.SendCommand{
		ConversationExternalId: "1200000000000000001",
		Text:                   "**bold** stays raw markdown",
	}
	id, err := sendVia(context.Background(), a, patch, cmd)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if id != "1200000000000000001:1230000000000000001" {
		t.Errorf("external id = %q", id)
	}
	if gotPath != "/api/v9/channels/1200000000000000001/messages" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["content"] != "**bold** stays raw markdown" {
		t.Errorf("content = %v (markdown must pass through untouched)", gotBody["content"])
	}
}

func TestSendReplyCarriesMessageReference(t *testing.T) {
	var gotBody map[string]any
	a, patch := testAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"id":"1230000000000000002","channel_id":"1200000000000000001"}`))
	})

	cmd := &miov1.SendCommand{
		ConversationExternalId: "1200000000000000001",
		Text:                   "reply",
		ThreadRootMessageId:    "1200000000000000001:1210000000000000001",
	}
	if _, err := sendVia(context.Background(), a, patch, cmd); err != nil {
		t.Fatalf("send: %v", err)
	}
	ref, _ := gotBody["message_reference"].(map[string]any)
	if ref == nil || ref["message_id"] != "1210000000000000001" {
		t.Errorf("message_reference = %v (thread root must resolve to bare id)", gotBody["message_reference"])
	}
}

func TestSendReactionMintsNoID(t *testing.T) {
	var gotPath, gotMethod string
	a, patch := testAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.WriteHeader(http.StatusNoContent)
	})

	cmd := &miov1.SendCommand{
		ConversationExternalId: "1200000000000000001",
		Relation: &miov1.MessageRelation{
			Kind:             miov1.MessageRelation_KIND_REACTION,
			TargetExternalId: "1200000000000000001:1210000000000000001",
			ReactionEmoji:    "👍",
		},
	}
	id, err := sendVia(context.Background(), a, patch, cmd)
	if err != nil {
		t.Fatalf("react: %v", err)
	}
	if id != "" {
		t.Errorf("reaction must mint no external id, got %q", id)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if gotPath == "" || gotPath == "/api/v9/channels/1200000000000000001/messages" {
		t.Errorf("reaction hit the message-create path: %q", gotPath)
	}
}

func TestSendWithoutTokenFails(t *testing.T) {
	a := &Adapter{}
	if _, err := a.Send(context.Background(), &miov1.SendCommand{ConversationExternalId: "c"}); err == nil {
		t.Fatal("send without token must error")
	}
}

func TestEditRequiresCompositeID(t *testing.T) {
	a := &Adapter{botToken: "test-token"}
	err := a.Edit(context.Background(), &miov1.SendCommand{EditOfExternalId: "bare-id-no-colon"})
	if err == nil {
		t.Fatal("edit with non-composite id must error rather than mis-target")
	}
}
