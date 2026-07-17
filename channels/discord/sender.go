package discord

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// newDiscordSession builds a REST-only discordgo session (no gateway WS is
// opened for sends — discordgo performs plain HTTP until Open() is called).
func newDiscordSession(token string) (*discordgo.Session, error) {
	return discordgo.New("Bot " + token)
}

// Send delivers a new outbound message via POST /channels/{id}/messages and
// returns composite(channel, message_id) so the sender pool can feed it back
// to Edit. Discord messages are native markdown — cmd.text passes through
// unformatted (mirrors what downstream producers already send).
//
// A KIND_REACTION relation routes to the reactions endpoint instead of
// posting a message: reactions mint no new message id, so Send returns ""
// and the pool stores nothing.
func (a *Adapter) Send(ctx context.Context, cmd *miov1.SendCommand) (string, error) {
	if a.botToken == "" {
		return "", fmt.Errorf("discord send: DISCORD_BOT_TOKEN env unset")
	}
	channel := cmd.GetConversationExternalId()
	if channel == "" {
		return "", fmt.Errorf("discord send: conversation_external_id is required")
	}

	s, err := newDiscordSession(a.botToken)
	if err != nil {
		return "", fmt.Errorf("discord send: session: %w", err)
	}

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
	respChannel := m.ChannelID
	if respChannel == "" {
		respChannel = channel
	}
	return composite(respChannel, m.ID), nil
}

// replyReference resolves the Discord message_reference for a reply.
// Precedence: explicit discord_reply_to attr (raw message id) >
// thread_root_message_id / KIND_REPLY relation target (both composite,
// split to the bare message id).
func replyReference(cmd *miov1.SendCommand, channel string) *discordgo.MessageReference {
	if attrs := cmd.GetAttributes(); attrs != nil {
		if id := attrs[attrDiscordReplyTo]; id != "" {
			return &discordgo.MessageReference{MessageID: id, ChannelID: channel}
		}
	}
	if root := cmd.GetThreadRootMessageId(); root != "" {
		if ch, id, ok := splitComposite(root); ok {
			return &discordgo.MessageReference{MessageID: id, ChannelID: ch}
		}
	}
	if rel := cmd.GetRelation(); rel.GetKind() == miov1.MessageRelation_KIND_REPLY {
		if ch, id, ok := splitComposite(rel.GetTargetExternalId()); ok {
			return &discordgo.MessageReference{MessageID: id, ChannelID: ch}
		}
	}
	return nil
}
