package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crashchat-ai/mio/services/gateway/internal/crypto"
	"github.com/crashchat-ai/mio/services/gateway/store/migrations"
)

// requirePool returns a live pool against MIO_TEST_DSN or skips. Migrations
// are applied so a fresh test database lands at the same schema as a
// freshly-deployed gateway. Cleanup truncates touched tables.
func requirePool(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	dsn := os.Getenv("MIO_TEST_DSN")
	if dsn == "" {
		t.Skip("MIO_TEST_DSN not set; skipping admin-store integration tests")
	}
	MigrationsFS = migrations.FS
	if err := MigrateUp(dsn); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := NewPool(ctx, dsn, 4)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	cleanup := func() {
		// Truncate in FK-safe order; identical to integration_test cleanup pattern.
		_, _ = pool.Exec(context.Background(), `
			TRUNCATE TABLE credentials, installs, messages, conversations, accounts, tenants CASCADE`)
		pool.Close()
	}
	return pool, cleanup
}

func newTenantID(t *testing.T, slug string) (uuid.UUID, string) {
	t.Helper()
	id := uuid.New()
	// Suffix slug with a short uuid fragment so re-running the test suite
	// against an existing DB doesn't trip uniqueness.
	return id, fmt.Sprintf("%s-%s", slug, id.String()[:8])
}

