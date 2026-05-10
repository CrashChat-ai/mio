package zohocliq

// normalize.go — maps a Cliq webhook payload to a mio.v1.Message.
//
// Informed by Step 0 captures in playground/cliq/captures/ and FINDINGS.md.
// Struct fields match the ACTUAL captured payload shapes — no speculative fields.
//
// ConversationKind decision tree (locked in FINDINGS.md):
//  1. operation=="message" && chat.is_dm==true  → CONVERSATION_KIND_DM
//  2. operation=="message_sent" && data.message.replied_message!=nil → CONVERSATION_KIND_THREAD
//  3. operation=="message_sent" && chat.chat_type=="channel" → CONVERSATION_KIND_CHANNEL_PUBLIC
//  4. operation=="message_edited" → treat as CHANNEL_PUBLIC (deduplicated by idempotency key)
//  5. Fallback → CONVERSATION_KIND_UNSPECIFIED, logged WARN

import (
	"encoding/json"
	"fmt"
	"strings"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// WebhookPayload is the union of Participation Handler and Message Handler shapes.
// Fields are optional (pointer or interface) because the two handlers differ:
// - Participation Handler: operation="message_sent", data.message, chat.chat_type="channel"
// - Message Handler (DM): operation="message", top-level message, chat.is_dm=true
type WebhookPayload struct {
	Operation string          `json:"operation"`
	Data      *PayloadData    `json:"data,omitempty"`    // Participation Handler
	Message   *MessageObj     `json:"message,omitempty"` // Message Handler (DM to bot)
	User      *UserObj        `json:"user,omitempty"`
	Chat      *ChatObj        `json:"chat,omitempty"`
}

// PayloadData is the "data" envelope in Participation Handler payloads.
type PayloadData struct {
	Time    int64       `json:"time,omitempty"`
	Message *MessageObj `json:"message,omitempty"`
}

// MessageObj holds the message fields. Present under data.message (channel)
// or top-level message (DM).
type MessageObj struct {
	ID              string          `json:"id,omitempty"`
	Text            string          `json:"text,omitempty"`
	Comment         string          `json:"comment,omitempty"` // attachment caption
	Type            string          `json:"type,omitempty"`    // "text" | "attachment"
	Mentions        []MentionObj    `json:"mentions,omitempty"`
	File            *FileObj        `json:"file,omitempty"`
	RepliedMessage  *RepliedMessage `json:"replied_message,omitempty"`
}

// RepliedMessage is the thread parent reference (FINDINGS.md Q3).
// Present when the message is a reply to another message.
type RepliedMessage struct {
	ID     string   `json:"id,omitempty"`
	Text   string   `json:"text,omitempty"`
	Sender *UserRef `json:"sender,omitempty"`
	Time   int64    `json:"time,omitempty"`
	Type   string   `json:"type,omitempty"`
}

// UserRef is a minimal user reference (sender inside replied_message).
type UserRef struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// MentionObj is a @mention in the message.
type MentionObj struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	DName string `json:"dname,omitempty"`
	Type  string `json:"type,omitempty"` // "user" | "bot"
}

// FileObj holds attachment metadata.
type FileObj struct {
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"` // MIME type
	URL  string `json:"url,omitempty"`
}

// UserObj is the message sender from Cliq.
type UserObj struct {
	ID         string `json:"id,omitempty"`
	Name       string `json:"name,omitempty"`
	FirstName  string `json:"first_name,omitempty"`
	LastName   string `json:"last_name,omitempty"`
	IsBot      bool   `json:"is_bot,omitempty"`
	ZohoUserID string `json:"zoho_user_id,omitempty"`
	Email      string `json:"email,omitempty"`
}

