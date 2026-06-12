package zohocliq

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

const cliqHistoryReadScope = "ZohoCliq.Messages.READ"

// FetchHistory implements channels.HistoryAdapter for Cliq. It uses the v2
// chat messages endpoint because it returns concrete chat message rows and is
// documented with fromtime/totime windowing plus a 100-row default/max page.
func (a *Adapter) FetchHistory(ctx context.Context, req channels.HistoryRequest) (channels.HistoryPage, error) {
	if req.Credential.AccessToken == "" {
		return channels.HistoryPage{}, fmt.Errorf("cliq history: empty access token")
	}
	if req.Conversation.ExternalID == "" {
		return channels.HistoryPage{}, fmt.Errorf("cliq history: conversation external_id is required")
	}

	limit := req.Limit
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if !req.Since.IsZero() {
		q.Set("fromtime", strconv.FormatInt(req.Since.UnixMilli(), 10))
	} else if req.Cursor != "" {
		q.Set("fromtime", req.Cursor)
	}
	if !req.Until.IsZero() {
		q.Set("totime", strconv.FormatInt(req.Until.UnixMilli(), 10))
	}
	endpoint := fmt.Sprintf("%s/api/v2/chats/%s/messages?%s",
		a.baseURL, url.PathEscape(req.Conversation.ExternalID), q.Encode())

	httpClient := a.httpClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return channels.HistoryPage{}, fmt.Errorf("cliq history: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Zoho-oauthtoken "+req.Credential.AccessToken)

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return channels.HistoryPage{}, fmt.Errorf("cliq history: http: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, _ := io.ReadAll(io.LimitReader(resp.Body, oauthBodyCap))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusUnauthorized && bodyLooksScopeMissing(body) {
			return channels.HistoryPage{}, &channels.ScopeMissingError{
				ChannelType: cliqChannelType,
				Scope:       cliqHistoryReadScope,
				Err:         fmt.Errorf("cliq history: http %d: %s", resp.StatusCode, truncate(string(body), errBodyCap)),
			}
		}
		return channels.HistoryPage{}, &HTTPError{Code: resp.StatusCode, Body: truncate(string(body), errBodyCap)}
	}

	var parsed cliqHistoryResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return channels.HistoryPage{}, fmt.Errorf("cliq history: parse response: %w", err)
	}

	out := channels.HistoryPage{
		Messages: make([]channels.HistoryMessage, 0, len(parsed.Data)),
	}
	var maxMillis int64
	for _, row := range parsed.Data {
		msg := row.toHistoryMessage(req.Conversation, a.botName)
		if msg.SourceMessageID == "" {
			continue
		}
		if row.Time > maxMillis {
			maxMillis = row.Time
		}
		out.Messages = append(out.Messages, msg)
	}
	if maxMillis > 0 {
		out.NextCursor = strconv.FormatInt(maxMillis+1, 10)
	}
	return out, nil
}

var _ channels.HistoryAdapter = (*Adapter)(nil)

type cliqHistoryResponse struct {
	Data []cliqHistoryMessage `json:"data"`
}

type cliqHistoryMessage struct {
	ID             string              `json:"id"`
	Time           int64               `json:"time"`
	Type           string              `json:"type"`
	Text           string              `json:"text"`
	Comment        string              `json:"comment"`
	Sender         cliqHistorySender   `json:"sender"`
	Content        cliqHistoryContent  `json:"content"`
	RepliedMessage *cliqHistoryReplied `json:"replied_message"`
}

type cliqHistorySender struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	IsBot bool   `json:"is_bot"`
}

type cliqHistoryContent struct {
	Text    string           `json:"text"`
	Comment string           `json:"comment"`
	File    *cliqHistoryFile `json:"file"`
}

type cliqHistoryFile struct {
	ID         string                `json:"id"`
	Name       string                `json:"name"`
	Type       string                `json:"type"`
	URL        string                `json:"url"`
	Dimensions cliqHistoryDimensions `json:"dimensions"`
}

type cliqHistoryDimensions struct {
	Size int64 `json:"size"`
}

type cliqHistoryReplied struct {
	ID string `json:"id"`
}

func (m cliqHistoryMessage) toHistoryMessage(conv channels.HistoryConversation, botName string) channels.HistoryMessage {
	attrs := map[string]string{
		"cliq_history_type": m.Type,
	}
	if name := conv.Attributes["cliq_channel_name"]; name != "" {
		attrs["cliq_channel_name"] = name
	}
	text := m.Text
	if text == "" {
		text = m.Content.Text
	}
	if text == "" {
		text = m.Comment
	}
	if text == "" {
		text = m.Content.Comment
	}

	msg := channels.HistoryMessage{
		SourceMessageID:   m.ID,
		SenderExternalID:  m.Sender.ID,
		SenderDisplayName: m.Sender.Name,
		SenderIsBot:       m.Sender.IsBot || (botName != "" && strings.EqualFold(m.Sender.Name, botName)),
		Text:              text,
		Attributes:        attrs,
	}
	if m.Time > 0 {
		msg.SentAt = time.UnixMilli(m.Time)
	}
	if m.RepliedMessage != nil {
		msg.ParentExternalID = m.RepliedMessage.ID
	}
	if m.Content.File != nil {
		msg.Attachments = append(msg.Attachments, cliqHistoryAttachment(m.Content.File))
		if m.Content.File.ID != "" {
			msg.Attributes["cliq_file_id"] = m.Content.File.ID
		}
	}
	return msg
}

func cliqHistoryAttachment(f *cliqHistoryFile) *miov1.Attachment {
	att := &miov1.Attachment{
		Kind:     cliqHistoryAttachmentKind(f),
		Url:      f.URL,
		Filename: f.Name,
		Bytes:    f.Dimensions.Size,
	}
	if f.URL == "" && f.ID != "" {
		att.ErrorCode = miov1.Attachment_ERROR_CODE_FORBIDDEN
	}
	return att
}

func cliqHistoryAttachmentKind(f *cliqHistoryFile) miov1.Attachment_Kind {
	switch {
	case strings.HasPrefix(f.Type, "image/"):
		return miov1.Attachment_KIND_IMAGE
	case strings.HasPrefix(f.Type, "audio/"):
		return miov1.Attachment_KIND_AUDIO
	case strings.HasPrefix(f.Type, "video/"):
		return miov1.Attachment_KIND_VIDEO
	default:
		return miov1.Attachment_KIND_FILE
	}
}

func bodyLooksScopeMissing(body []byte) bool {
	lower := strings.ToLower(string(body))
	return strings.Contains(lower, "oauthtoken_scope_invalid") ||
		strings.Contains(lower, "scope")
}