func TestTenants_RoundTrip(t *testing.T) {
	pool, cleanup := requirePool(t)
	defer cleanup()
	ctx := context.Background()

	id, slug := newTenantID(t, "tenant-roundtrip")
	tn, err := EnsureTenant(ctx, pool, id, slug, "Round Trip Co")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if tn.ID != id || tn.Slug != slug || tn.DisplayName != "Round Trip Co" {
		t.Errorf("ensure shape: %+v", tn)
	}

	list, err := ListTenants(ctx, pool)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, x := range list {
		if x.ID == id {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("inserted tenant not in list")
	}

	if err := DisableTenant(ctx, pool, id); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, err := GetTenant(ctx, pool, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DisabledAt == nil {
		t.Errorf("disabled_at not set")
	}
}

func TestAccounts_UniqueConstraintFourCol(t *testing.T) {
	pool, cleanup := requirePool(t)
	defer cleanup()
	ctx := context.Background()

	tid, slug := newTenantID(t, "acct-unique")
	if _, err := EnsureTenant(ctx, pool, tid, slug, "Acct Co"); err != nil {
		t.Fatalf("tenant: %v", err)
	}

	a1id := uuid.New()
	if _, err := CreateAccount(ctx, pool, a1id, tid, "zoho_cliq", "default", "ext-1", "Display 1", nil); err != nil {
		t.Fatalf("a1: %v", err)
	}

	// Same (tenant, channel_type, provider, external_id) → uniqueness trip.
	a2id := uuid.New()
	_, err := CreateAccount(ctx, pool, a2id, tid, "zoho_cliq", "default", "ext-1", "Display 2", nil)
	if err == nil {
		t.Fatal("expected uniqueness violation")
	}
	if !strings.Contains(err.Error(), "duplicate") && !strings.Contains(err.Error(), "unique") {
		t.Errorf("err should indicate uniqueness: %v", err)
	}

	// Different provider on the same (tenant, channel, external_id) — should succeed.
	a3id := uuid.New()
	if _, err := CreateAccount(ctx, pool, a3id, tid, "zoho_cliq", "alt", "ext-1", "Display 3", nil); err != nil {
		t.Fatalf("a3 (different provider): %v", err)
	}
}

func TestAccounts_ListByTenant(t *testing.T) {
	pool, cleanup := requirePool(t)
	defer cleanup()
	ctx := context.Background()

	tid, slug := newTenantID(t, "acct-list")
	if _, err := EnsureTenant(ctx, pool, tid, slug, ""); err != nil {
		t.Fatalf("tenant: %v", err)
	}
	for i, name := range []string{"a", "b", "c"} {
		_, err := CreateAccount(ctx, pool, uuid.New(), tid, "zoho_cliq", "default",
			fmt.Sprintf("ext-%d", i), name, nil)
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	list, err := ListAccounts(ctx, pool, tid)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("len: %d, want 3", len(list))
	}
}

func TestAccounts_UpdateAndRateLimit(t *testing.T) {
	pool, cleanup := requirePool(t)
	defer cleanup()
	ctx := context.Background()

	tid, slug := newTenantID(t, "acct-update")
	if _, err := EnsureTenant(ctx, pool, tid, slug, ""); err != nil {
		t.Fatalf("tenant: %v", err)
	}
	aid := uuid.New()
	if _, err := CreateAccount(ctx, pool, aid, tid, "zoho_cliq", "default", "ext-before", "Before", nil); err != nil {
		t.Fatalf("acct: %v", err)
	}

	updated, err := UpdateAccount(ctx, pool, aid, "After", "ext-after")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.DisplayName != "After" || updated.ExternalID != "ext-after" {
		t.Fatalf("updated account: %+v", updated)
	}

	limited, err := SetAccountRateLimit(ctx, pool, aid, 12, "account")
	if err != nil {
		t.Fatalf("rate limit: %v", err)
	}
	if limited.RateLimitPerSecond != 12 || limited.RateLimitScope != "account" {
		t.Fatalf("rate limit account: %+v", limited)
	}

	cleared, err := SetAccountRateLimit(ctx, pool, aid, 0, "")
	if err != nil {
		t.Fatalf("clear rate limit: %v", err)
	}
	if cleared.RateLimitPerSecond != 0 || cleared.RateLimitScope != "" {
		t.Fatalf("cleared rate limit: %+v", cleared)
	}
}

func TestCredentials_RoundTrip(t *testing.T) {
	pool, cleanup := requirePool(t)
	defer cleanup()
	ctx := context.Background()

	tid, slug := newTenantID(t, "cred-roundtrip")
	if _, err := EnsureTenant(ctx, pool, tid, slug, ""); err != nil {
		t.Fatalf("tenant: %v", err)
	}
	aid := uuid.New()
	if _, err := CreateAccount(ctx, pool, aid, tid, "zoho_cliq", "default", "ext-cred", "n", nil); err != nil {
		t.Fatalf("acct: %v", err)
	}

	cipher := crypto.NewNoopCipher("dev")
	payload := CredentialPayload{
		AccessToken:  "access-tok-1",
		RefreshToken: "refresh-tok-1",
		ExpiresAt:    time.Now().Add(time.Hour).UTC().Truncate(time.Second),
		Extras:       map[string]string{"api_domain": "https://www.zohoapis.com"},
	}
	if err := PutCredential(ctx, pool, cipher, aid, "oauth2_refresh", payload); err != nil {
		t.Fatalf("put: %v", err)
	}

	row, err := GetCredential(ctx, pool, cipher, aid, nil)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if row.AuthKind != "oauth2_refresh" {
		t.Errorf("auth_kind: %q", row.AuthKind)
	}
	if row.Plaintext.AccessToken != payload.AccessToken {
		t.Errorf("access mismatch: %q vs %q", row.Plaintext.AccessToken, payload.AccessToken)
	}
	if row.Plaintext.RefreshToken != payload.RefreshToken {
		t.Errorf("refresh mismatch")
	}
	if row.Plaintext.Extras["api_domain"] != "https://www.zohoapis.com" {
		t.Errorf("extras: %+v", row.Plaintext.Extras)
	}

	meta, err := GetCredentialMetadata(ctx, pool, aid)
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	if meta.AccountID != aid || meta.AuthKind != "oauth2_refresh" || meta.KeyVersion != int32(cipher.KeyVersion()) {
		t.Fatalf("metadata shape: %+v", meta)
	}
	if meta.ExpiresAt == nil || meta.RotatedAt.IsZero() {
		t.Fatalf("metadata timestamps: %+v", meta)
	}
}

func TestCredentials_NotFound(t *testing.T) {
	pool, cleanup := requirePool(t)
	defer cleanup()
	ctx := context.Background()

	_, err := GetCredential(ctx, pool, crypto.NewNoopCipher("dev"), uuid.New(), nil)
	if !errors.Is(err, ErrCredentialNotFound) {
		t.Errorf("expected ErrCredentialNotFound, got %v", err)
	}
}

func TestCredentials_KeyVersionTracked(t *testing.T) {
	pool, cleanup := requirePool(t)
	defer cleanup()
	ctx := context.Background()

	tid, slug := newTenantID(t, "cred-kv")
	if _, err := EnsureTenant(ctx, pool, tid, slug, ""); err != nil {
		t.Fatalf("tenant: %v", err)
	}
	aid := uuid.New()
	if _, err := CreateAccount(ctx, pool, aid, tid, "zoho_cliq", "default", "ext-kv", "n", nil); err != nil {
		t.Fatalf("acct: %v", err)
	}

	// Put using NoopCipher (KeyVersion=0).
	noop := crypto.NewNoopCipher("dev")
	if err := PutCredential(ctx, pool, noop, aid, "oauth2_refresh",
		CredentialPayload{AccessToken: "x"}); err != nil {
		t.Fatalf("put: %v", err)
	}
	// Direct SQL check: column = 0.
	var stored int
	if err := pool.QueryRow(ctx, "SELECT key_version FROM credentials WHERE account_id=$1", aid).
		Scan(&stored); err != nil {
		t.Fatalf("sql check: %v", err)
	}
	if stored != 0 {
		t.Errorf("key_version stored=%d, want 0", stored)
	}
}

func TestMigrationDownThenUp_PreservesBaseRows(t *testing.T) {
	pool, cleanup := requirePool(t)
	defer cleanup()
	ctx := context.Background()

	tid, slug := newTenantID(t, "mig-test")
	if _, err := EnsureTenant(ctx, pool, tid, slug, "Mig Co"); err != nil {
		t.Fatalf("tenant: %v", err)
	}
	if _, err := CreateAccount(ctx, pool, uuid.New(), tid, "zoho_cliq", "default", "ext-mig", "n", nil); err != nil {
		t.Fatalf("acct: %v", err)
	}

	// Down/up cycle is tested via migrate package directly; this test
	// instead verifies that 000002 idempotency on a populated DB does NOT
	// drop the rows we just inserted (additive migration semantics).
	if err := MigrateUp(os.Getenv("MIO_TEST_DSN")); err != nil {
		t.Fatalf("migrate up (re-run): %v", err)
	}
	got, err := GetTenant(ctx, pool, tid)
	if err != nil {
		t.Errorf("tenant survived: %v", err)
	}
	if got.Slug != slug {
		t.Errorf("slug mismatch")
	}
}
