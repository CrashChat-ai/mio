package zohocliq

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// fixtureDir points to PII-scrubbed copies of the Step 0 captures (originals
// live in playground/cliq/captures/ which is gitignored).
const fixtureDir = "testdata"

func TestNormalize_ChannelText(t *testing.T) {
	body := loadFixture(t, "2026-05-03T10-38-07-channel-text.json")
	payload, err := ParseWebhookPayload(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	nm, err := Normalize(payload)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if nm.ConversationKind != "CONVERSATION_KIND_CHANNEL_PUBLIC" {
		t.Errorf("kind: got %q, want CONVERSATION_KIND_CHANNEL_PUBLIC", nm.ConversationKind)
	}
	if nm.ConversationExternalID == "" {
		t.Error("conversation_external_id must be non-empty")
	}
	if nm.SourceMessageID == "" {
		t.Error("source_message_id must be non-empty")
	}
	if nm.SenderExternalID == "" {
		t.Error("sender_external_id must be non-empty")
	}
	if nm.Text != "halo" {
		t.Errorf("text: got %q, want %q", nm.Text, "halo")
	}
}

func TestNormalize_FileAttachment(t *testing.T) {
	body := loadFixture(t, "2026-05-03T10-39-55-channel-file-attachment.json")
	payload, err := ParseWebhookPayload(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	nm, err := Normalize(payload)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if nm.ConversationKind != "CONVERSATION_KIND_CHANNEL_PUBLIC" {
		t.Errorf("kind: got %q", nm.ConversationKind)
	}
	if nm.Text != "test with file attached" {
		t.Errorf("text (comment fallback): got %q", nm.Text)
	}
	if _, ok := nm.Attributes["cliq_attachment"]; !ok {
		t.Error("expected cliq_attachment in attributes for file message")
	}
	// P9: structured Attachments must also be populated so the
	// attachment-downloader sidecar can find the URL without parsing the
	// JSON-encoded `cliq_attachment` attribute.
	if len(nm.Attachments) != 1 {
		t.Fatalf("expected 1 normalized attachment, got %d", len(nm.Attachments))
	}
	att := nm.Attachments[0]
	if att.URL == "" {
		t.Error("expected non-empty Attachments[0].URL")
	}
	if att.MIME == "" {
		t.Error("expected non-empty Attachments[0].MIME")
	}
	if att.Filename == "" {
		t.Error("expected non-empty Attachments[0].Filename")
	}
}

func TestNormalize_ThreadReply(t *testing.T) {
	body := loadFixture(t, "2026-05-07T22-06-22-channel-thread-reply.json")
	payload, err := ParseWebhookPayload(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	nm, err := Normalize(payload)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if nm.ConversationKind != "CONVERSATION_KIND_THREAD" {
		t.Errorf("kind: got %q, want CONVERSATION_KIND_THREAD", nm.ConversationKind)
	}
	if nm.ParentExternalID == "" {
		t.Error("parent_external_id must be set for thread reply")
	}
}

func TestNormalize_DM(t *testing.T) {
	body := loadFixture(t, "2026-05-03T05-40-13-dm-to-bot.json")
	payload, err := ParseWebhookPayload(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	nm, err := Normalize(payload)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if nm.ConversationKind != "CONVERSATION_KIND_DM" {
		t.Errorf("kind: got %q, want CONVERSATION_KIND_DM", nm.ConversationKind)
	}
}

func TestNormalize_BotMention(t *testing.T) {
	body := loadFixture(t, "2026-05-03T10-41-49-channel-bot-mention.json")
	payload, err := ParseWebhookPayload(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	nm, err := Normalize(payload)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if nm.ConversationKind != "CONVERSATION_KIND_CHANNEL_PUBLIC" {
		t.Errorf("kind: got %q", nm.ConversationKind)
	}
	if _, ok := nm.Attributes["cliq_mentions"]; !ok {
		t.Error("expected cliq_mentions in attributes for mention message")
	}
}

func TestNormalize_MessageEdited(t *testing.T) {
	body := loadFixture(t, "2026-05-07T16-07-01-channel-message-edited.json")
	payload, err := ParseWebhookPayload(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	nm, err := Normalize(payload)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	// Edited messages still map to CHANNEL_PUBLIC; dedup handles idempotency.
	if nm.ConversationKind != "CONVERSATION_KIND_CHANNEL_PUBLIC" {
		t.Errorf("kind: got %q", nm.ConversationKind)
	}
	if nm.SourceMessageID == "" {
		t.Error("source_message_id required even for edits")
	}
}

func TestNormalize_MissingMessageID_Error(t *testing.T) {
	raw := `{"operation":"message_sent","data":{"message":{"text":"hello"}},"chat":{"id":"CT_123","chat_type":"channel"}}`
	payload, err := ParseWebhookPayload([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Normalize(payload)
	if err == nil {
		t.Fatal("expected error for missing message id")
	}
}

// loadFixture reads a JSON fixture and extracts the body_json field if present,
// otherwise returns the raw file bytes.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join(fixtureDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("load fixture %s: %v", name, err)
	}
	// Fixtures wrap the actual payload in a body_json field.
	var wrapper struct {
		BodyJSON json.RawMessage `json:"body_json"`
	}
	if err := json.Unmarshal(data, &wrapper); err == nil && wrapper.BodyJSON != nil {
		return wrapper.BodyJSON
	}
	// Fallback: file is raw payload JSON.
	return data
}
