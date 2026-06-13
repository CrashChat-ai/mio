package slack

import (
	"context"
	"errors"
	"fmt"

	"github.com/crashchat-ai/mio/pkg/channels"
	slackapi "github.com/slack-go/slack"
)

const (
	slackHistoryReadScope = "channels:history"
	slackHistoryMaxLimit  = 999
)

// scopeMissingErrors marks Slack API error strings that mean the credential
// cannot read this conversation's history — surfaced as ScopeMissingError so the
// reconciler tells the operator to re-consent rather than retrying forever.
var scopeMissingErrors = map[string]bool{
	"missing_scope":     true,
	"not_in_channel":    true,
	"channel_not_found": true,
	"not_authed":        true,
	"invalid_auth":      true,
	"token_revoked":     true,
	"account_inactive":  true,
}

// FetchHistory implements channels.HistoryAdapter for Slack. One call pulls a
// single conversations.history page for req.Conversation.ExternalID, then
// expands threaded replies (reply_count>0) via conversations.replies. Every row
// carries the same composite(channel, ts) SourceMessageID the socket path emits
// so reconciled rows dedup against live-captured rows on (account_id, ...id).
func (a *Adapter) FetchHistory(ctx context.Context, req channels.HistoryRequest) (channels.HistoryPage, error) {
	token := req.Credential.AccessToken
	if token == "" {
		token = a.botToken
	}
	if token == "" {
		return channels.HistoryPage{}, fmt.Errorf("slack history: no bot token (credential empty, SLACK_BOT_TOKEN unset)")
	}
	channel := req.Conversation.ExternalID
	if channel == "" {
		return channels.HistoryPage{}, fmt.Errorf("slack history: conversation external_id is required")
	}

	limit := req.Limit
	if limit <= 0 || limit > slackHistoryMaxLimit {
		limit = slackHistoryMaxLimit
	}
	api := newSlackClient(token)

	resp, err := api.GetConversationHistoryContext(ctx, &slackapi.GetConversationHistoryParameters{
		ChannelID: channel,
		Cursor:    req.Cursor,
		Oldest:    historyOldest(req),
		Latest:    historyLatest(req),
		Limit:     limit,
	})
	if err != nil {
		return channels.HistoryPage{}, classifyHistoryError(err)
	}

	out := channels.HistoryPage{
		Messages:   make([]channels.HistoryMessage, 0, len(resp.Messages)),
		NextCursor: resp.ResponseMetaData.NextCursor,
	}
	for i := range resp.Messages {
		msg := resp.Messages[i]
		hm := a.toHistoryMessage(req.Conversation, channel, &msg.Msg)
		if hm.SourceMessageID == "" {
			continue
		}
		out.Messages = append(out.Messages, hm)

		if msg.ReplyCount > 0 {
			replies, err := a.fetchReplies(ctx, api, req.Conversation, channel, msg.Timestamp)
			if err != nil {
				return channels.HistoryPage{}, err
			}
			out.Messages = append(out.Messages, replies...)
		}
	}
	return out, nil
}

var _ channels.HistoryAdapter = (*Adapter)(nil)

// classifyHistoryError maps a slack-go error onto ScopeMissingError (re-consent),
// a Retry-After-bearing DeliveryError (429), or a plain wrapped error.
func classifyHistoryError(err error) error {
	var rle *slackapi.RateLimitedError
	if errors.As(err, &rle) {
		secs := int(rle.RetryAfter.Seconds())
		if secs < 1 {
			secs = 1
		}
		return &DeliveryError{err: err, rateLimited: true, retryAfterSecs: secs}
	}
	if scopeMissingErrors[err.Error()] {
		return &channels.ScopeMissingError{
			ChannelType: channelType,
			Scope:       slackHistoryReadScope,
			Err:         err,
		}
	}
	return fmt.Errorf("slack history: %w", err)
}

func historyOldest(req channels.HistoryRequest) string {
	if !req.Since.IsZero() {
		return tsFromTime(req.Since)
	}
	return ""
}

func historyLatest(req channels.HistoryRequest) string {
	if !req.Until.IsZero() {
		return tsFromTime(req.Until)
	}
	return ""
}
