package slack

import (
	"context"
	"fmt"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	slackapi "github.com/slack-go/slack"
)

// Edit updates an existing Slack message in-place via chat.update.
//
// cmd.EditOfExternalId is the composite(channel, ts) that Send returned and the
// pool stored in outbound_state. chat.update needs BOTH channel and ts, so a
// bare/legacy id that can't be split is a hard error rather than a silent
// wrong-target update.
func (a *Adapter) Edit(ctx context.Context, cmd *miov1.SendCommand) error {
	if a.botToken == "" {
		return fmt.Errorf("slack edit: SLACK_BOT_TOKEN env unset")
	}

	channel, ts, ok := splitComposite(cmd.GetEditOfExternalId())
	if !ok {
		return fmt.Errorf("slack edit: edit_of_external_id %q is not a composite channel:ts id", cmd.GetEditOfExternalId())
	}

	api := newSlackClient(a.botToken)
	_, _, _, err := api.UpdateMessageContext(ctx, channel, ts,
		slackapi.MsgOptionText(markdownToSlackMrkdwn(cmd.GetText()), false))
	if err != nil {
		return classifyDeliveryError(err)
	}
	return nil
}
