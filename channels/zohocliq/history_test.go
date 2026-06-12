package zohocliq

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

func TestFetchHistory_NormalizesCliqRows(t *testing.T) {
	var gotPath string
	var gotQuery string
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [
				{
					"sender": {"name": "TobyTime", "id": "bot-1"},
					"id": "1781194520798_518474135774",
					"time": 1781194520798,
					"type": "text",
					"content": {"text": "RBT Scheduling - Weekday schedule"}
				},
				{
					"sender": {"name": "Jane", "id": "user-1"},
					"id": "1781194521798_518474135775",
					"time": 1781194521798,
					"type": "file",
					"content": {
						"comment": "see report",
						"file": {
							"name": "report.pdf",
							"type": "file",
							"id": "file-1",
							"dimensions": {"size": 2048}
						}
					}
				}
			]
		}`))
	}))
	defer srv.Close()

	a := &Adapter{baseURL: srv.URL, botName: "TobyTime", httpClient: srv.Client()}
	page, err := a.FetchHistory(context.Background(), channels.HistoryRequest{
		Credential: channels.Credential{AccessToken: "access-token"},
		Conversation: channels.HistoryConversation{
			ExternalID:  "CT_123",
			DisplayName: "TobyTimeDev",
			Attributes:  map[string]string{"cliq_channel_name": "tobytimedev"},
		},
		Cursor: "1781194520000",
		Limit:  50,
	})
	if err != nil {
		t.Fatalf("FetchHistory: %v", err)
	}
	if gotPath != "/api/v2/chats/CT_123/messages" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotQuery != "fromtime=1781194520000&limit=50" {
		t.Fatalf("query = %q", gotQuery)
	}
	if gotAuth != "Zoho-oauthtoken access-token" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if page.NextCursor != "1781194521799" {
		t.Fatalf("next cursor = %q", page.NextCursor)
	}
	if len(page.Messages) != 2 {
		t.Fatalf("messages len = %d", len(page.Messages))
	}
	first := page.Messages[0]
	if first.SourceMessageID != "1781194520798_518474135774" || first.Text != "RBT Scheduling - Weekday schedule" {
		t.Fatalf("first message not normalized: %+v", first)
	}
	if !first.SenderIsBot {
		t.Fatalf("expected bot sender: %+v", first)
	}
	if first.Attributes["cliq_channel_name"] != "tobytimedev" {
		t.Fatalf("missing channel attr: %+v", first.Attributes)
	}
	second := page.Messages[1]
	if second.Text != "see report" {
		t.Fatalf("second text = %q", second.Text)
	}
	if len(second.Attachments) != 1 {
		t.Fatalf("attachments len = %d", len(second.Attachments))
	}
	att := second.Attachments[0]
	if att.GetKind() != miov1.Attachment_KIND_FILE || att.GetFilename() != "report.pdf" || att.GetBytes() != 2048 {
		t.Fatalf("attachment not normalized: %+v", att)
	}
	if att.GetErrorCode() != miov1.Attachment_ERROR_CODE_FORBIDDEN {
		t.Fatalf("attachment error code = %v", att.GetErrorCode())
	}
	if second.Attributes["cliq_file_id"] != "file-1" {
		t.Fatalf("missing file id attr: %+v", second.Attributes)
	}
}

func TestFetchHistory_RepliedMessageSnapshot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [
				{
					"sender": {"name": "Alice", "id": "user-alice"},
					"id": "msg-reply-001",
					"time": 1781200000000,
					"type": "text",
					"text": "This is a thread reply",
					"replied_message": {
						"id": "msg-parent-001",
						"text": "Original quoted text",
						"sender": {"id": "user-bob", "name": "Bob"},
						"time": 1781100000000,
						"type": "text"
					}
				},
				{
					"sender": {"name": "Carol", "id": "user-carol"},
					"id": "msg-plain-001",
					"time": 1781200001000,
					"type": "text",
					"text": "Plain message no reply"
				}
			]
		}`))
	}))
	defer srv.Close()

	a := &Adapter{baseURL: srv.URL, httpClient: srv.Client()}
	page, err := a.FetchHistory(context.Background(), channels.HistoryRequest{
		Credential:   channels.Credential{AccessToken: "tok"},
		Conversation: channels.HistoryConversation{ExternalID: "CT_test"},
	})
	if err != nil {
		t.Fatalf("FetchHistory: %v", err)
	}
	if len(page.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(page.Messages))
	}

	reply := page.Messages[0]
	if reply.ParentExternalID != "msg-parent-001" {
		t.Errorf("ParentExternalID = %q, want %q", reply.ParentExternalID, "msg-parent-001")
	}
	wantAttrs := map[string]string{
		"cliq_replied_message_id":          "msg-parent-001",
		"cliq_replied_message_text":        "Original quoted text",
		"cliq_replied_message_sender_id":   "user-bob",
		"cliq_replied_message_sender_name": "Bob",
		"cliq_replied_message_time":        "1781100000000",
		"cliq_replied_message_type":        "text",
	}
	for k, want := range wantAttrs {
		got, ok := reply.Attributes[k]
		if !ok {
			t.Errorf("reply: attribute %q missing", k)
			continue
		}
		if got != want {
			t.Errorf("reply: attribute %q = %q, want %q", k, got, want)
		}
	}

	plain := page.Messages[1]
	if plain.ParentExternalID != "" {
		t.Errorf("plain message ParentExternalID = %q, want empty", plain.ParentExternalID)
	}
	replyAttrs := []string{
		"cliq_replied_message_id",
		"cliq_replied_message_text",
		"cliq_replied_message_sender_id",
		"cliq_replied_message_sender_name",
		"cliq_replied_message_time",
		"cliq_replied_message_type",
	}
	for _, k := range replyAttrs {
		if _, ok := plain.Attributes[k]; ok {
			t.Errorf("plain message must not have attribute %q", k)
		}
	}
}

func TestFetchHistory_ScopeMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"oauthtoken_scope_invalid"}`))
	}))
	defer srv.Close()

	a := &Adapter{baseURL: srv.URL, httpClient: srv.Client()}
	_, err := a.FetchHistory(context.Background(), channels.HistoryRequest{
		Credential:   channels.Credential{AccessToken: "access-token"},
		Conversation: channels.HistoryConversation{ExternalID: "CT_123"},
	})
	if !errors.Is(err, channels.ErrScopeMissing) {
		t.Fatalf("expected scope missing, got %T %v", err, err)
	}
}
