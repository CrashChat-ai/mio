package slack

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// Fixtures are AUTHORED synthetic event_callback envelopes (Slack Events API
// schema). P6 replaces them with real test-workspace captures.
func normalizeFixture(t *testing.T, name string) (*miov1.Message, error) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	env, err := ParseEnvelope(body)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return Normalize(env)
}

func mustNormalize(t *testing.T, name string) *miov1.Message {
	t.Helper()
	msg, err := normalizeFixture(t, name)
	if err != nil {
		t.Fatalf("normalize %s: unexpected error: %v", name, err)
	}
	return msg
}

func TestNormalizePlainMessage(t *testing.T) {
	msg := mustNormalize(t, "message.json")
	if msg.GetChannelType() != "slack" {
		t.Errorf("channel_type = %q", msg.GetChannelType())
	}
	if msg.GetConversationExternalId() != "C01PUBLIC001" {
		t.Errorf("conversation = %q", msg.GetConversationExternalId())
	}
	if msg.GetConversationKind() != miov1.ConversationKind_CONVERSATION_KIND_CHANNEL_PUBLIC {
		t.Errorf("kind = %v", msg.GetConversationKind())
	}
	if got, want := msg.GetSourceMessageId(), "C01PUBLIC001:1700000000.000100"; got != want {
		t.Errorf("source_message_id = %q, want %q", got, want)
	}
	if msg.GetSender().GetExternalId() != "U01SENDER001" {
		t.Errorf("sender = %q", msg.GetSender().GetExternalId())
	}
	if msg.GetText() != "hello from a public channel" {
		t.Errorf("text = %q", msg.GetText())
	}
	if msg.GetAttributes()[attrSlackTS] != "1700000000.000100" {
		t.Errorf("slack_ts attr = %q", msg.GetAttributes()[attrSlackTS])
	}
	if msg.GetAttributes()[attrSlackTeamID] != "T01ABCD2EFG" {
		t.Errorf("slack_team_id attr = %q", msg.GetAttributes()[attrSlackTeamID])
	}
	if msg.GetAttributes()[attrSlackEventID] != "Ev01PLAINMSG1" {
		t.Errorf("slack_event_id attr = %q", msg.GetAttributes()[attrSlackEventID])
	}
	if msg.GetRelation() != nil {
		t.Errorf("plain message must not carry a relation")
	}
}