// ChatObj holds conversation metadata.
type ChatObj struct {
	ID                string `json:"id,omitempty"`
	Title             string `json:"title,omitempty"`
	ChatType          string `json:"chat_type,omitempty"`           // "channel"
	ChannelUniqueName string `json:"channel_unique_name,omitempty"` // public channel slug
	IsDM              bool   `json:"is_dm,omitempty"`               // DM signal (FINDINGS Q1)
	ChannelID         string `json:"channel_id,omitempty"`
	Owner             string `json:"owner,omitempty"`
}

// ParseWebhookPayload unmarshals raw body bytes into a WebhookPayload.
func ParseWebhookPayload(body []byte) (*WebhookPayload, error) {
	var p WebhookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("zohocliq: parse payload: %w", err)
	}
	return &p, nil
}

// NormalizedMessage holds the fields extracted from a Cliq payload,
// ready to be assembled into a mio.v1.Message by the handler.
type NormalizedMessage struct {
	// Conversation
	ConversationExternalID string
	ConversationKind       string // ConversationKind enum string
	ConversationDisplayName string
	ParentExternalID       string // non-empty for THREAD kind

	// Message
	SourceMessageID   string
	SenderExternalID  string
	SenderDisplayName string
	SenderIsBot       bool
	Text              string

	// Channel-specific extras go into attributes.
	Attributes map[string]string

	// Structured attachments (raw url/mime/filename — sidecar persists bytes
	// and rewrites url to a stable signed URL via P9 attachment-downloader).
	// Empty for text-only messages. Kept as a sibling to Attributes so
	// downstream consumers can iterate over typed Attachment without parsing
	// the JSON-encoded `cliq_attachment` attribute.
	Attachments []NormalizedAttachment
}

// NormalizedAttachment is the channel-agnostic attachment metadata extracted
// from the inbound webhook. handler.go converts these to mio.v1.Attachment
// before publishing to MESSAGES_INBOUND.
type NormalizedAttachment struct {
	URL      string
	MIME     string
	Filename string
}

// attachmentKindFromMime maps a MIME type to the structured Kind enum on
// mio.v1.Attachment. Unknown types fall back to KIND_FILE so consumers can
// always render *something*.
func attachmentKindFromMime(mime string) miov1.Attachment_Kind {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return miov1.Attachment_KIND_IMAGE
	case strings.HasPrefix(mime, "audio/"):
		return miov1.Attachment_KIND_AUDIO
	case strings.HasPrefix(mime, "video/"):
		return miov1.Attachment_KIND_VIDEO
	case mime == "":
		return miov1.Attachment_KIND_UNSPECIFIED
	default:
		return miov1.Attachment_KIND_FILE
	}
}

