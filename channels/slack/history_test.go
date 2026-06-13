package slack

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/crashchat-ai/mio/pkg/channels"
)

// fakeHistory routes by Slack method path and serves canned JSON per method,
// recording the last form values so tests assert channel/cursor/oldest/limit.
type fakeHistory struct {
	srv       *httptest.Server
	lastForm  map[string]map[string]string
	responses map[string]string
	status    map[string]int
	headers   map[string]map[string]string
}

func newFakeHistory(t *testing.T) *fakeHistory {
	t.Helper()
	f := &fakeHistory{
		lastForm:  map[string]map[string]string{},
		responses: map[string]string{},
		status:    map[string]int{},
		headers:   map[string]map[string]string{},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		method := strings.TrimPrefix(r.URL.Path, "/")
		vals := map[string]string{}
		for k := range r.Form {
			vals[k] = r.Form.Get(k)
		}
		f.lastForm[method] = vals
		for k, v := range f.headers[method] {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "application/json")
		if code := f.status[method]; code != 0 {
			w.WriteHeader(code)
		}
		body := f.responses[method]
		if body == "" {
			body = `{"ok":true}`
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(f.srv.Close)
	testAPIURL = f.srv.URL + "/"
	t.Cleanup(func() { testAPIURL = "" })
	return f
}

func histReq(channel string) channels.HistoryRequest {
	return channels.HistoryRequest{
		Credential:   channels.Credential{AccessToken: "xoxb-test"},
		Conversation: channels.HistoryConversation{ExternalID: channel},
	}
}

func TestFetchHistory_NormalizesAndComposites(t *testing.T) {
	f := newFakeHistory(t)
	f.responses["conversations.history"] = `{
		"ok": true,
		"messages": [
			{"type":"message","user":"U1","text":"hello","ts":"1700000000.000100"},
			{"type":"message","bot_id":"B9","username":"bot","text":"beep","ts":"1700000001.000200",
			 "files":[{"id":"F1","name":"a.png","mimetype":"image/png","size":42,"url_private":"https://x/a.png"}]}
		],
		"response_metadata": {"next_cursor": "CURSOR2"}
	}`
	a := &Adapter{botToken: "xoxb-test"}
	page, err := a.FetchHistory(context.Background(), histReq("C1"))
	if err != nil {
		t.Fatalf("FetchHistory: %v", err)
	}
	if page.NextCursor != "CURSOR2" {
		t.Fatalf("next cursor = %q", page.NextCursor)
	}
	if len(page.Messages) != 2 {
		t.Fatalf("messages len = %d", len(page.Messages))
	}
	if f.lastForm["conversations.history"]["channel"] != "C1" {
		t.Errorf("wrong channel: %q", f.lastForm["conversations.history"]["channel"])
	}
	if f.lastForm["conversations.history"]["limit"] != "999" {
		t.Errorf("limit not clamped to 999: %q", f.lastForm["conversations.history"]["limit"])
	}
	first := page.Messages[0]
	if first.SourceMessageID != "C1:1700000000.000100" {
		t.Errorf("composite id = %q", first.SourceMessageID)
	}
	if first.SenderExternalID != "U1" || first.Text != "hello" || first.SenderIsBot {
		t.Errorf("first not normalized: %+v", first)
	}
	if first.SentAt.Unix() != 1700000000 {
		t.Errorf("SentAt = %v", first.SentAt)
	}
	second := page.Messages[1]
	if !second.SenderIsBot || second.SenderExternalID != "B9" {
		t.Errorf("bot sender not set: %+v", second)
	}
	if len(second.Attachments) != 1 || second.Attachments[0].GetFilename() != "a.png" {
		t.Errorf("attachment not normalized: %+v", second.Attachments)
	}
}

func TestFetchHistory_ExpandsReplies(t *testing.T) {
	f := newFakeHistory(t)
	f.responses["conversations.history"] = `{
		"ok": true,
		"messages": [
			{"type":"message","user":"U1","text":"thread root","ts":"1700000000.000100","reply_count":2,"thread_ts":"1700000000.000100"}
		]
	}`
	f.responses["conversations.replies"] = `{
		"ok": true,
		"messages": [
			{"type":"message","user":"U1","text":"thread root","ts":"1700000000.000100","thread_ts":"1700000000.000100"},
			{"type":"message","user":"U2","text":"reply one","ts":"1700000010.000100","thread_ts":"1700000000.000100"},
			{"type":"message","user":"U3","text":"reply two","ts":"1700000020.000100","thread_ts":"1700000000.000100"}
		]
	}`
	a := &Adapter{botToken: "xoxb-test"}
	page, err := a.FetchHistory(context.Background(), histReq("C1"))
	if err != nil {
		t.Fatalf("FetchHistory: %v", err)
	}
	if len(page.Messages) != 3 {
		t.Fatalf("want root+2 replies = 3, got %d", len(page.Messages))
	}
	if f.lastForm["conversations.replies"]["ts"] != "1700000000.000100" {
		t.Errorf("replies fetched for wrong ts: %q", f.lastForm["conversations.replies"]["ts"])
	}
	reply := page.Messages[1]
	if reply.SourceMessageID != "C1:1700000010.000100" {
		t.Errorf("reply composite id = %q", reply.SourceMessageID)
	}
	if reply.ParentExternalID != "C1:1700000000.000100" {
		t.Errorf("reply parent composite = %q, want C1:1700000000.000100", reply.ParentExternalID)
	}
	root := page.Messages[0]
	if root.ParentExternalID != "" {
		t.Errorf("thread root must have no parent: %q", root.ParentExternalID)
	}
}

func TestFetchHistory_FollowsCursor(t *testing.T) {
	f := newFakeHistory(t)
	f.responses["conversations.history"] = `{
		"ok": true,
		"messages": [{"type":"message","user":"U1","text":"a","ts":"1700000000.000100"}],
		"response_metadata": {"next_cursor": "NEXT"}
	}`
	a := &Adapter{botToken: "xoxb-test"}
	req := histReq("C1")
	req.Cursor = "PREV"
	page, err := a.FetchHistory(context.Background(), req)
	if err != nil {
		t.Fatalf("FetchHistory: %v", err)
	}
	if f.lastForm["conversations.history"]["cursor"] != "PREV" {
		t.Errorf("inbound cursor not forwarded: %q", f.lastForm["conversations.history"]["cursor"])
	}
	if page.NextCursor != "NEXT" {
		t.Errorf("next cursor = %q", page.NextCursor)
	}
}

func TestFetchHistory_ScopeMissing(t *testing.T) {
	f := newFakeHistory(t)
	f.responses["conversations.history"] = `{"ok":false,"error":"missing_scope"}`
	a := &Adapter{botToken: "xoxb-test"}
	_, err := a.FetchHistory(context.Background(), histReq("C1"))
	if !channels.IsScopeMissing(err) {
		t.Fatalf("want ScopeMissing, got %T %v", err, err)
	}
	var sme *channels.ScopeMissingError
	if !errors.As(err, &sme) || sme.Scope != "channels:history" {
		t.Fatalf("scope not set: %T %v", err, err)
	}
}

func TestFetchHistory_RateLimited(t *testing.T) {
	f := newFakeHistory(t)
	f.status["conversations.history"] = http.StatusTooManyRequests
	f.headers["conversations.history"] = map[string]string{"Retry-After": "7"}
	a := &Adapter{botToken: "xoxb-test"}
	_, err := a.FetchHistory(context.Background(), histReq("C1"))
	if err == nil {
		t.Fatal("want rate-limit error")
	}
	var de *DeliveryError
	if !errors.As(err, &de) || !de.IsRateLimited() || de.RetryAfterSeconds() != 7 {
		t.Fatalf("want rate-limited DeliveryError with retry-after 7, got %T %v", err, err)
	}
}

func TestFetchHistory_RequiresToken(t *testing.T) {
	a := &Adapter{}
	_, err := a.FetchHistory(context.Background(), channels.HistoryRequest{
		Conversation: channels.HistoryConversation{ExternalID: "C1"},
	})
	if err == nil || !strings.Contains(err.Error(), "no bot token") {
		t.Fatalf("want no-token error, got %v", err)
	}
}
