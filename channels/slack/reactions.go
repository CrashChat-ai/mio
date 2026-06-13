package slack

import (
	"context"
	"fmt"
	"strings"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	slackapi "github.com/slack-go/slack"
)

// addReaction applies (or removes) an emoji reaction on the message the
// relation targets. emoji name is taken from reaction_emoji with any wrapping
// colons stripped (slack-go wants the bare name). target_external_id is the
// composite(channel, ts) of the reacted message; the relation's channel must
// match the command's conversation for a coherent ItemRef.
//
// Removal is signalled by the slack_reaction_removed="true" attr, mirroring the
// inbound reaction model (proto frozen — no KIND_REACTION_REMOVED enum).
func (a *Adapter) reactTo(ctx context.Context, api *slackapi.Client, channel string, rel *miov1.MessageRelation, removed bool) error {
	emoji := strings.Trim(rel.GetReactionEmoji(), ":")
	if emoji == "" {
		return fmt.Errorf("slack reaction: reaction_emoji is required")
	}

	ts := ""
	if _, t, ok := splitComposite(rel.GetTargetExternalId()); ok {
		ts = t
	}
	if ts == "" {
		return fmt.Errorf("slack reaction: relation.target_external_id %q is not a composite channel:ts id", rel.GetTargetExternalId())
	}

	item := slackapi.ItemRef{Channel: channel, Timestamp: ts}

	var err error
	if removed {
		err = api.RemoveReactionContext(ctx, emoji, item)
	} else {
		err = api.AddReactionContext(ctx, emoji, item)
	}
	if err != nil {
		return classifyDeliveryError(err)
	}
	return nil
}