// Normalize maps a Cliq WebhookPayload to a NormalizedMessage.
// Returns an error only if the payload is so malformed we cannot
// extract a source_message_id or conversation_id.
func Normalize(p *WebhookPayload) (*NormalizedMessage, error) {
	nm := &NormalizedMessage{
		Attributes: make(map[string]string),
	}

	// --- Conversation ID ---
	if p.Chat != nil && p.Chat.ID != "" {
		nm.ConversationExternalID = p.Chat.ID
		nm.ConversationDisplayName = p.Chat.Title
	}
	// Stash channel unique_name (Cliq API slug, e.g. "ducdev") so the outbound
	// bot adapter can address /channelsbyname/{slug}/message. Title (e.g.
	// "#DucDev") is a display string the API rejects with request_url_invalid.
	if p.Chat != nil && p.Chat.ChannelUniqueName != "" {
		if nm.Attributes == nil {
			nm.Attributes = map[string]string{}
		}
		nm.Attributes["cliq_channel_name"] = p.Chat.ChannelUniqueName
	}

	// --- ConversationKind (decision tree from FINDINGS.md) ---
	nm.ConversationKind = deriveKind(p)

	// --- Extract the message object (differs between handlers) ---
	msg := effectiveMessage(p)
	if msg == nil {
		return nil, fmt.Errorf("zohocliq: normalize: no message object in payload (operation=%q)", p.Operation)
	}
	if msg.ID == "" {
		return nil, fmt.Errorf("zohocliq: normalize: empty message id (operation=%q)", p.Operation)
	}
	nm.SourceMessageID = msg.ID

	// --- Text ---
	nm.Text = effectiveText(msg)

	// --- Thread parent ref (FINDINGS.md Q3) ---
	if msg.RepliedMessage != nil && msg.RepliedMessage.ID != "" {
		nm.ParentExternalID = msg.RepliedMessage.ID
		nm.Attributes["cliq_replied_message_id"] = msg.RepliedMessage.ID
	}

	// --- Sender (FINDINGS.md Q4) ---
	if p.User != nil && p.User.ID != "" {
		nm.SenderExternalID = p.User.ID
		nm.SenderDisplayName = displayName(p.User)
		nm.SenderIsBot = p.User.IsBot
	} else {
		// Synthesize system sender for events without a user field.
		nm.SenderExternalID = "system"
		nm.SenderIsBot = true
	}

	// --- Attachment → structured + attributes (legacy JSON kept for back-compat) ---
	if msg.File != nil {
		// Structured form for the P9 attachment-downloader sidecar.
		if msg.File.URL != "" {
			nm.Attachments = append(nm.Attachments, NormalizedAttachment{
				URL:      msg.File.URL,
				MIME:     msg.File.Type,
				Filename: msg.File.Name,
			})
		}
		// Raw JSON form preserved so consumers that already parse this
		// attribute keep working until they migrate to typed Attachments.
		attJSON, _ := json.Marshal(msg.File)
		nm.Attributes["cliq_attachment"] = string(attJSON)
	}

	// --- Mentions → attributes ---
	if len(msg.Mentions) > 0 {
		mentJSON, _ := json.Marshal(msg.Mentions)
		nm.Attributes["cliq_mentions"] = string(mentJSON)
	}

	// --- Operation → attributes (for consumers that care) ---
	nm.Attributes["cliq_operation"] = p.Operation

	if nm.ConversationExternalID == "" {
		return nil, fmt.Errorf("zohocliq: normalize: empty conversation external_id")
	}

	return nm, nil
}

// deriveKind applies the locked ConversationKind decision tree from FINDINGS.md.
func deriveKind(p *WebhookPayload) string {
	// DM: Message Handler shape (operation="message", chat.is_dm=true).
	if p.Operation == "message" && p.Chat != nil && p.Chat.IsDM {
		return "CONVERSATION_KIND_DM"
	}

	// Thread: replied_message present.
	msg := effectiveMessage(p)
	if msg != nil && msg.RepliedMessage != nil && msg.RepliedMessage.ID != "" {
		return "CONVERSATION_KIND_THREAD"
	}

	// Channel (public by default — no private signal in captured payloads).
	if p.Chat != nil && (p.Chat.ChatType == "channel" || p.Chat.ChannelUniqueName != "") {
		return "CONVERSATION_KIND_CHANNEL_PUBLIC"
	}

	// Fallback.
	return "CONVERSATION_KIND_UNSPECIFIED"
}

// effectiveMessage returns the message object regardless of which handler shape
// the payload uses (Participation Handler: data.message; Message Handler: message).
func effectiveMessage(p *WebhookPayload) *MessageObj {
	if p.Data != nil && p.Data.Message != nil {
		return p.Data.Message
	}
	return p.Message
}

// effectiveText extracts message text from whichever field is populated.
// Attachment payloads use comment; text payloads use text.
func effectiveText(msg *MessageObj) string {
	if msg.Text != "" {
		return msg.Text
	}
	if msg.Comment != "" {
		return msg.Comment
	}
	return ""
}

// displayName builds a display name from the user object.
func displayName(u *UserObj) string {
	if u.Name != "" {
		return u.Name
	}
	parts := []string{u.FirstName, u.LastName}
	var non []string
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			non = append(non, s)
		}
	}
	if len(non) > 0 {
		return strings.Join(non, " ")
	}
	return u.ID
}
