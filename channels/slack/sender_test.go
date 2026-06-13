package slack

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// fakeSlack records the last form values per Slack API method and replies with a
// scripted response, mirroring how slack-go parses {ok,channel,ts} / errors.
type fakeSlack struct {
	srv      *httptest.Server
	lastForm map[string]map[string]string // method path -> form values
	respTS   string
	respChan string
}

func newFakeSlack(t *testing.T) *fakeSlack {
	t.Helper()
	f := &fakeSlack{lastForm: map[string]map[string]string{}, respTS: "111.222", respChan: "C999"}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		method := strings.TrimPrefix(r.URL.Path, "/")
		vals := map[string]string{}
		for k := range r.Form {
			vals[k] = r.Form.Get(k)
		}
		f.lastForm[method] = vals
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"channel":"` + f.respChan + `","ts":"` + f.respTS + `"}`))
	}))
	t.Cleanup(f.srv.Close)
	testAPIURL = f.srv.URL + "/"
	t.Cleanup(func() { testAPIURL = "" })
	return f
}

func newTestAdapter() *Adapter { return &Adapter{botToken: "xoxb-test"} }

func TestSend_ReturnsComposite(t *testing.T) {
	f := newFakeSlack(t)
	a := newTestAdapter()

	cmd := &miov1.SendCommand{ConversationExternalId: "C123", Text: "hello **world**"}
	got, err := a.Send(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	// channel comes from the API response (C999), ts from the response.
	if got != "C999:111.222" {
		t.Fatalf("want composite C999:111.222, got %q", got)
	}
	if txt := f.lastForm["chat.postMessage"]["text"]; txt != "hello *world*" {
		t.Errorf("mrkdwn not applied: %q", txt)
	}
	if f.lastForm["chat.postMessage"]["channel"] != "C123" {
		t.Errorf("posted to wrong channel: %q", f.lastForm["chat.postMessage"]["channel"])
	}
}

// TestComposite_RoundTrip is THE invariant: Send's return value, stored and fed
// back as EditOfExternalId (the pool resolveEdit path), splits to the exact same
// channel + ts so chat.update targets the original message.
func TestComposite_RoundTrip(t *testing.T) {
	f := newFakeSlack(t)
	a := newTestAdapter()

	sent, err := a.Send(context.Background(), &miov1.SendCommand{ConversationExternalId: "C123", Text: "v1"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Simulate pool.go: Set(externalID) → resolveEdit feeds it as EditOfExternalId.
	state := map[string]string{}
	state["cmd-1"] = sent
	editCmd := &miov1.SendCommand{EditOfExternalId: state["cmd-1"], Text: "v2"}

	if err := a.Edit(context.Background(), editCmd); err != nil {
		t.Fatalf("Edit: %v", err)
	}

	upd := f.lastForm["chat.update"]
	if upd["channel"] != "C999" {
		t.Errorf("Edit hit wrong channel %q (want C999 from Send response)", upd["channel"])
	}
	if upd["ts"] != "111.222" {
		t.Errorf("Edit hit wrong ts %q (want 111.222)", upd["ts"])
	}
	if upd["text"] != "v2" {
		t.Errorf("Edit text %q", upd["text"])
	}
}

func TestEdit_RejectsBareID(t *testing.T) {
	newFakeSlack(t)
	a := newTestAdapter()
	err := a.Edit(context.Background(), &miov1.SendCommand{EditOfExternalId: "111.222", Text: "x"})
	if err == nil || !strings.Contains(err.Error(), "not a composite") {
		t.Fatalf("want composite-split error, got %v", err)
	}
}

func TestSend_ThreadReply(t *testing.T) {
	tests := []struct {
		name     string
		cmd      *miov1.SendCommand
		wantTS   string
		wantBcst bool
	}{
		{
			name:   "thread_root composite",
			cmd:    &miov1.SendCommand{ConversationExternalId: "C1", Text: "r", ThreadRootMessageId: "C1:100.200"},
			wantTS: "100.200",
		},
		{
			name:   "slack_thread_ts attr wins",
			cmd:    &miov1.SendCommand{ConversationExternalId: "C1", Text: "r", ThreadRootMessageId: "C1:100.200", Attributes: map[string]string{attrSlackThreadTS: "999.888"}},
			wantTS: "999.888",
		},
		{
			name: "KIND_REPLY relation",
			cmd: &miov1.SendCommand{ConversationExternalId: "C1", Text: "r", Relation: &miov1.MessageRelation{
				Kind: miov1.MessageRelation_KIND_REPLY, TargetExternalId: "C1:300.400",
			}},
			wantTS: "300.400",
		},
		{
			name: "broadcast attr",
			cmd: &miov1.SendCommand{ConversationExternalId: "C1", Text: "r", ThreadRootMessageId: "C1:100.200",
				Attributes: map[string]string{attrSlackThreadBroadcast: "true"}},
			wantTS: "100.200", wantBcst: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSlack(t)
			a := newTestAdapter()
			if _, err := a.Send(context.Background(), tc.cmd); err != nil {
				t.Fatalf("Send: %v", err)
			}
			form := f.lastForm["chat.postMessage"]
			if form["thread_ts"] != tc.wantTS {
				t.Errorf("thread_ts = %q, want %q", form["thread_ts"], tc.wantTS)
			}
			gotBcst := form["reply_broadcast"] == "true"
			if gotBcst != tc.wantBcst {
				t.Errorf("reply_broadcast = %v, want %v", gotBcst, tc.wantBcst)
			}
		})
	}
}

func TestSend_TokenUnset(t *testing.T) {
	a := &Adapter{}
	if _, err := a.Send(context.Background(), &miov1.SendCommand{ConversationExternalId: "C1"}); err == nil {
		t.Fatal("want error when SLACK_BOT_TOKEN unset")
	}
}

func TestSend_RequiresChannel(t *testing.T) {
	newFakeSlack(t)
	a := newTestAdapter()
	if _, err := a.Send(context.Background(), &miov1.SendCommand{Text: "x"}); err == nil {
		t.Fatal("want error when conversation_external_id empty")
	}
}

var _ channels.Adapter = (*Adapter)(nil)
