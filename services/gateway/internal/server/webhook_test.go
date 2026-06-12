package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"github.com/crashchat-ai/mio/services/gateway/store"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"log/slog"
)

type fakeInbound struct {
	secret       []byte
	verifyErr    error
	normalizeMsg *miov1.Message
	normalizeErr error
	handshake    bool
	aliases      []string
}

func (f *fakeInbound) VerifySignature(http.Header, []byte) error { return f.verifyErr }

func (f *fakeInbound) Normalize([]byte) (*miov1.Message, error) {
	return f.normalizeMsg, f.normalizeErr
}

func (f *fakeInbound) HandleHandshake(w http.ResponseWriter, r *http.Request) bool {
	if f.handshake {
		w.WriteHeader(http.StatusOK)
		return true
	}
	return false
}

func (f *fakeInbound) WithSecret(secret []byte) channels.InboundAdapter {
	cp := *f
	cp.secret = secret
	return &cp
}

func (f *fakeInbound) RouteAliases() []string { return f.aliases }

type fakeStore struct {
	convErr     error
	msgFresh    bool
	msgErr      error
	parentRef   channels.MessageRef
	parentFound bool
	parentErr   error

	gotKind        string
	gotDisplayName *string
	gotThreadRoot  *string
}

func (s *fakeStore) EnsureConversation(
	_ context.Context, id uuid.UUID,
	_, _, _, kind, _ string,
	_ *uuid.UUID, _ *string, displayName *string, _ map[string]string,
) (channels.Conversation, error) {
	s.gotKind = kind
	s.gotDisplayName = displayName
	return channels.Conversation{ID: id}, s.convErr
}

func (s *fakeStore) EnsureUniqueMessage(
	_ context.Context, id uuid.UUID,
	_, _, _ string, threadRoot *string,
	_, _, _ string, _ map[string]string,
) (uuid.UUID, bool, error) {
	s.gotThreadRoot = threadRoot
	return id, s.msgFresh, s.msgErr
}

func (s *fakeStore) FindMessageBySource(_ context.Context, _, _ string) (channels.MessageRef, bool, error) {
	return s.parentRef, s.parentFound, s.parentErr
}

type fakePub struct {
	published *miov1.Message
	err       error
}

func (p *fakePub) PublishInbound(_ context.Context, msg *miov1.Message) error {
	p.published = msg
	return p.err
}

func basicMsg() *miov1.Message {
	return &miov1.Message{
		SchemaVersion:          1,
		ChannelType:            "fake_chan",
		ConversationExternalId: "conv-ext-1",
		ConversationKind:       miov1.ConversationKind_CONVERSATION_KIND_DM,
		SourceMessageId:        "src-1",
		Sender:                 &miov1.Sender{ExternalId: "user-1"},
		Text:                   "hello",
		Attributes:             map[string]string{channels.AttrConversationDisplayName: "General"},
	}
}

func newPipeline(inb channels.InboundAdapter, st *fakeStore, pub *fakePub) *webhookPipeline {
	return &webhookPipeline{
		channelType: "fake_chan",
		inbound:     inb,
		store:       st,
		pub:         pub,
		tenantID:    "tenant-1",
		accountID:   "acct-1",
		metrics:     newGatewayMetrics(prometheus.NewRegistry()),
		logger:      slog.Default(),
	}
}

func post(t *testing.T, h http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/webhooks/fake-chan", strings.NewReader(`{}`)))
	return rec
}

func TestWebhookPipeline_Success(t *testing.T) {
	st := &fakeStore{msgFresh: true}
	pub := &fakePub{}
	p := newPipeline(&fakeInbound{normalizeMsg: basicMsg()}, st, pub)

	rec := post(t, p)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if pub.published == nil {
		t.Fatal("expected publish")
	}
	if pub.published.TenantId != "tenant-1" || pub.published.AccountId != "acct-1" {
		t.Errorf("tenant/account not stamped: %s/%s", pub.published.TenantId, pub.published.AccountId)
	}
	if pub.published.Id == "" || pub.published.ConversationId == "" {
		t.Error("durable ids not stamped")
	}
	if st.gotKind != "CONVERSATION_KIND_DM" {
		t.Errorf("kind = %q", st.gotKind)
	}
	if st.gotDisplayName == nil || *st.gotDisplayName != "General" {
		t.Errorf("display name = %v", st.gotDisplayName)
	}
}

