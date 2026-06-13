package slack

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/crashchat-ai/mio/pkg/channels"
	slackapi "github.com/slack-go/slack"
)

// fetchReplies pulls conversations.replies for a thread parent and returns the
// replies (excluding the parent itself, which history already yielded). The
// parent ts is composite-linked via ParentExternalID on each reply.
func (a *Adapter) fetchReplies(ctx context.Context, api *slackapi.Client, conv channels.HistoryConversation, channel, parentTS string) ([]channels.HistoryMessage, error) {
	var out []channels.HistoryMessage
	cursor := ""
	for {
		msgs, hasMore, next, err := api.GetConversationRepliesContext(ctx, &slackapi.GetConversationRepliesParameters{
			ChannelID: channel,
			Timestamp: parentTS,
			Cursor:    cursor,
			Limit:     slackHistoryMaxLimit,
		})
		if err != nil {
			return nil, classifyHistoryError(err)
		}
		for i := range msgs {
			m := msgs[i]
			if m.Timestamp == parentTS {
				continue
			}
			hm := a.toHistoryMessage(conv, channel, &m.Msg)
			if hm.SourceMessageID == "" {
				continue
			}
			out = append(out, hm)
		}
		if !hasMore || next == "" {
			break
		}
		cursor = next
	}
	return out, nil
}

// toHistoryMessage normalizes a slack-go Msg to a channels.HistoryMessage with
// the composite SourceMessageID/ParentExternalID invariant. ThreadTimestamp set
// and != ts marks a reply: parent = composite(channel, thread_ts).
func (a *Adapter) toHistoryMessage(conv channels.HistoryConversation, channel string, m *slackapi.Msg) channels.HistoryMessage {
	attrs := map[string]string{
		attrSlackChannel: channel,
		attrSlackTS:      m.Timestamp,
	}
	if m.SubType != "" {
		attrs["slack_subtype"] = m.SubType
	}
	if m.ThreadTimestamp != "" {
		attrs["slack_thread_ts"] = m.ThreadTimestamp
	}

	hm := channels.HistoryMessage{
		SourceMessageID:  composite(channel, m.Timestamp),
		SenderExternalID: historySenderID(m),
		SenderIsBot:      m.BotID != "",
		Text:             m.Text,
		SentAt:           tsToTime(m.Timestamp),
		Attributes:       attrs,
		Attachments:      attachmentsFromFiles(historyFiles(m.Files)),
	}
	if m.Username != "" {
		hm.SenderDisplayName = m.Username
	}
	if m.ThreadTimestamp != "" && m.ThreadTimestamp != m.Timestamp {
		hm.ParentExternalID = composite(channel, m.ThreadTimestamp)
	}
	return hm
}

func historySenderID(m *slackapi.Msg) string {
	if m.User != "" {
		return m.User
	}
	return m.BotID
}

// historyFiles maps slack-go File rows onto the channels.File shape Normalize's
// attachmentsFromFiles consumes (shared with the socket path).
func historyFiles(files []slackapi.File) []File {
	if len(files) == 0 {
		return nil
	}
	out := make([]File, 0, len(files))
	for _, f := range files {
		out = append(out, File{
			ID:                 f.ID,
			Name:               f.Name,
			Mimetype:           f.Mimetype,
			Size:               int64(f.Size),
			URLPrivate:         f.URLPrivate,
			URLPrivateDownload: f.URLPrivateDownload,
		})
	}
	return out
}

// tsToTime parses a Slack ts ("<seconds>.<micros>") into a UTC time.
func tsToTime(ts string) time.Time {
	secStr, fracStr, _ := strings.Cut(ts, ".")
	sec, err := strconv.ParseInt(secStr, 10, 64)
	if err != nil {
		return time.Time{}
	}
	var nsec int64
	if fracStr != "" {
		if micros, err := strconv.ParseInt(fracStr, 10, 64); err == nil {
			nsec = micros * 1000
		}
	}
	return time.Unix(sec, nsec).UTC()
}

// tsFromTime renders a time as a Slack ts for oldest/latest windowing.
func tsFromTime(t time.Time) string {
	return strconv.FormatInt(t.Unix(), 10) + "." + strconv.FormatInt(int64(t.Nanosecond()/1000), 10)
}
