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
	"github.com/crashchat-ai/mio/ui/web/internal/audit"
	"github.com/crashchat-ai/mio/ui/web/internal/auth"
)

type fakeAdmin struct {
	tenants  []*adminv1.Tenant
	channels []*adminv1.ChannelTypeInfo
	accounts []*adminv1.Account
	stream   *fakeStream

	createdTenant *adminv1.Tenant
	startInstall  *adminv1.StartInstallResponse
	complete      *adminv1.Account
	credential    *adminv1.GetCredentialMetadataResponse
	disabledID    string
	rotatedID     string
}

func (f *fakeAdmin) CreateTenant(_ context.Context, slug, displayName string) (*adminv1.Tenant, error) {
	f.createdTenant = &adminv1.Tenant{Id: "new-tenant", Slug: slug, DisplayName: displayName, Status: "active"}
	return f.createdTenant, nil
}

func (f *fakeAdmin) ListTenants(context.Context) ([]*adminv1.Tenant, error) {
	return f.tenants, nil
}

func (f *fakeAdmin) ListChannelTypes(context.Context) ([]*adminv1.ChannelTypeInfo, error) {
	return f.channels, nil
}

func (f *fakeAdmin) StartInstall(context.Context, string, string, string) (*adminv1.StartInstallResponse, error) {
	f.startInstall = &adminv1.StartInstallResponse{InstallId: "install-1", OauthUrl: "https://example.test/oauth", RedirectUri: "http://127.0.0.1/callback"}
	return f.startInstall, nil
}

func (f *fakeAdmin) CompleteInstall(context.Context, string) (*adminv1.Account, error) {
	f.complete = f.accounts[0]
	return f.complete, nil
}

func (f *fakeAdmin) GetAccount(_ context.Context, accountID string) (*adminv1.Account, error) {
	for _, account := range f.accounts {
		if account.GetId() == accountID {
			return account, nil
		}
	}
	return nil, nil
}

func (f *fakeAdmin) UpdateAccount(_ context.Context, accountID, displayName, externalID string) (*adminv1.Account, error) {
	account := &adminv1.Account{Id: accountID, TenantId: "t1", ChannelType: "zoho_cliq", Provider: "default", DisplayName: displayName, ExternalId: externalID}
	f.accounts = []*adminv1.Account{account}
	return account, nil
}

func (f *fakeAdmin) SetRateLimit(_ context.Context, accountID string, perSecond int32, scope string) (*adminv1.Account, error) {
	account := &adminv1.Account{Id: accountID, TenantId: "t1", ChannelType: "zoho_cliq", Provider: "default", RateLimitPerSecond: perSecond, RateLimitScope: scope}
	f.accounts = []*adminv1.Account{account}
	return account, nil
}

func (f *fakeAdmin) GetCredentialMetadata(context.Context, string) (*adminv1.GetCredentialMetadataResponse, error) {
	if f.credential != nil {
		return f.credential, nil
	}
	return &adminv1.GetCredentialMetadataResponse{AccountId: "a1", HasCredential: true, AuthKind: "oauth2_refresh", KeyVersion: 7}, nil
}

func (f *fakeAdmin) ListAccounts(context.Context, string) ([]*adminv1.Account, error) {
	return f.accounts, nil
}

func (f *fakeAdmin) DisableAccount(_ context.Context, accountID string) error {
	f.disabledID = accountID
	return nil
}

func (f *fakeAdmin) RotateCredential(_ context.Context, accountID string) error {
	f.rotatedID = accountID
	return nil
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

func TestViewerCannotMutateAndAttemptIsAudited(t *testing.T) {
	handler, auditLog, fake := newTestFixture(t, auth.RoleViewer)
	cookie := loginCookie(t, handler)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/tenants", strings.NewReader(`{"slug":"blocked"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.createdTenant != nil {
		t.Fatal("viewer mutation reached admin service")
	}
	events := auditLog.Events()
	if len(events) != 1 || events[0].Action != "tenant.create" || events[0].Result != audit.ResultDenied {
		t.Fatalf("audit events: %+v", events)
	}
}

