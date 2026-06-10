package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"
	"github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1/adminv1connect"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"

	"github.com/crashchat-ai/mio/pkg/channels"
	"github.com/crashchat-ai/mio/services/gateway/internal/crypto"
	"github.com/crashchat-ai/mio/services/gateway/store"
	"github.com/crashchat-ai/mio/services/gateway/store/migrations"
)

// ── Stubs ─────────────────────────────────────────────────────────────────

// stubChannelType is unique to these tests so we don't collide with the
// real cliq adapter registration and we can build a Registry slice without
// touching the global registry.
const stubChannelType = "stub_admin_test"

// stubCredentialAdapter records arguments and returns canned credentials.
type stubCredentialAdapter struct {
	authorizeCalls atomic.Int32
	exchangeCalls  atomic.Int32
	refreshCalls   atomic.Int32

	exchangeErr  error
	exchangeCred channels.Credential

	refreshErr  error
	refreshCred channels.Credential
}

func (s *stubCredentialAdapter) AuthorizeURL(state string) string {
	s.authorizeCalls.Add(1)
	if state == "" {
		return ""
	}
	return "https://stub.example/authorize?state=" + state
}

func (s *stubCredentialAdapter) ExchangeCode(_ context.Context, _ string) (channels.Credential, error) {
	s.exchangeCalls.Add(1)
	if s.exchangeErr != nil {
		return channels.Credential{}, s.exchangeErr
	}
	return s.exchangeCred, nil
}

func (s *stubCredentialAdapter) RefreshCredential(_ context.Context, _ channels.Credential) (channels.Credential, error) {
	s.refreshCalls.Add(1)
	if s.refreshErr != nil {
		return channels.Credential{}, s.refreshErr
	}
	return s.refreshCred, nil
}

// stubAdapter is the minimal channels.Adapter the admin server needs for
// these tests. Send/Edit are unused (admin path).
type stubAdapter struct {
	creds *stubCredentialAdapter
}

func newStubAdapter() *stubAdapter {
	return &stubAdapter{creds: &stubCredentialAdapter{}}
}

func (a *stubAdapter) Send(_ context.Context, _ *miov1.SendCommand) (string, error) {
	return "", errors.New("stub: Send not used in admin tests")
}
func (a *stubAdapter) Edit(_ context.Context, _ *miov1.SendCommand) error {
	return errors.New("stub: Edit not used in admin tests")
}
func (a *stubAdapter) ChannelType() string                      { return stubChannelType }
func (a *stubAdapter) MaxDeliver() int                          { return 5 }
func (a *stubAdapter) RateLimitKey(_ *miov1.SendCommand) string { return "" }
func (a *stubAdapter) Inbound() channels.InboundAdapter         { return nil }
func (a *stubAdapter) Credentials() channels.CredentialAdapter  { return a.creds }
func (a *stubAdapter) Capabilities() *miov1.ChannelCapabilities {
	return &miov1.ChannelCapabilities{AuthKind: "oauth2_refresh"}
}

// ── Test rig ──────────────────────────────────────────────────────────────

type testRig struct {
	pool    *pgxpool.Pool
	server  *AdminServer
	adapter *stubAdapter
	http    *httptest.Server
	client  adminv1connect.AdminServiceClient
}

func newTestRig(t *testing.T) (*testRig, func()) {
	t.Helper()
	dsn := os.Getenv("MIO_TEST_DSN")
	if dsn == "" {
		t.Skip("MIO_TEST_DSN not set; skipping admin server integration tests")
	}
	store.MigrationsFS = migrations.FS
	if err := store.MigrateUp(dsn); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := store.NewPool(ctx, dsn, 4)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}

	adapter := newStubAdapter()
	srv := NewServer(Deps{
		Pool:      pool,
		Cipher:    crypto.NewNoopCipher("dev"),
		Registry:  []channels.Adapter{adapter},
		PublicURL: "http://127.0.0.1:9999", // placeholder, replaced after httptest binds
	})

	path, handler := adminv1connect.NewAdminServiceHandler(srv)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	mux.Handle("/oauth/callback", OAuthCallbackHandler(srv, nil))
	httpSrv := httptest.NewServer(mux)
	srv.publicURL = httpSrv.URL

	client := adminv1connect.NewAdminServiceClient(http.DefaultClient, httpSrv.URL)
	rig := &testRig{
		pool:    pool,
		server:  srv,
		adapter: adapter,
		http:    httpSrv,
		client:  client,
	}
	cleanup := func() {
		httpSrv.Close()
		_, _ = pool.Exec(context.Background(),
			`TRUNCATE TABLE credentials, installs, messages, conversations, accounts, tenants CASCADE`)
		pool.Close()
	}
	return rig, cleanup
}

