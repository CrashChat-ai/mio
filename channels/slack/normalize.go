package slack

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Envelope is the Slack event_callback wrapper — byte-identical between the
// Socket Mode payload and the Events API POST body, so Normalize is written
// once and serves both transports.
type Envelope struct {
	Type         string    `json:"type"`
	TeamID       string    `json:"team_id"`
	EnterpriseID string    `json:"enterprise_id"`
	EventID      string    `json:"event_id"`
	Event        EventBody `json:"event"`
}

// EventBody is the union of message + reaction inner events. Pointer/optional
// fields distinguish subtypes (message_changed nests Message; reaction events
// carry Item).
type EventBody struct {
	Type        string `json:"type"`
	SubType     string `json:"subtype"`
	Channel     string `json:"channel"`
	ChannelType string `json:"channel_type"`
	User        string `json:"user"`
	Text        string `json:"text"`
	TS          string `json:"ts"`
	ThreadTS    string `json:"thread_ts"`
	BotID       string `json:"bot_id"`
	Files       []File `json:"files"`

	Message         *NestedMessage `json:"message,omitempty"`
	PreviousMessage *NestedMessage `json:"previous_message,omitempty"`
	DeletedTS       string         `json:"deleted_ts,omitempty"`

	Reaction string        `json:"reaction,omitempty"`
	Item     *ReactionItem `json:"item,omitempty"`
}

// NestedMessage is the edited/previous message carried inside message_changed
// (and the parent inside message_deleted on some shapes).
type NestedMessage struct {
	Type     string `json:"type"`
	SubType  string `json:"subtype"`
	User     string `json:"user"`
	Text     string `json:"text"`
	TS       string `json:"ts"`
	ThreadTS string `json:"thread_ts"`
	BotID    string `json:"bot_id"`
	Files    []File `json:"files"`
}

// ReactionItem locates the message a reaction targets.
type ReactionItem struct {
	Type    string `json:"type"`
	Channel string `json:"channel"`
	TS      string `json:"ts"`
}

// File is a Slack file_share attachment.
type File struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Mimetype           string `json:"mimetype"`
	Size               int64  `json:"size"`
	URLPrivate         string `json:"url_private"`
	URLPrivateDownload string `json:"url_private_download"`
}

// ParseEnvelope unmarshals raw payload bytes into an Envelope.
func ParseEnvelope(body []byte) (*Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(body, &e); err != nil {
		return nil, fmt.Errorf("slack: parse envelope: %w", err)
	}
	return &e, nil
}

// Normalize maps a Slack event_callback Envelope to a canonical mio.v1.Message.
// Id / TenantId / AccountId are left empty — the gateway pipeline fills them.
// Pure: no API calls (display-name + thread-parent enrichment removed per brief).
//
// Soft drops (echo loops, redundant/noise events) wrap ErrNormalizeSoft so the
// caller acks without retry.
func Normalize(e *Envelope) (*miov1.Message, error) {
	switch e.Event.Type {
	case "message":
		return normalizeMessage(e)
	case "reaction_added", "reaction_removed":
		return normalizeReaction(e)
	default:
		return nil, fmt.Errorf("%w: slack: unhandled event type %q", channels.ErrNormalizeSoft, e.Event.Type)
	}
}

// baseMessage seeds the common envelope-level fields shared by every event
// variant. slackChannelType is the Slack channel_type (channel/group/im/mpim),
// distinct from the adapter's ChannelType slug ("slack").
func baseMessage(e *Envelope, channel, slackChannelType string) *miov1.Message {
	attrs := map[string]string{
		attrSlackChannel: channel,
	}
	if e.TeamID != "" {
		attrs[attrSlackTeamID] = e.TeamID
	}
	if e.EnterpriseID != "" {
		attrs[attrSlackEnterpriseID] = e.EnterpriseID
	}
	if e.EventID != "" {
		attrs[attrSlackEventID] = e.EventID
	}
	return &miov1.Message{
		SchemaVersion:          1,
		ChannelType:            channelType,
		ConversationExternalId: channel,
		ConversationKind:       conversationKind(slackChannelType),
		ReceivedAt:             timestamppb.Now(),
		Attributes:             attrs,
	}
}

// conversationKind maps Slack channel_type to the mio ConversationKind. mpim →
// GROUP_DM (locked): keeps small group chats distinct from private channels.
func conversationKind(slackChannelType string) miov1.ConversationKind {
	switch slackChannelType {
	case "channel":
		return miov1.ConversationKind_CONVERSATION_KIND_CHANNEL_PUBLIC
	case "group":
		return miov1.ConversationKind_CONVERSATION_KIND_CHANNEL_PRIVATE
	case "im":
		return miov1.ConversationKind_CONVERSATION_KIND_DM
	case "mpim":
		return miov1.ConversationKind_CONVERSATION_KIND_GROUP_DM
	default:
		return miov1.ConversationKind_CONVERSATION_KIND_UNSPECIFIED
	}
}

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

func attachmentsFromFiles(files []File) []*miov1.Attachment {
	var out []*miov1.Attachment
	for _, f := range files {
		url := f.URLPrivateDownload
		if url == "" {
			url = f.URLPrivate
		}
		out = append(out, &miov1.Attachment{
			Kind:     attachmentKindFromMime(f.Mimetype),
			Url:      url,
			Mime:     f.Mimetype,
			Filename: f.Name,
			Bytes:    f.Size,
		})
	}
	return out
}
