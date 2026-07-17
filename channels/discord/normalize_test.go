package discord

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// Fixtures are AUTHORED synthetic gateway dispatch envelopes (Discord Gateway
// API schema, {"t","d"} as re-wrapped by discordrunner).
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

func TestNormalizeGuildMessage(t *testing.T) {
	msg := mustNormalize(t, "message_create.json")
	if msg.GetChannelType() != "discord" {
		t.Errorf("channel_type = %q", msg.GetChannelType())
	}
	if msg.GetConversationExternalId() != "1200000000000000001" {
		t.Errorf("conversation = %q", msg.GetConversationExternalId())
	}
	if msg.GetConversationKind() != miov1.ConversationKind_CONVERSATION_KIND_CHANNEL_PUBLIC {
		t.Errorf("kind = %v", msg.GetConversationKind())
	}
	if got, want := msg.GetSourceMessageId(), "1200000000000000001:1210000000000000001"; got != want {
		t.Errorf("source_message_id = %q, want %q", got, want)
	}
	if msg.GetSender().GetExternalId() != "980000000000000001" {
		t.Errorf("sender = %q", msg.GetSender().GetExternalId())
	}
	if msg.GetSender().GetDisplayName() != "Alice" {
		t.Errorf("display_name = %q (global_name must win over username)", msg.GetSender().GetDisplayName())
	}
	if msg.GetText() != "hello from a guild channel" {
		t.Errorf("text = %q", msg.GetText())
	}
	if msg.GetAttributes()[attrDiscordGuildID] != "1190000000000000001" {
		t.Errorf("guild attr = %q", msg.GetAttributes()[attrDiscordGuildID])
	}
	if msg.GetRelation() != nil {
		t.Errorf("plain message must not carry a relation")
	}
	if msg.GetReceivedAt().AsTime().Year() != 2026 {
		t.Errorf("received_at must come from the event timestamp, got %v", msg.GetReceivedAt().AsTime())
	}
}

func TestNormalizeDMKind(t *testing.T) {
	msg := mustNormalize(t, "message_create_dm.json")
	if msg.GetConversationKind() != miov1.ConversationKind_CONVERSATION_KIND_DM {
		t.Errorf("kind = %v, want DM (no guild_id)", msg.GetConversationKind())
	}
	if _, ok := msg.GetAttributes()[attrDiscordGuildID]; ok {
		t.Errorf("dm must not carry a guild attr")
	}
}

func TestNormalizeBotEchoSoftDrop(t *testing.T) {
	_, err := normalizeFixture(t, "message_create_bot.json")
	if !errors.Is(err, channels.ErrNormalizeSoft) {
		t.Errorf("bot echo must soft-drop, got err=%v", err)
	}
}

func TestNormalizeReply(t *testing.T) {
	msg := mustNormalize(t, "message_create_reply.json")
	wantRoot := "1200000000000000001:1210000000000000001"
	if msg.GetThreadRootMessageId() != wantRoot {
		t.Errorf("thread_root = %q, want %q", msg.GetThreadRootMessageId(), wantRoot)
	}
	rel := msg.GetRelation()
	if rel.GetKind() != miov1.MessageRelation_KIND_REPLY {
		t.Errorf("relation kind = %v, want KIND_REPLY", rel.GetKind())
	}
	if rel.GetTargetExternalId() != wantRoot {
		t.Errorf("relation target = %q, want %q", rel.GetTargetExternalId(), wantRoot)
	}
}

func TestNormalizeAttachment(t *testing.T) {
	msg := mustNormalize(t, "message_create_attachment.json")
	if len(msg.GetAttachments()) != 1 {
		t.Fatalf("attachments = %d, want 1", len(msg.GetAttachments()))
	}
	att := msg.GetAttachments()[0]
	if att.GetKind() != miov1.Attachment_KIND_IMAGE {
		t.Errorf("attachment kind = %v, want KIND_IMAGE", att.GetKind())
	}
	if att.GetFilename() != "screenshot.png" || att.GetBytes() != 20480 {
		t.Errorf("attachment meta = %q/%d", att.GetFilename(), att.GetBytes())
	}
}