// ── Tests ─────────────────────────────────────────────────────────────────

func TestAdminServer_CreateThenListThenGetTenant(t *testing.T) {
	rig, cleanup := newTestRig(t)
	defer cleanup()

	slug := "rig-tenant-" + uuid.New().String()[:8]
	created, err := rig.client.CreateTenant(context.Background(),
		connect.NewRequest(&adminv1.CreateTenantRequest{Slug: slug, DisplayName: "Rig Co"}))
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	if created.Msg.GetTenant().GetSlug() != slug {
		t.Errorf("slug: %q", created.Msg.GetTenant().GetSlug())
	}
	tID := created.Msg.GetTenant().GetId()

	listed, err := rig.client.ListTenants(context.Background(),
		connect.NewRequest(&adminv1.ListTenantsRequest{}))
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	found := false
	for _, t := range listed.Msg.GetTenants() {
		if t.GetId() == tID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("created tenant not in list")
	}

	got, err := rig.client.GetTenant(context.Background(),
		connect.NewRequest(&adminv1.GetTenantRequest{Id: tID}))
	if err != nil {
		t.Fatalf("GetTenant: %v", err)
	}
	if got.Msg.GetTenant().GetSlug() != slug {
		t.Errorf("GetTenant slug: %q", got.Msg.GetTenant().GetSlug())
	}

	// Missing-id branch.
	_, err = rig.client.GetTenant(context.Background(),
		connect.NewRequest(&adminv1.GetTenantRequest{Id: uuid.New().String()}))
	if err == nil {
		t.Fatal("expected NotFound for unknown tenant")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", connect.CodeOf(err))
	}
}

func TestAdminServer_StartInstall_PersistsPendingRow(t *testing.T) {
	rig, cleanup := newTestRig(t)
	defer cleanup()
	ctx := context.Background()

	tID := uuid.New()
	if _, err := store.EnsureTenant(ctx, rig.pool, tID,
		"si-tenant-"+tID.String()[:8], "SI Co"); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	resp, err := rig.client.StartInstall(ctx, connect.NewRequest(&adminv1.StartInstallRequest{
		TenantId:    tID.String(),
		ChannelType: stubChannelType,
		Provider:    "default",
	}))
	if err != nil {
		t.Fatalf("StartInstall: %v", err)
	}
	if resp.Msg.GetOauthUrl() == "" {
		t.Error("oauth_url empty")
	}
	if resp.Msg.GetRedirectUri() != rig.http.URL+"/oauth/callback" {
		t.Errorf("redirect_uri: %q", resp.Msg.GetRedirectUri())
	}
	installID := resp.Msg.GetInstallId()
	installUUID, err := uuid.Parse(installID)
	if err != nil {
		t.Fatalf("install_id not a UUID: %v", err)
	}

	// installs row: state=pending, account_id == installID (placeholder
	// account created with the same UUID by StartInstall).
	var state string
	var accountID uuid.UUID
	if err := rig.pool.QueryRow(ctx,
		`SELECT state, account_id FROM installs WHERE id=$1`, installUUID).
		Scan(&state, &accountID); err != nil {
		t.Fatalf("read installs row: %v", err)
	}
	if state != "pending" {
		t.Errorf("install state: %q, want pending", state)
	}
	if accountID != installUUID {
		t.Errorf("account_id (%s) != install_id (%s)", accountID, installUUID)
	}

	// placeholder account row: external_id has "pending:" prefix; provider matches.
	acct, err := store.GetAccount(ctx, rig.pool, installUUID)
	if err != nil {
		t.Fatalf("get placeholder account: %v", err)
	}
	if acct.ChannelType != stubChannelType {
		t.Errorf("channel_type: %q", acct.ChannelType)
	}
	if acct.Provider != "default" {
		t.Errorf("provider: %q", acct.Provider)
	}
	if got := acct.ExternalID; got == "" || got[:8] != "pending:" {
		t.Errorf("external_id should start with pending: prefix; got %q", got)
	}

	// Stash carries the state nonce — capture must succeed.
	u, _ := url.Parse(resp.Msg.GetOauthUrl())
	stateNonce := u.Query().Get("state")
	if stateNonce == "" {
		t.Fatal("authorize URL missing state nonce")
	}
	gotInstallID := rig.server.Stash().capture(stateNonce, "code-from-callback")
	if gotInstallID != installID {
		t.Errorf("stash capture: got %q want %q", gotInstallID, installID)
	}
}

