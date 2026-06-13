package slack

import (
	"context"
	"fmt"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	slackapi "github.com/slack-go/slack"
)

// attrSlackThreadTS lets a producer pin a reply to a thread by raw ts directly,
// bypassing the composite split. Other slack_* attrs live in attrs.go (P1).
const attrSlackThreadTS = "slack_thread_ts"

// testAPIURL, when non-empty, points the slack-go client at a fake server.
// Set only by tests (sender_test.go); production leaves it "" so the client
// hits the real https://slack.com/api/ endpoint.
var testAPIURL string

// newSlackClient builds a bot-token slack-go client, honouring testAPIURL.
func newSlackClient(token string) *slackapi.Client {
	if testAPIURL != "" {
		return slackapi.New(token, slackapi.OptionAPIURL(testAPIURL))
	}
	return slackapi.New(token)
}

// Send delivers a new outbound message to Slack via chat.postMessage and
// returns composite(channel, ts) so the sender pool can feed it back to Edit.
//
// A KIND_REACTION relation routes to reactions.add instead of posting a message:
// reactions emit no new ts, so Send returns "" and the pool stores nothing.
func (a *Adapter) Send(ctx context.Context, cmd *miov1.SendCommand) (string, error) {
	if a.botToken == "" {
		return "", fmt.Errorf("slack send: SLACK_BOT_TOKEN env unset")
	}
	channel := cmd.GetConversationExternalId()
	if channel == "" {
		return "", fmt.Errorf("slack send: conversation_external_id is required")
	}

	api := newSlackClient(a.botToken)

	if rel := cmd.GetRelation(); rel.GetKind() == miov1.MessageRelation_KIND_REACTION {
		removed := cmd.GetAttributes()[attrSlackReactionRemoved] == "true"
		if err := a.reactTo(ctx, api, channel, rel, removed); err != nil {
			return "", err
		}
		return "", nil
	}

	opts := []slackapi.MsgOption{slackapi.MsgOptionText(markdownToSlackMrkdwn(cmd.GetText()), false)}
	if ts := threadTS(cmd); ts != "" {
		opts = append(opts, slackapi.MsgOptionTS(ts))
		if threadBroadcast(cmd) {
			opts = append(opts, slackapi.MsgOptionBroadcast())
		}
	}

	respChannel, ts, err := api.PostMessageContext(ctx, channel, opts...)
	if err != nil {
		return "", classifyDeliveryError(err)
	}
	if respChannel == "" {
		respChannel = channel
	}
	return composite(respChannel, ts), nil
}

// threadTS resolves the Slack thread_ts for a reply. Precedence: explicit
// slack_thread_ts attr (raw ts the producer set) > thread_root_message_id /
// KIND_REPLY relation target (both composite, split to the bare ts).
func threadTS(cmd *miov1.SendCommand) string {
	if attrs := cmd.GetAttributes(); attrs != nil {
		if ts := attrs[attrSlackThreadTS]; ts != "" {
			return ts
		}
	}
	if root := cmd.GetThreadRootMessageId(); root != "" {
		if _, ts, ok := splitComposite(root); ok {
			return ts
		}
		return root
	}
	if rel := cmd.GetRelation(); rel.GetKind() == miov1.MessageRelation_KIND_REPLY {
		if _, ts, ok := splitComposite(rel.GetTargetExternalId()); ok {
			return ts
		}
	}
	return ""
}

// threadBroadcast reports whether a threaded reply should also post to the
// channel (reply_broadcast), driven by the slack_thread_broadcast attr.
func threadBroadcast(cmd *miov1.SendCommand) bool {
	if attrs := cmd.GetAttributes(); attrs != nil {
		return attrs[attrSlackThreadBroadcast] == "true"
	}
	return false
}

// ChannelType, MaxDeliver, RateLimitKey live in adapter.go (P1).