func TestOperatorMutationIsAudited(t *testing.T) {
	handler, auditLog, _ := newTestFixture(t, auth.RoleOperator)
	cookie := loginCookie(t, handler)
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/accounts", strings.NewReader(`{"accountId":"a1","displayName":"Cliq staging","externalId":"ext-2"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"displayName":"Cliq staging"`) {
		t.Fatalf("body: %s", rec.Body.String())
	}
	events := auditLog.Events()
	if len(events) != 1 || events[0].Action != "account.update" || events[0].Result != audit.ResultSuccess {
		t.Fatalf("audit events: %+v", events)
	}
}

func TestCredentialRotationRequiresCredentialAdmin(t *testing.T) {
	handler, _, fake := newTestFixture(t, auth.RoleOperator)
	cookie := loginCookie(t, handler)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/accounts/rotate-credential", strings.NewReader(`{"accountId":"a1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("operator status: %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.rotatedID != "" {
		t.Fatal("operator rotated credential")
	}

	handler, auditLog, fake := newTestFixture(t, auth.RoleCredentialAdmin)
	cookie = loginCookie(t, handler)
	req = httptest.NewRequest(http.MethodPost, "/api/admin/accounts/rotate-credential", strings.NewReader(`{"accountId":"a1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("credential-admin status: %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.rotatedID != "a1" {
		t.Fatalf("rotatedID: %q", fake.rotatedID)
	}
	events := auditLog.Events()
	if len(events) != 1 || events[0].Action != "credential.rotate" || events[0].Result != audit.ResultSuccess {
		t.Fatalf("audit events: %+v", events)
	}
}

func TestCredentialMetadataDoesNotExposePlaintext(t *testing.T) {
	handler := newTestHandler(t)
	cookie := loginCookie(t, handler)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/accounts/credential-metadata?account_id=a1", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "accessToken") || strings.Contains(body, "refreshToken") {
		t.Fatalf("credential body leaked plaintext: %s", body)
	}
	if !strings.Contains(body, `"keyVersion":7`) {
		t.Fatalf("credential body: %s", body)
	}
}

func newTestHandler(t *testing.T) http.Handler {
	handler, _, _ := newTestFixture(t, auth.RoleCredentialAdmin)
	return handler
}

func newTestFixture(t *testing.T, role auth.Role) (http.Handler, *audit.MemoryLogger, *fakeAdmin) {
	t.Helper()
	manager, err := auth.NewManager(auth.Config{
		Mode:        auth.ModeDev,
		Store:       auth.NewMemoryStore(),
		Allowlist:   auth.ParseAllowlist("operator@example.com", ""),
		DevRole:     role,
		DevIdentity: auth.Identity{Email: "operator@example.com", Name: "Operator"},
		StateSecret: []byte("test-secret-test-secret-test-secret"),
	})
	if err != nil {
		t.Fatalf("auth.NewManager: %v", err)
	}
	now := timestamppb.New(time.Date(2026, 6, 10, 10, 0, 0, 0, time.UTC))
	auditLog := audit.NewMemoryLogger()
	fake := &fakeAdmin{
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
			{Id: "a1", TenantId: "t1", ChannelType: "zoho_cliq", Provider: "default", DisplayName: "Cliq prod", CreatedAt: now, RateLimitPerSecond: 10, RateLimitScope: "account"},
		},
		stream: &fakeStream{messages: []*adminv1.TailMessagesResponse{
			{Id: "m1", TenantId: "t1", AccountId: "a1", Text: "hello", ReceivedAt: now},
		}},
	}
	return New(Config{
		Auth:   manager,
		Admin:  fake,
		Audit:  auditLog,
		Assets: http.NotFoundHandler(),
	}), auditLog, fake
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
