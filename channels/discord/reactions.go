package discord

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// reactTo applies (or removes) an emoji reaction on the message the relation
// targets. reaction_emoji carries the unicode emoji or a custom name:id pair;
// wrapping colons are stripped. target_external_id is the composite
// (channel, message_id) of the reacted message.
//
// Removal is signalled by the discord_reaction_removed="true" attr, mirroring
// the inbound reaction model (proto frozen — no KIND_REACTION_REMOVED enum).
func (a *Adapter) reactTo(ctx context.Context, s *discordgo.Session, channel string, rel *miov1.MessageRelation, removed bool) error {
	emoji := strings.Trim(rel.GetReactionEmoji(), ":")
	if emoji == "" {
		return fmt.Errorf("discord reaction: reaction_emoji is required")
	}

	msgID := ""
	if _, id, ok := splitComposite(rel.GetTargetExternalId()); ok {
		msgID = id
	}
	if msgID == "" {
		return fmt.Errorf("discord reaction: relation.target_external_id %q is not a composite channel:message id", rel.GetTargetExternalId())
	}

	var err error
	if removed {
		err = s.MessageReactionRemove(channel, msgID, emoji, "@me", discordgo.WithContext(ctx))
	} else {
		err = s.MessageReactionAdd(channel, msgID, emoji, discordgo.WithContext(ctx))
	}
	if err != nil {
		return classifyDeliveryError(err)
	}
	return nil
}