func TestAdminServer_StartInstall_UnknownChannelType_NotFound(t *testing.T) {
	rig, cleanup := newTestRig(t)
	defer cleanup()
	ctx := context.Background()
	tID := uuid.New()
	if _, err := store.EnsureTenant(ctx, rig.pool, tID,
		"ucht-"+tID.String()[:8], ""); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	_, err := rig.client.StartInstall(ctx, connect.NewRequest(&adminv1.StartInstallRequest{
		TenantId:    tID.String(),
		ChannelType: "no_such_channel",
	}))
	if err == nil {
		t.Fatal("expected NotFound")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("code: %v", connect.CodeOf(err))
	}
}

func TestAdminServer_OAuthCallback_HappyAndErrors(t *testing.T) {
	rig, cleanup := newTestRig(t)
	defer cleanup()

	// Pre-reserve a state nonce as StartInstall would.
	installID := uuid.New().String()
	state := "state-cb-" + uuid.New().String()[:8]
	rig.server.Stash().reserve(installID, state)

	// Happy path.
	resp, err := http.Get(rig.http.URL + "/oauth/callback?state=" + state + "&code=code-1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("happy callback status: %d", resp.StatusCode)
	}
	resp.Body.Close() //nolint:errcheck

	// Stash now has the code under installID.
	sc, ok := rig.server.Stash().consume(installID)
	if !ok {
		t.Fatal("stash should have captured code via callback")
	}
	if sc.code != "code-1" {
		t.Errorf("captured code: %q", sc.code)
	}

	// Bad state → 400.
	resp, err = http.Get(rig.http.URL + "/oauth/callback?state=never-reserved&code=x")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad-state status: %d", resp.StatusCode)
	}
	resp.Body.Close() //nolint:errcheck

	// Missing params → 400.
	resp, err = http.Get(rig.http.URL + "/oauth/callback?state=only-state")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing-code status: %d", resp.StatusCode)
	}
	resp.Body.Close() //nolint:errcheck

	resp, err = http.Get(rig.http.URL + "/oauth/callback")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("no-params status: %d", resp.StatusCode)
	}
	resp.Body.Close() //nolint:errcheck
}

func TestAdminServer_CompleteInstall_Happy(t *testing.T) {
	rig, cleanup := newTestRig(t)
	defer cleanup()
	ctx := context.Background()

	// Seed: tenant + StartInstall to allocate the placeholder account + install row.
	tID := uuid.New()
	if _, err := store.EnsureTenant(ctx, rig.pool, tID,
		"ci-tenant-"+tID.String()[:8], ""); err != nil {
		t.Fatalf("seed: %v", err)
	}
	si, err := rig.client.StartInstall(ctx, connect.NewRequest(&adminv1.StartInstallRequest{
		TenantId: tID.String(), ChannelType: stubChannelType,
	}))
	if err != nil {
		t.Fatalf("StartInstall: %v", err)
	}
	installID := si.Msg.GetInstallId()

	// Drive callback to stash the code.
	u, _ := url.Parse(si.Msg.GetOauthUrl())
	stateNonce := u.Query().Get("state")
	if got := rig.server.Stash().capture(stateNonce, "code-happy"); got != installID {
		t.Fatalf("stash capture mismatch")
	}
	// Re-reserve since capture consumed the state mapping; but consume happens
	// inside CompleteInstall, so we need the byID entry. Re-stash manually
	// using the same primitives:
	rig.server.Stash().reserve(installID, stateNonce)
	rig.server.Stash().capture(stateNonce, "code-happy")

	// Tell the stub adapter what credential to return.
	rig.adapter.creds.exchangeCred = channels.Credential{
		AccessToken:  "access-from-stub",
		RefreshToken: "refresh-from-stub",
		ExpiresAt:    time.Now().Add(time.Hour),
		Extras:       map[string]string{"api_domain": "https://stub.example"},
	}

	resp, err := rig.client.CompleteInstall(ctx, connect.NewRequest(&adminv1.CompleteInstallRequest{
		InstallId: installID,
	}))
	if err != nil {
		t.Fatalf("CompleteInstall: %v", err)
	}
	if rig.adapter.creds.exchangeCalls.Load() != 1 {
		t.Errorf("stub.ExchangeCode call count: %d", rig.adapter.creds.exchangeCalls.Load())
	}
	if resp.Msg.GetAccount().GetTenantId() != tID.String() {
		t.Errorf("account tenant: %q", resp.Msg.GetAccount().GetTenantId())
	}

	// installs.state flipped to active.
	var st string
	if err := rig.pool.QueryRow(ctx, `SELECT state FROM installs WHERE id=$1`, installID).
		Scan(&st); err != nil {
		t.Fatalf("read installs: %v", err)
	}
	if st != "active" {
		t.Errorf("installs.state: %q want active", st)
	}

	// credentials row written with NoopCipher key_version=0.
	var kv int
	if err := rig.pool.QueryRow(ctx,
		`SELECT key_version FROM credentials WHERE account_id=$1`, installID).
		Scan(&kv); err != nil {
		t.Fatalf("read credentials: %v", err)
	}
	if kv != 0 {
		t.Errorf("credentials.key_version: %d", kv)
	}
}