func TestNormalizeEdit(t *testing.T) {
	msg := mustNormalize(t, "message_update.json")
	target := "1200000000000000001:1210000000000000001"
	if msg.GetRelation().GetKind() != miov1.MessageRelation_KIND_EDIT {
		t.Errorf("relation kind = %v, want KIND_EDIT", msg.GetRelation().GetKind())
	}
	if msg.GetRelation().GetTargetExternalId() != target {
		t.Errorf("edit target = %q, want %q", msg.GetRelation().GetTargetExternalId(), target)
	}
	if msg.GetText() != "hello from a guild channel (edited)" {
		t.Errorf("edit text = %q", msg.GetText())
	}
	if msg.GetSender().GetExternalId() != "980000000000000001" {
		t.Errorf("edit sender = %q", msg.GetSender().GetExternalId())
	}
}

func TestNormalizeUnfurlUpdateSoftDrop(t *testing.T) {
	_, err := normalizeFixture(t, "message_update_unfurl.json")
	if !errors.Is(err, channels.ErrNormalizeSoft) {
		t.Errorf("authorless update must soft-drop, got err=%v", err)
	}
}

func TestNormalizeDelete(t *testing.T) {
	msg := mustNormalize(t, "message_delete.json")
	target := "1200000000000000001:1210000000000000001"
	if msg.GetRelation().GetKind() != miov1.MessageRelation_KIND_DELETE {
		t.Errorf("relation kind = %v, want KIND_DELETE", msg.GetRelation().GetKind())
	}
	if msg.GetRelation().GetTargetExternalId() != target {
		t.Errorf("delete target = %q, want %q", msg.GetRelation().GetTargetExternalId(), target)
	}
	// MESSAGE_DELETE carries no author — never invent an id, never flag the
	// unknown author as a bot (slack learned this the hard way, mio#81).
	if got := msg.GetSender().GetExternalId(); got != "" {
		t.Errorf("delete sender = %q, want empty", got)
	}
	if msg.GetSender().GetIsBot() {
		t.Error("delete is_bot = true, want false for unknown authorship")
	}
}

func TestNormalizeReactionAdd(t *testing.T) {
	msg := mustNormalize(t, "reaction_add.json")
	target := "1200000000000000001:1210000000000000001"
	rel := msg.GetRelation()
	if rel.GetKind() != miov1.MessageRelation_KIND_REACTION {
		t.Errorf("relation kind = %v, want KIND_REACTION", rel.GetKind())
	}
	if rel.GetTargetExternalId() != target {
		t.Errorf("reaction target = %q, want %q", rel.GetTargetExternalId(), target)
	}
	if rel.GetReactionEmoji() != "👍" {
		t.Errorf("reaction emoji = %q", rel.GetReactionEmoji())
	}
	if msg.GetSender().GetExternalId() != "980000000000000002" {
		t.Errorf("reaction sender = %q", msg.GetSender().GetExternalId())
	}
	if msg.GetAttributes()[attrDiscordReactionRemoved] != "" {
		t.Errorf("reaction_add must not carry removed attr")
	}
	if msg.GetSourceMessageId() == "" || msg.GetSourceMessageId() == target {
		t.Errorf("reaction needs its own synthetic source id, got %q", msg.GetSourceMessageId())
	}
}

func TestNormalizeReactionRemoved(t *testing.T) {
	msg := mustNormalize(t, "reaction_remove.json")
	if msg.GetRelation().GetKind() != miov1.MessageRelation_KIND_REACTION {
		t.Errorf("removed must reuse KIND_REACTION, got %v", msg.GetRelation().GetKind())
	}
	if msg.GetAttributes()[attrDiscordReactionRemoved] != "true" {
		t.Errorf("discord_reaction_removed attr = %q, want true", msg.GetAttributes()[attrDiscordReactionRemoved])
	}
	add := mustNormalize(t, "reaction_add.json")
	if add.GetSourceMessageId() == msg.GetSourceMessageId() {
		t.Errorf("add and remove must dedup separately, both got %q", msg.GetSourceMessageId())
	}
}

func TestNormalizeUnhandledEventSoftDrop(t *testing.T) {
	env := &Envelope{T: "TYPING_START", D: []byte(`{}`)}
	_, err := Normalize(env)
	if !errors.Is(err, channels.ErrNormalizeSoft) {
		t.Errorf("unhandled event must soft-drop, got err=%v", err)
	}
}