func TestNormalizeConversationKinds(t *testing.T) {
	cases := []struct {
		fixture string
		want    miov1.ConversationKind
	}{
		{"message.json", miov1.ConversationKind_CONVERSATION_KIND_CHANNEL_PUBLIC},
		{"message_private.json", miov1.ConversationKind_CONVERSATION_KIND_CHANNEL_PRIVATE},
		{"message_dm.json", miov1.ConversationKind_CONVERSATION_KIND_DM},
		{"message_mpim.json", miov1.ConversationKind_CONVERSATION_KIND_GROUP_DM},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			if got := mustNormalize(t, tc.fixture).GetConversationKind(); got != tc.want {
				t.Errorf("kind = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNormalizeThreadReply(t *testing.T) {
	msg := mustNormalize(t, "thread_reply.json")
	wantRoot := "C01PUBLIC001:1700000000.000100"
	if msg.GetThreadRootMessageId() != wantRoot {
		t.Errorf("thread_root = %q, want %q", msg.GetThreadRootMessageId(), wantRoot)
	}
	if msg.GetParentConversationId() != wantRoot {
		t.Errorf("parent_conversation_id = %q, want %q", msg.GetParentConversationId(), wantRoot)
	}
	rel := msg.GetRelation()
	if rel.GetKind() != miov1.MessageRelation_KIND_REPLY {
		t.Errorf("relation kind = %v, want KIND_REPLY", rel.GetKind())
	}
	if rel.GetTargetExternalId() != wantRoot {
		t.Errorf("relation target = %q, want %q", rel.GetTargetExternalId(), wantRoot)
	}
	if msg.GetSourceMessageId() != "C01PUBLIC001:1700000100.000500" {
		t.Errorf("source_message_id = %q", msg.GetSourceMessageId())
	}
}

func TestNormalizeThreadBroadcast(t *testing.T) {
	msg := mustNormalize(t, "thread_broadcast.json")
	if msg.GetAttributes()[attrSlackThreadBroadcast] != "true" {
		t.Errorf("thread_broadcast attr = %q", msg.GetAttributes()[attrSlackThreadBroadcast])
	}
	if msg.GetRelation().GetKind() != miov1.MessageRelation_KIND_REPLY {
		t.Errorf("broadcast must be KIND_REPLY, got %v", msg.GetRelation().GetKind())
	}
}

func TestNormalizeEdit(t *testing.T) {
	msg := mustNormalize(t, "message_changed.json")
	target := "C01PUBLIC001:1700000000.000100"
	if msg.GetRelation().GetKind() != miov1.MessageRelation_KIND_EDIT {
		t.Errorf("relation kind = %v, want KIND_EDIT", msg.GetRelation().GetKind())
	}
	if msg.GetRelation().GetTargetExternalId() != target {
		t.Errorf("edit target = %q, want %q", msg.GetRelation().GetTargetExternalId(), target)
	}
	if msg.GetSourceMessageId() != target {
		t.Errorf("edit source_message_id = %q, want %q", msg.GetSourceMessageId(), target)
	}
	if msg.GetText() != "hello from a public channel (edited)" {
		t.Errorf("edit text = %q (must come from nested message)", msg.GetText())
	}
	if msg.GetSender().GetExternalId() != "U01SENDER001" {
		t.Errorf("edit sender = %q", msg.GetSender().GetExternalId())
	}
}

func TestNormalizeEditUnfurlSoftDrop(t *testing.T) {
	_, err := normalizeFixture(t, "message_changed_unfurl.json")
	if !errors.Is(err, channels.ErrNormalizeSoft) {
		t.Errorf("unfurl-only edit must soft-drop, got err=%v", err)
	}
}

func TestNormalizeDelete(t *testing.T) {
	msg := mustNormalize(t, "message_deleted.json")
	target := "C01PUBLIC001:1700000000.000100"
	if msg.GetRelation().GetKind() != miov1.MessageRelation_KIND_DELETE {
		t.Errorf("relation kind = %v, want KIND_DELETE", msg.GetRelation().GetKind())
	}
	if msg.GetRelation().GetTargetExternalId() != target {
		t.Errorf("delete target = %q, want %q", msg.GetRelation().GetTargetExternalId(), target)
	}
}

func TestNormalizeReactionAdded(t *testing.T) {
	msg := mustNormalize(t, "reaction_added.json")
	target := "C01PUBLIC001:1700000000.000100"
	rel := msg.GetRelation()
	if rel.GetKind() != miov1.MessageRelation_KIND_REACTION {
		t.Errorf("relation kind = %v, want KIND_REACTION", rel.GetKind())
	}
	if rel.GetTargetExternalId() != target {
		t.Errorf("reaction target = %q, want %q", rel.GetTargetExternalId(), target)
	}
	if rel.GetReactionEmoji() != "thumbsup" {
		t.Errorf("reaction emoji = %q", rel.GetReactionEmoji())
	}
	if msg.GetAttributes()[attrSlackReactionRemoved] != "" {
		t.Errorf("reaction_added must not carry removed attr")
	}
	if msg.GetConversationExternalId() != "C01PUBLIC001" {
		t.Errorf("reaction conversation must come from item.channel, got %q", msg.GetConversationExternalId())
	}
}

func TestNormalizeReactionRemoved(t *testing.T) {
	msg := mustNormalize(t, "reaction_removed.json")
	if msg.GetRelation().GetKind() != miov1.MessageRelation_KIND_REACTION {
		t.Errorf("removed must reuse KIND_REACTION, got %v", msg.GetRelation().GetKind())
	}
	if msg.GetAttributes()[attrSlackReactionRemoved] != "true" {
		t.Errorf("slack_reaction_removed attr = %q, want true", msg.GetAttributes()[attrSlackReactionRemoved])
	}
}

func TestNormalizeFileShare(t *testing.T) {
	msg := mustNormalize(t, "file_share.json")
	if len(msg.GetAttachments()) != 1 {
		t.Fatalf("attachments = %d, want 1", len(msg.GetAttachments()))
	}
	att := msg.GetAttachments()[0]
	if att.GetKind() != miov1.Attachment_KIND_IMAGE {
		t.Errorf("attachment kind = %v, want KIND_IMAGE", att.GetKind())
	}
	if att.GetUrl() != "https://files.slack.com/files-pri/T01ABCD2EFG-F01IMAGE0001/download/screenshot.png" {
		t.Errorf("attachment url = %q (must prefer url_private_download)", att.GetUrl())
	}
	if att.GetMime() != "image/png" {
		t.Errorf("attachment mime = %q", att.GetMime())
	}
	if att.GetBytes() != 20480 {
		t.Errorf("attachment bytes = %d", att.GetBytes())
	}
	if att.GetFilename() != "screenshot.png" {
		t.Errorf("attachment filename = %q", att.GetFilename())
	}
	if msg.GetAttributes()[attrSlackEnterpriseID] != "E01GRID00001" {
		t.Errorf("enterprise_id attr = %q", msg.GetAttributes()[attrSlackEnterpriseID])
	}
}

func TestNormalizeBotEchoSoftDrop(t *testing.T) {
	_, err := normalizeFixture(t, "bot_message.json")
	if !errors.Is(err, channels.ErrNormalizeSoft) {
		t.Errorf("bot echo must soft-drop, got err=%v", err)
	}
}

func TestNormalizeUnhandledEventSoftDrop(t *testing.T) {
	env := &Envelope{Type: "event_callback", Event: EventBody{Type: "team_join"}}
	_, err := Normalize(env)
	if !errors.Is(err, channels.ErrNormalizeSoft) {
		t.Errorf("unhandled event type must soft-drop, got err=%v", err)
	}
}

func TestNormalizeJoinSubtypeSoftDrop(t *testing.T) {
	env := &Envelope{
		Type:  "event_callback",
		Event: EventBody{Type: "message", SubType: "channel_join", Channel: "C01", TS: "1.1"},
	}
	_, err := Normalize(env)
	if !errors.Is(err, channels.ErrNormalizeSoft) {
		t.Errorf("join subtype must soft-drop, got err=%v", err)
	}
}