func TestAdminServer_CompleteInstall_UnknownInstallID(t *testing.T) {
	rig, cleanup := newTestRig(t)
	defer cleanup()
	_, err := rig.client.CompleteInstall(context.Background(),
		connect.NewRequest(&adminv1.CompleteInstallRequest{InstallId: uuid.New().String()}))
	if err == nil {
		t.Fatal("expected error for unknown install_id")
	}
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("code: %v", connect.CodeOf(err))
	}
}

func TestAdminServer_CompleteInstall_ExpiredCode(t *testing.T) {
	rig, cleanup := newTestRig(t)
	defer cleanup()

	// Reserve + capture with a clock pinned to "way ago" so the entry is born expired.
	installID := uuid.New().String()
	rig.server.Stash().reserve(installID, "state-exp")
	// Use the stash's clock injector to expire the captured code on the spot.
	now := time.Now()
	rig.server.Stash().clock = func() time.Time { return now.Add(-installStashTTL - time.Hour) }
	rig.server.Stash().capture("state-exp", "code-expired")
	// Now restore clock to "now" so consume sees the entry as expired.
	rig.server.Stash().clock = func() time.Time { return now }

	_, err := rig.client.CompleteInstall(context.Background(),
		connect.NewRequest(&adminv1.CompleteInstallRequest{InstallId: installID}))
	if err == nil {
		t.Fatal("expected FailedPrecondition for expired/missing code")
	}
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("code: %v", connect.CodeOf(err))
	}
}

func TestAdminServer_DisableAccount_Idempotent(t *testing.T) {
	rig, cleanup := newTestRig(t)
	defer cleanup()
	ctx := context.Background()

	tID := uuid.New()
	if _, err := store.EnsureTenant(ctx, rig.pool, tID,
		"da-"+tID.String()[:8], ""); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	aID := uuid.New()
	if _, err := store.CreateAccount(ctx, rig.pool, aID, tID, stubChannelType,
		"default", "ext-da", "Disable Me", nil); err != nil {
		t.Fatalf("seed account: %v", err)
	}

	// First disable — row updates.
	if _, err := rig.client.DisableAccount(ctx,
		connect.NewRequest(&adminv1.DisableAccountRequest{AccountId: aID.String()})); err != nil {
		t.Fatalf("DisableAccount: %v", err)
	}
	acct, err := store.GetAccount(ctx, rig.pool, aID)
	if err != nil {
		t.Fatalf("read acct: %v", err)
	}
	if acct.DisabledAt == nil {
		t.Fatal("disabled_at not set")
	}

	// Second disable — idempotent, no error.
	if _, err := rig.client.DisableAccount(ctx,
		connect.NewRequest(&adminv1.DisableAccountRequest{AccountId: aID.String()})); err != nil {
		t.Fatalf("DisableAccount idempotent: %v", err)
	}
}

