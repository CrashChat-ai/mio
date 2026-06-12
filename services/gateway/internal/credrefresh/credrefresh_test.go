package credrefresh

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"github.com/crashchat-ai/mio/services/gateway/internal/crypto"
	"github.com/crashchat-ai/mio/services/gateway/store"
	"github.com/crashchat-ai/mio/services/gateway/store/migrations"
)

type fakeCreds struct {
	fresh   channels.Credential
	err     error
	calls   int
	lastCur channels.Credential
}

func (f *fakeCreds) AuthorizeURL(string) string { return "" }
func (f *fakeCreds) ExchangeCode(context.Context, string) (channels.Credential, error) {
	return channels.Credential{}, errors.New("unused")
}
func (f *fakeCreds) RefreshCredential(_ context.Context, cur channels.Credential) (channels.Credential, error) {
	f.calls++
	f.lastCur = cur
	if f.err != nil {
		return channels.Credential{}, f.err
	}
	return f.fresh, nil
}

type fakeRefreshAdapter struct {
	slug  string
	creds *fakeCreds
}

func (a *fakeRefreshAdapter) Send(context.Context, *miov1.SendCommand) (string, error) {
	return "", nil
}
func (a *fakeRefreshAdapter) Edit(context.Context, *miov1.SendCommand) error { return nil }
func (a *fakeRefreshAdapter) ChannelType() string                            { return a.slug }
func (a *fakeRefreshAdapter) MaxDeliver() int                                { return 5 }
func (a *fakeRefreshAdapter) RateLimitKey(*miov1.SendCommand) string         { return "" }
func (a *fakeRefreshAdapter) Capabilities() *miov1.ChannelCapabilities {
	return &miov1.ChannelCapabilities{}
}
func (a *fakeRefreshAdapter) Inbound() channels.InboundAdapter        { return nil }
func (a *fakeRefreshAdapter) Credentials() channels.CredentialAdapter { return a.creds }

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("MIO_TEST_DSN")
	if dsn == "" {
		t.Skip("MIO_TEST_DSN not set; skipping credrefresh integration tests")
	}
	store.MigrationsFS = migrations.FS
	if err := store.MigrateUp(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seedAccountWithCredential(t *testing.T, pool *pgxpool.Pool, cipher crypto.Cipher, slug string, expiresIn time.Duration) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	tenant, err := store.EnsureTenant(ctx, pool, uuid.New(), "cr-"+slug, "CredRefresh "+slug)
	if err != nil {
		t.Fatal(err)
	}
	acct, err := store.CreateAccount(ctx, pool, uuid.New(), tenant.ID, slug, "default", "ws-"+slug, slug, nil)
	if err != nil {
		t.Fatal(err)
	}
	payload := store.CredentialPayload{
		AccessToken:  "old-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(expiresIn),
	}
	if err := store.PutCredential(ctx, pool, cipher, acct.ID, "oauth2_refresh", payload); err != nil {
		t.Fatal(err)
	}
	return acct.ID
}

func TestRefreshExpiring_RotatesExpiringCredential(t *testing.T) {
	pool := testPool(t)
	cipher := crypto.NewNoopCipher("dev")
	ctx := context.Background()

	acctID := seedAccountWithCredential(t, pool, cipher, "cr_exp", 5*time.Minute)
	newExpiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	fc := &fakeCreds{fresh: channels.Credential{
		AccessToken:  "new-token",
		RefreshToken: "refresh-token-2",
		ExpiresAt:    newExpiry,
	}}

	r := New(pool, cipher, []channels.Adapter{&fakeRefreshAdapter{slug: "cr_exp", creds: fc}},
		0, 30*time.Minute, nil, prometheus.NewRegistry())
	r.RefreshExpiring(ctx)

	if fc.calls != 1 {
		t.Fatalf("want 1 refresh call, got %d", fc.calls)
	}
	if fc.lastCur.RefreshToken != "refresh-token" {
		t.Errorf("refresh must receive stored refresh token, got %q", fc.lastCur.RefreshToken)
	}
	row, err := store.GetCredential(ctx, pool, cipher, acctID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if row.Plaintext.AccessToken != "new-token" || row.Plaintext.RefreshToken != "refresh-token-2" {
		t.Errorf("credential not rotated: %+v", row.Plaintext)
	}
}

func TestRefreshExpiring_SkipsNonExpiring(t *testing.T) {
	pool := testPool(t)
	cipher := crypto.NewNoopCipher("dev")

	seedAccountWithCredential(t, pool, cipher, "cr_far", 24*time.Hour)
	fc := &fakeCreds{}

	r := New(pool, cipher, []channels.Adapter{&fakeRefreshAdapter{slug: "cr_far", creds: fc}},
		0, 30*time.Minute, nil, prometheus.NewRegistry())
	r.RefreshExpiring(context.Background())

	if fc.calls != 0 {
		t.Fatalf("non-expiring credential must not refresh, got %d calls", fc.calls)
	}
}

func TestRefreshExpiring_ErrorRetriesNextTick(t *testing.T) {
	pool := testPool(t)
	cipher := crypto.NewNoopCipher("dev")
	ctx := context.Background()

	acctID := seedAccountWithCredential(t, pool, cipher, "cr_err", 5*time.Minute)
	fc := &fakeCreds{err: errors.New("provider 500")}

	r := New(pool, cipher, []channels.Adapter{&fakeRefreshAdapter{slug: "cr_err", creds: fc}},
		0, 30*time.Minute, nil, prometheus.NewRegistry())
	r.RefreshExpiring(ctx)
	r.RefreshExpiring(ctx)

	if fc.calls != 2 {
		t.Fatalf("failed refresh must retry next tick, got %d calls", fc.calls)
	}
	row, err := store.GetCredential(ctx, pool, cipher, acctID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if row.Plaintext.AccessToken != "old-token" {
		t.Errorf("failed refresh must not mutate credential, got %+v", row.Plaintext)
	}
}
