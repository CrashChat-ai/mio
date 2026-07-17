package discord

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Envelope is the wire shape the discordrunner feeds Normalize: the gateway
// dispatch event name plus its raw data payload. The runner builds it from
// discordgo's raw *Event so this package parses plain JSON and stays
// fixture-testable without depending on discordgo types.
type Envelope struct {
	T string          `json:"t"`
	D json.RawMessage `json:"d"`
}

// eventBody is the union of the message + reaction dispatch payloads.
type eventBody struct {
	ID        string  `json:"id"`
	ChannelID string  `json:"channel_id"`
	GuildID   string  `json:"guild_id"`
	Author    *author `json:"author"`
	Content   string  `json:"content"`
	Timestamp string  `json:"timestamp"`

	MessageReference *messageReference `json:"message_reference"`
	Attachments      []attachment      `json:"attachments"`

	// Reaction events (MESSAGE_REACTION_ADD / _REMOVE)
	UserID    string `json:"user_id"`
	MessageID string `json:"message_id"`
	Emoji     *emoji `json:"emoji"`
}

type author struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"global_name"`
	Bot        bool   `json:"bot"`
}

type messageReference struct {
	MessageID string `json:"message_id"`
	ChannelID string `json:"channel_id"`
}

type attachment struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	URL         string `json:"url"`
}

type emoji struct {
	Name string `json:"name"`
}

// ParseEnvelope unmarshals raw runner payload bytes into an Envelope.
func ParseEnvelope(body []byte) (*Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(body, &e); err != nil {
		return nil, fmt.Errorf("discord: parse envelope: %w", err)
	}
	return &e, nil
}

// Normalize maps a Discord gateway dispatch Envelope to a canonical
// mio.v1.Message. Id / TenantId / AccountId are left empty — the gateway
// pipeline fills them. Pure: no API calls.
//
// Soft drops (bot echo, redundant/noise events) wrap ErrNormalizeSoft so the
// caller acks without retry.
func Normalize(e *Envelope) (*miov1.Message, error) {
	var ev eventBody
	if err := json.Unmarshal(e.D, &ev); err != nil {
		return nil, fmt.Errorf("discord: parse %s: %w", e.T, err)
	}
	switch e.T {
	case "MESSAGE_CREATE":
		return normalizeCreate(&ev)
	case "MESSAGE_UPDATE":
		return normalizeUpdate(&ev)
	case "MESSAGE_DELETE":
		return normalizeDelete(&ev)
	case "MESSAGE_REACTION_ADD", "MESSAGE_REACTION_REMOVE":
		return normalizeReaction(e.T, &ev)
	default:
		return nil, fmt.Errorf("%w: discord: unhandled event type %q", channels.ErrNormalizeSoft, e.T)
	}
}

// baseMessage seeds the common envelope-level fields shared by every event
// variant. Guild presence is the only signal in the dispatch payload:
// guild_id set → guild text channel (public); absent → DM. Private-channel
// and thread distinctions need channel-object lookups (API calls) — deferred.
func baseMessage(ev *eventBody) *miov1.Message {
	attrs := map[string]string{
		attrDiscordChannelID: ev.ChannelID,
	}
	if ev.GuildID != "" {
		attrs[attrDiscordGuildID] = ev.GuildID
	}
	kind := miov1.ConversationKind_CONVERSATION_KIND_DM
	if ev.GuildID != "" {
		kind = miov1.ConversationKind_CONVERSATION_KIND_CHANNEL_PUBLIC
	}
	return &miov1.Message{
		SchemaVersion:          1,
		ChannelType:            channelType,
		ConversationExternalId: ev.ChannelID,
		ConversationKind:       kind,
		ReceivedAt:             receivedAt(ev.Timestamp),
		Attributes:             attrs,
	}
}

// receivedAt parses the ISO8601 event timestamp, falling back to now — the
// gateway pipeline treats ReceivedAt as required.
func receivedAt(ts string) *timestamppb.Timestamp {
	if ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			return timestamppb.New(t)
		}
	}
	return timestamppb.Now()
}

func normalizeCreate(ev *eventBody) (*miov1.Message, error) {
	if ev.Author != nil && ev.Author.Bot {
		return nil, fmt.Errorf("%w: discord: bot echo (author=%s)", channels.ErrNormalizeSoft, ev.Author.ID)
	}
	if ev.ID == "" || ev.ChannelID == "" {
		return nil, fmt.Errorf("%w: discord: missing id/channel_id", channels.ErrNormalizeSoft)
	}

	msg := baseMessage(ev)
	msg.SourceMessageId = composite(ev.ChannelID, ev.ID)
	msg.Sender = senderOf(ev.Author)
	msg.Text = ev.Content
	msg.Attributes[attrDiscordMessageID] = ev.ID
	msg.Attachments = attachmentsOf(ev.Attachments)

	if ref := ev.MessageReference; ref != nil && ref.MessageID != "" {
		refChannel := ref.ChannelID
		if refChannel == "" {
			refChannel = ev.ChannelID
		}
		target := composite(refChannel, ref.MessageID)
		msg.ThreadRootMessageId = target
		msg.ParentConversationId = target
		msg.Relation = &miov1.MessageRelation{
			Kind:             miov1.MessageRelation_KIND_REPLY,
			TargetExternalId: target,
		}
	}
	return msg, nil
}

func normalizeUpdate(ev *eventBody) (*miov1.Message, error) {
	if ev.Author != nil && ev.Author.Bot {
		return nil, fmt.Errorf("%w: discord: bot echo edit", channels.ErrNormalizeSoft)
	}
	// Embed/link-unfurl updates re-fire MESSAGE_UPDATE with no author and no
	// content — drop them (mirror of slack's unfurl-only edit drop).
	if ev.Author == nil || ev.Author.ID == "" {
		return nil, fmt.Errorf("%w: discord: authorless update (embed unfurl)", channels.ErrNormalizeSoft)
	}
	if ev.ID == "" || ev.ChannelID == "" {
		return nil, fmt.Errorf("%w: discord: missing id/channel_id", channels.ErrNormalizeSoft)
	}

	target := composite(ev.ChannelID, ev.ID)
	msg := baseMessage(ev)
	msg.SourceMessageId = target
	msg.Sender = senderOf(ev.Author)
	msg.Text = ev.Content
	msg.Attributes[attrDiscordMessageID] = ev.ID
	msg.Attachments = attachmentsOf(ev.Attachments)
	msg.Relation = &miov1.MessageRelation{
		Kind:             miov1.MessageRelation_KIND_EDIT,
		TargetExternalId: target,
	}
	return msg, nil
}

// normalizeDelete: MESSAGE_DELETE carries only {id, channel_id, guild_id} —
// no author. Keep the delete (the relation still removes the target) with an
// empty sender; never invent an id, never flag an unknown author as a bot
// (downstream contact rosters overwrite is_bot from the first message seen).
func normalizeDelete(ev *eventBody) (*miov1.Message, error) {
	if ev.ID == "" || ev.ChannelID == "" {
		return nil, fmt.Errorf("%w: discord: message_delete missing id/channel_id", channels.ErrNormalizeSoft)
	}
	target := composite(ev.ChannelID, ev.ID)
	msg := baseMessage(ev)
	msg.SourceMessageId = target
	msg.Sender = &miov1.Sender{}
	msg.Attributes[attrDiscordMessageID] = ev.ID
	msg.Relation = &miov1.MessageRelation{
		Kind:             miov1.MessageRelation_KIND_DELETE,
		TargetExternalId: target,
	}
	return msg, nil
}

func normalizeReaction(t string, ev *eventBody) (*miov1.Message, error) {
	if ev.MessageID == "" || ev.ChannelID == "" {
		return nil, fmt.Errorf("%w: discord: reaction without message_id/channel_id", channels.ErrNormalizeSoft)
	}
	if ev.Emoji == nil || ev.Emoji.Name == "" {
		return nil, fmt.Errorf("%w: discord: reaction without emoji", channels.ErrNormalizeSoft)
	}
	target := composite(ev.ChannelID, ev.MessageID)
	msg := baseMessage(ev)
	// Reaction events have no message id of their own; synthesize a stable
	// source id from the (message, user, emoji, op) tuple for dedup.
	op := "add"
	if t == "MESSAGE_REACTION_REMOVE" {
		op = "remove"
	}
	msg.SourceMessageId = target + ":" + ev.UserID + ":" + ev.Emoji.Name + ":" + op
	msg.Sender = &miov1.Sender{ExternalId: ev.UserID}
	msg.Relation = &miov1.MessageRelation{
		Kind:             miov1.MessageRelation_KIND_REACTION,
		TargetExternalId: target,
		ReactionEmoji:    ev.Emoji.Name,
	}
	if op == "remove" {
		msg.Attributes[attrDiscordReactionRemoved] = "true"
	}
	return msg, nil
}

func senderOf(a *author) *miov1.Sender {
	if a == nil {
		return &miov1.Sender{}
	}
	name := a.GlobalName
	if name == "" {
		name = a.Username
	}
	return &miov1.Sender{ExternalId: a.ID, DisplayName: name, IsBot: a.Bot}
}

func attachmentsOf(atts []attachment) []*miov1.Attachment {
	var out []*miov1.Attachment
	for _, a := range atts {
		out = append(out, &miov1.Attachment{
			Kind:     attachmentKindFromMime(a.ContentType),
			Url:      a.URL,
			Mime:     a.ContentType,
			Filename: a.Filename,
			Bytes:    a.Size,
		})
	}
	return out
}
