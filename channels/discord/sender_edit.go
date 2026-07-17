package discord

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// Edit updates an existing Discord message in-place via
// PATCH /channels/{channel}/messages/{id}.
//
// cmd.EditOfExternalId is the composite(channel, message_id) that Send
// returned and the pool stored in outbound_state. The PATCH needs BOTH parts,
// so a bare/legacy id that can't be split is a hard error rather than a
// silent wrong-target update.
func (a *Adapter) Edit(ctx context.Context, cmd *miov1.SendCommand) error {
	if a.botToken == "" {
		return fmt.Errorf("discord edit: DISCORD_BOT_TOKEN env unset")
	}

	channel, msgID, ok := splitComposite(cmd.GetEditOfExternalId())
	if !ok {
		return fmt.Errorf("discord edit: edit_of_external_id %q is not a composite channel:message id", cmd.GetEditOfExternalId())
	}

	s, err := newDiscordSession(a.botToken)
	if err != nil {
		return fmt.Errorf("discord edit: session: %w", err)
	}
	if _, err := s.ChannelMessageEdit(channel, msgID, cmd.GetText(), discordgo.WithContext(ctx)); err != nil {
		return classifyDeliveryError(err)
	}
	return nil
}
