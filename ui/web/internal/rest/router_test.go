package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"

	"github.com/crashchat-ai/mio/ui/web/internal/adminclient"
	"github.com/crashchat-ai/mio/ui/web/internal/auth"
)

type fakeAdmin struct {
	tenants  []*adminv1.Tenant
	channels []*adminv1.ChannelTypeInfo
	accounts []*adminv1.Account
	stream   *fakeStream
}

func (f *fakeAdmin) ListTenants(context.Context) ([]*adminv1.Tenant, error) {
	return f.tenants, nil
}

func (f *fakeAdmin) ListChannelTypes(context.Context) ([]*adminv1.ChannelTypeInfo, error) {
	return f.channels, nil
}

func (f *fakeAdmin) ListAccounts(context.Context, string) ([]*adminv1.Account, error) {
	return f.accounts, nil
}

func (f *fakeAdmin) TailMessages(context.Context, string, string) (adminclient.MessageStream, error) {
	return f.stream, nil
}

type fakeStream struct {
	messages []*adminv1.TailMessagesResponse
	index    int
}

func (f *fakeStream) Receive() bool {
	return f.index < len(f.messages)
}

func (f *fakeStream) Msg() *adminv1.TailMessagesResponse {
	msg := f.messages[f.index]
	f.index++
	return msg
}

func (f *fakeStream) Err() error   { return nil }
func (f *fakeStream) Close() error { return nil }

func TestAdminRoutesRequireSession(t *testing.T) {
	handler := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/tenants", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminReadRoutes(t *testing.T) {
	handler := newTestHandler(t)
	cookie := loginCookie(t, handler)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/tenants", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tenants status: %d body=%s", rec.Code, rec.Body.String())
	}
	var tenants struct {
		Tenants []tenantJSON `json:"tenants"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tenants); err != nil {
		t.Fatalf("decode tenants: %v", err)
	}
	if len(tenants.Tenants) != 1 || tenants.Tenants[0].Slug != "acme" {
		t.Fatalf("tenants: %+v", tenants.Tenants)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/accounts?tenant_id=t1", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("accounts status: %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"displayName":"Cliq prod"`) {
		t.Fatalf("accounts body: %s", rec.Body.String())
	}
}

func TestTailMessagesSSE(t *testing.T) {
	handler := newTestHandler(t)
	cookie := loginCookie(t, handler)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/messages/tail?account_id=a1", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tail status: %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: message") || !strings.Contains(body, `"text":"hello"`) {
		t.Fatalf("tail body: %s", body)
	}
}

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	manager, err := auth.NewManager(auth.Config{
		Mode:        auth.ModeDev,
		Store:       auth.NewMemoryStore(),
		Allowlist:   auth.ParseAllowlist("operator@example.com", ""),
		DevIdentity: auth.Identity{Email: "operator@example.com", Name: "Operator"},
		StateSecret: []byte("test-secret-test-secret-test-secret"),
	})
	if err != nil {
		t.Fatalf("auth.NewManager: %v", err)
	}
	now := timestamppb.New(time.Date(2026, 6, 10, 10, 0, 0, 0, time.UTC))
	return New(Config{
		Auth: manager,
		Admin: &fakeAdmin{
			tenants: []*adminv1.Tenant{
				{Id: "t1", Slug: "acme", DisplayName: "Acme", Status: "active", CreatedAt: now},
			},
			channels: []*adminv1.ChannelTypeInfo{
				{
					Slug:   "zoho_cliq",
					Status: "active",
					Capabilities: &miov1.ChannelCapabilities{
						AuthKind:           "oauth2_refresh",
						SupportsThreads:    true,
						RateLimitScope:     "account",
						RateLimitPerSecond: 10,
						MaxTextBytes:       4096,
					},
				},
			},
			accounts: []*adminv1.Account{
				{Id: "a1", TenantId: "t1", ChannelType: "zoho_cliq", Provider: "default", DisplayName: "Cliq prod", CreatedAt: now},
			},
			stream: &fakeStream{messages: []*adminv1.TailMessagesResponse{
				{Id: "m1", TenantId: "t1", AccountId: "a1", Text: "hello", ReceivedAt: now},
			}},
		},
		Assets: http.NotFoundHandler(),
	})
}

func loginCookie(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("login status: %d body=%s", rec.Code, rec.Body.String())
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "mio_web_session" {
			return cookie
		}
	}
	t.Fatal("session cookie not found")
	return nil
}