func TestAdminServer_AccountMutationRPCsAndCredentialMetadata(t *testing.T) {
	rig, cleanup := newTestRig(t)
	defer cleanup()
	ctx := context.Background()

	tID := uuid.New()
	if _, err := store.EnsureTenant(ctx, rig.pool, tID,
		"acct-rpc-"+tID.String()[:8], ""); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	aID := uuid.New()
	if _, err := store.CreateAccount(ctx, rig.pool, aID, tID, stubChannelType,
		"default", "ext-before", "Before", nil); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	if err := store.PutCredential(ctx, rig.pool, crypto.NewNoopCipher("dev"),
		aID, "oauth2_refresh", store.CredentialPayload{AccessToken: "plaintext-token"}); err != nil {
		t.Fatalf("seed credential: %v", err)
	}

	got, err := rig.client.GetAccount(ctx,
		connect.NewRequest(&adminv1.GetAccountRequest{AccountId: aID.String()}))
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if got.Msg.GetAccount().GetExternalId() != "ext-before" {
		t.Fatalf("account shape: %+v", got.Msg.GetAccount())
	}

	updated, err := rig.client.UpdateAccount(ctx,
		connect.NewRequest(&adminv1.UpdateAccountRequest{
			AccountId:   aID.String(),
			DisplayName: "After",
			ExternalId:  "ext-after",
		}))
	if err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}
	if updated.Msg.GetAccount().GetDisplayName() != "After" || updated.Msg.GetAccount().GetExternalId() != "ext-after" {
		t.Fatalf("updated account: %+v", updated.Msg.GetAccount())
	}

	limited, err := rig.client.SetRateLimit(ctx,
		connect.NewRequest(&adminv1.SetRateLimitRequest{
			AccountId:          aID.String(),
			RateLimitPerSecond: 9,
		}))
	if err != nil {
		t.Fatalf("SetRateLimit: %v", err)
	}
	if limited.Msg.GetAccount().GetRateLimitPerSecond() != 9 || limited.Msg.GetAccount().GetRateLimitScope() != "account" {
		t.Fatalf("rate limit account: %+v", limited.Msg.GetAccount())
	}

	meta, err := rig.client.GetCredentialMetadata(ctx,
		connect.NewRequest(&adminv1.GetCredentialMetadataRequest{AccountId: aID.String()}))
	if err != nil {
		t.Fatalf("GetCredentialMetadata: %v", err)
	}
	if !meta.Msg.GetHasCredential() || meta.Msg.GetAuthKind() != "oauth2_refresh" {
		t.Fatalf("metadata: %+v", meta.Msg)
	}
}

func TestAdminServer_RotateCredential_WritesNewKeyVersion(t *testing.T) {
	rig, cleanup := newTestRig(t)
	defer cleanup()
	ctx := context.Background()

	// Seed tenant + account + credential under NoopCipher (kv=0).
	tID := uuid.New()
	if _, err := store.EnsureTenant(ctx, rig.pool, tID,
		"rot-"+tID.String()[:8], ""); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	aID := uuid.New()
	if _, err := store.CreateAccount(ctx, rig.pool, aID, tID, stubChannelType,
		"default", "ext-rot", "Rot Me", nil); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	if err := store.PutCredential(ctx, rig.pool, crypto.NewNoopCipher("dev"),
		aID, "oauth2_refresh", store.CredentialPayload{
			AccessToken:  "ax-old",
			RefreshToken: "rt-old",
			ExpiresAt:    time.Now().Add(time.Hour),
		}); err != nil {
		t.Fatalf("seed credential: %v", err)
	}

	// Stub returns a fresh access token; server re-encrypts under the
	// configured cipher (Noop in this rig). Asserting the call happened
	// AND key_version landed unchanged validates the flow end-to-end.
	rig.adapter.creds.refreshCred = channels.Credential{
		AccessToken:  "ax-new",
		RefreshToken: "rt-new",
		ExpiresAt:    time.Now().Add(time.Hour),
	}

	if _, err := rig.client.RotateCredential(ctx,
		connect.NewRequest(&adminv1.RotateCredentialRequest{AccountId: aID.String()})); err != nil {
		t.Fatalf("RotateCredential: %v", err)
	}
	if rig.adapter.creds.refreshCalls.Load() != 1 {
		t.Errorf("stub.RefreshCredential call count: %d", rig.adapter.creds.refreshCalls.Load())
	}

	// Read back; access token should be the rotated value, key_version is
	// whatever the rig cipher reports.
	row, err := store.GetCredential(ctx, rig.pool, crypto.NewNoopCipher("dev"), aID, nil)
	if err != nil {
		t.Fatalf("get credential: %v", err)
	}
	if row.Plaintext.AccessToken != "ax-new" {
		t.Errorf("access not rotated: %q", row.Plaintext.AccessToken)
	}
	if row.KeyVersion != 0 {
		t.Errorf("key_version: %d (Noop reports 0)", row.KeyVersion)
	}
}

func TestAdminServer_RotateCredential_NoCredentialErrors(t *testing.T) {
	rig, cleanup := newTestRig(t)
	defer cleanup()
	ctx := context.Background()

	// Account without credentials yet.
	tID := uuid.New()
	if _, err := store.EnsureTenant(ctx, rig.pool, tID,
		"nocr-"+tID.String()[:8], ""); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	aID := uuid.New()
	if _, err := store.CreateAccount(ctx, rig.pool, aID, tID, stubChannelType,
		"default", "ext-nocr", "n", nil); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	_, err := rig.client.RotateCredential(ctx,
		connect.NewRequest(&adminv1.RotateCredentialRequest{AccountId: aID.String()}))
	if err == nil {
		t.Fatal("expected NotFound for missing credential")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("code: %v", connect.CodeOf(err))
	}
}