func TestWebhookPipeline_Dedup(t *testing.T) {
	st := &fakeStore{msgFresh: false}
	pub := &fakePub{}
	p := newPipeline(&fakeInbound{normalizeMsg: basicMsg()}, st, pub)

	rec := post(t, p)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if pub.published != nil {
		t.Error("duplicate must not publish")
	}
}

func TestWebhookPipeline_BadSignature(t *testing.T) {
	p := newPipeline(&fakeInbound{verifyErr: errors.New("nope")}, &fakeStore{}, &fakePub{})
	rec := post(t, p)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestWebhookPipeline_SoftNormalizeError200(t *testing.T) {
	inb := &fakeInbound{normalizeErr: channels.ErrNormalizeSoft}
	rec := post(t, newPipeline(inb, &fakeStore{}, &fakePub{}))
	if rec.Code != http.StatusOK {
		t.Fatalf("soft normalize failure must return 200, got %d", rec.Code)
	}
	var body map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || !body["ok"] {
		t.Errorf("want {ok:true}, got %s", rec.Body.String())
	}
}

func TestWebhookPipeline_HardParseError400(t *testing.T) {
	inb := &fakeInbound{normalizeErr: errors.New("bad json")}
	rec := post(t, newPipeline(inb, &fakeStore{}, &fakePub{}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("hard parse failure must return 400, got %d", rec.Code)
	}
}

func TestWebhookPipeline_HandshakeConsumed(t *testing.T) {
	pub := &fakePub{}
	rec := post(t, newPipeline(&fakeInbound{handshake: true}, &fakeStore{}, pub))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if pub.published != nil {
		t.Error("handshake must not continue the pipeline")
	}
}

func replyMsg() *miov1.Message {
	msg := basicMsg()
	msg.ParentConversationId = "parent-ext-1"
	msg.Relation = &miov1.MessageRelation{
		Kind:             miov1.MessageRelation_KIND_REPLY,
		TargetExternalId: "parent-ext-1",
	}
	return msg
}

func TestWebhookPipeline_ReplyResolvedParent(t *testing.T) {
	parentID := uuid.New()
	rootID := uuid.New()
	st := &fakeStore{
		msgFresh:    true,
		parentFound: true,
		parentRef:   channels.MessageRef{ID: parentID, ThreadRootMessageID: rootID},
	}
	pub := &fakePub{}
	rec := post(t, newPipeline(&fakeInbound{normalizeMsg: replyMsg()}, st, pub))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if got := pub.published.GetRelation().GetTargetMessageId(); got != parentID.String() {
		t.Errorf("relation target = %q, want %q", got, parentID)
	}
	if pub.published.GetThreadRootMessageId() != rootID.String() {
		t.Errorf("thread root = %q, want %q", pub.published.GetThreadRootMessageId(), rootID)
	}
	if st.gotThreadRoot == nil || *st.gotThreadRoot != rootID.String() {
		t.Errorf("db thread root = %v", st.gotThreadRoot)
	}
}

func TestWebhookPipeline_ReplyUnresolvedParent(t *testing.T) {
	st := &fakeStore{msgFresh: true, parentFound: false}
	pub := &fakePub{}
	rec := post(t, newPipeline(&fakeInbound{normalizeMsg: replyMsg()}, st, pub))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if got := pub.published.GetRelation().GetTargetMessageId(); got != "" {
		t.Errorf("unresolved parent must leave target empty, got %q", got)
	}
	if pub.published.GetThreadRootMessageId() != "" {
		t.Error("unresolved parent must leave thread root empty")
	}
	if pub.published.GetRelation().GetTargetExternalId() != "parent-ext-1" {
		t.Error("external target must survive for downstream resolution")
	}
}

type fakeResolver struct {
	res        store.ResolvedAccount
	ok         bool
	err        error
	gotKey     string
	gotChannel string
}

func (f *fakeResolver) Resolve(_ context.Context, channelType, workspaceKey string) (store.ResolvedAccount, bool, error) {
	f.gotChannel = channelType
	f.gotKey = workspaceKey
	return f.res, f.ok, f.err
}

type keyedInbound struct {
	fakeInbound
	key string
}

func (k *keyedInbound) WorkspaceKey(*miov1.Message) string { return k.key }

func TestWebhookPipeline_ResolverWins_EnvNeverUsed(t *testing.T) {
	st := &fakeStore{msgFresh: true}
	pub := &fakePub{}
	res := &fakeResolver{res: store.ResolvedAccount{TenantID: "tenant-db", AccountID: "acct-db"}, ok: true}
	inb := &keyedInbound{fakeInbound: fakeInbound{normalizeMsg: basicMsg()}, key: "org-42"}
	p := newPipeline(inb, st, pub)
	p.accounts = res

	rec := post(t, p)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if pub.published.TenantId != "tenant-db" || pub.published.AccountId != "acct-db" {
		t.Errorf("resolved identity must win over env: got %s/%s",
			pub.published.TenantId, pub.published.AccountId)
	}
	if res.gotKey != "org-42" || res.gotChannel != "fake_chan" {
		t.Errorf("resolver inputs: key=%q channel=%q", res.gotKey, res.gotChannel)
	}
}

func TestWebhookPipeline_EnvFallbackWhenUnresolved(t *testing.T) {
	st := &fakeStore{msgFresh: true}
	pub := &fakePub{}
	p := newPipeline(&fakeInbound{normalizeMsg: basicMsg()}, st, pub)
	p.accounts = &fakeResolver{ok: false}

	rec := post(t, p)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if pub.published.TenantId != "tenant-1" || pub.published.AccountId != "acct-1" {
		t.Errorf("env fallback expected: got %s/%s", pub.published.TenantId, pub.published.AccountId)
	}
}

func TestWebhookPipeline_Unroutable200NoPublish(t *testing.T) {
	st := &fakeStore{msgFresh: true}
	pub := &fakePub{}
	p := newPipeline(&fakeInbound{normalizeMsg: basicMsg()}, st, pub)
	p.accounts = &fakeResolver{ok: false}
	p.tenantID, p.accountID = "", ""

	rec := post(t, p)
	if rec.Code != http.StatusOK {
		t.Fatalf("unroutable must 200 (platform must not retry), got %d", rec.Code)
	}
	if pub.published != nil {
		t.Error("unroutable webhook must not publish")
	}
	if st.gotKind != "" {
		t.Error("unroutable webhook must not touch the store")
	}
}

func TestWebhookPipeline_ResolverError500(t *testing.T) {
	st := &fakeStore{msgFresh: true}
	pub := &fakePub{}
	p := newPipeline(&fakeInbound{normalizeMsg: basicMsg()}, st, pub)
	p.accounts = &fakeResolver{err: errors.New("pg down")}

	rec := post(t, p)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("resolver error must 500 (platform retries), got %d", rec.Code)
	}
	if pub.published != nil {
		t.Error("resolver error must not publish")
	}
	if st.gotKind != "" {
		t.Error("resolver error must not fall back to env identity and touch the store")
	}
}

func TestRouter_UnknownChannel404(t *testing.T) {
	h := New(nil, nil, nil, Config{Logger: slog.Default()}, prometheus.NewRegistry())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/webhooks/nope", strings.NewReader(`{}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
}

// fakeAdapter registers through the real registry to prove a channel mounts
// with zero core edits: slug route + alias + secret injection.
type fakeAdapter struct {
	inbound *fakeInbound
}

func (a *fakeAdapter) Send(context.Context, *miov1.SendCommand) (string, error) { return "", nil }
func (a *fakeAdapter) Edit(context.Context, *miov1.SendCommand) error           { return nil }
func (a *fakeAdapter) ChannelType() string                                      { return "fake_chan" }
func (a *fakeAdapter) MaxDeliver() int                                          { return 5 }
func (a *fakeAdapter) RateLimitKey(*miov1.SendCommand) string                   { return "" }
func (a *fakeAdapter) Capabilities() *miov1.ChannelCapabilities {
	return &miov1.ChannelCapabilities{}
}
func (a *fakeAdapter) Inbound() channels.InboundAdapter        { return a.inbound }
func (a *fakeAdapter) Credentials() channels.CredentialAdapter { return nil }

func TestRouter_RegistryMountWithAlias(t *testing.T) {
	inb := &fakeInbound{normalizeErr: channels.ErrNormalizeSoft, aliases: []string{"/fakealias"}}
	channels.RegisterAdapter(&fakeAdapter{inbound: inb})

	h := New(nil, nil, nil, Config{
		WebhookSecrets: map[string][]byte{"fake_chan": []byte("sek")},
		Logger:         slog.Default(),
	}, prometheus.NewRegistry())

	for _, path := range []string{"/webhooks/fake-chan", "/fakealias"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`)))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: want 200 (soft normalize), got %d", path, rec.Code)
		}
	}
	if string(inb.secret) == "sek" {
		t.Error("WithSecret must copy, not mutate the registered instance")
	}
}
