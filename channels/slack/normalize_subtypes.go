package slack

import (
	"fmt"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// normalizeMessage handles the "message" event and its subtypes.
func normalizeMessage(e *Envelope) (*miov1.Message, error) {
	ev := e.Event
	switch ev.SubType {
	case "message_changed":
		return normalizeEdit(e)
	case "message_deleted":
		return normalizeDelete(e)
	case "", "file_share", "thread_broadcast":
		return normalizePlain(e)
	default:
		// bot_message, joins, topic/purpose changes, message_replied, etc.
		return nil, fmt.Errorf("%w: slack: dropped message subtype %q", channels.ErrNormalizeSoft, ev.SubType)
	}
}

func normalizePlain(e *Envelope) (*miov1.Message, error) {
	ev := e.Event
	if ev.BotID != "" {
		return nil, fmt.Errorf("%w: slack: bot echo (bot_id=%s)", channels.ErrNormalizeSoft, ev.BotID)
	}
	if ev.TS == "" || ev.Channel == "" {
		return nil, fmt.Errorf("%w: slack: missing ts/channel", channels.ErrNormalizeSoft)
	}

	msg := baseMessage(e, ev.Channel, ev.ChannelType)
	msg.SourceMessageId = composite(ev.Channel, ev.TS)
	msg.Sender = &miov1.Sender{ExternalId: ev.User}
	msg.Text = ev.Text
	msg.Attributes[attrSlackTS] = ev.TS
	msg.Attachments = attachmentsFromFiles(ev.Files)

	if ev.SubType == "thread_broadcast" {
		msg.Attributes[attrSlackThreadBroadcast] = "true"
	}
	applyThread(msg, ev.Channel, ev.ThreadTS, ev.TS)
	return msg, nil
}

func normalizeEdit(e *Envelope) (*miov1.Message, error) {
	ev := e.Event
	if ev.Message == nil {
		return nil, fmt.Errorf("%w: slack: message_changed without nested message", channels.ErrNormalizeSoft)
	}
	nested := ev.Message
	if nested.BotID != "" {
		return nil, fmt.Errorf("%w: slack: bot echo edit", channels.ErrNormalizeSoft)
	}
	// Unfurl-only updates re-fire message_changed with unchanged text — drop them.
	if ev.PreviousMessage != nil && ev.PreviousMessage.Text == nested.Text {
		return nil, fmt.Errorf("%w: slack: unfurl-only edit (no text change)", channels.ErrNormalizeSoft)
	}

	target := composite(ev.Channel, nested.TS)
	msg := baseMessage(e, ev.Channel, ev.ChannelType)
	msg.SourceMessageId = target
	msg.Sender = &miov1.Sender{ExternalId: nested.User}
	msg.Text = nested.Text
	msg.Attributes[attrSlackTS] = nested.TS
	msg.Attachments = attachmentsFromFiles(nested.Files)
	msg.Relation = &miov1.MessageRelation{
		Kind:             miov1.MessageRelation_KIND_EDIT,
		TargetExternalId: target,
	}
	applyThread(msg, ev.Channel, nested.ThreadTS, nested.TS)
	return msg, nil
}

func normalizeDelete(e *Envelope) (*miov1.Message, error) {
	ev := e.Event
	if ev.DeletedTS == "" {
		return nil, fmt.Errorf("%w: slack: message_deleted without deleted_ts", channels.ErrNormalizeSoft)
	}
	target := composite(ev.Channel, ev.DeletedTS)
	msg := baseMessage(e, ev.Channel, ev.ChannelType)
	msg.SourceMessageId = target
	msg.Sender = &miov1.Sender{ExternalId: ev.User, IsBot: true}
	msg.Attributes[attrSlackTS] = ev.DeletedTS
	msg.Relation = &miov1.MessageRelation{
		Kind:             miov1.MessageRelation_KIND_DELETE,
		TargetExternalId: target,
	}
	return msg, nil
}

func normalizeReaction(e *Envelope) (*miov1.Message, error) {
	ev := e.Event
	if ev.Item == nil || ev.Item.TS == "" {
		return nil, fmt.Errorf("%w: slack: reaction without item", channels.ErrNormalizeSoft)
	}
	channel := ev.Item.Channel
	target := composite(channel, ev.Item.TS)
	msg := baseMessage(e, channel, ev.ChannelType)
	msg.SourceMessageId = composite(channel, ev.TS)
	msg.Sender = &miov1.Sender{ExternalId: ev.User}
	msg.Attributes[attrSlackTS] = ev.TS
	msg.Relation = &miov1.MessageRelation{
		Kind:             miov1.MessageRelation_KIND_REACTION,
		TargetExternalId: target,
		ReactionEmoji:    ev.Reaction,
	}
	if ev.Type == "reaction_removed" {
		msg.Attributes[attrSlackReactionRemoved] = "true"
	}
	return msg, nil
}

// applyThread stamps reply fields when threadTS is a real parent (≠ the
// message's own ts), mirroring the Cliq flat-capture shape: KIND_REPLY with
// target == thread-root composite.
func applyThread(msg *miov1.Message, channel, threadTS, selfTS string) {
	if threadTS == "" || threadTS == selfTS {
		return
	}
	root := composite(channel, threadTS)
	msg.ThreadRootMessageId = root
	msg.ParentConversationId = root
	msg.Relation = &miov1.MessageRelation{
		Kind:             miov1.MessageRelation_KIND_REPLY,
		TargetExternalId: root,
	}
}
