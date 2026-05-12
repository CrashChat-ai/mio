package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/google/uuid"

	"github.com/crashchat-ai/mio/gateway/internal/crypto"
)

// writeTestAgeKey mirrors crypto/cipher_test.writeAgeKey for use by the
// rotation integration test. Kept inline rather than exported so the
// crypto package's test helper does not leak into production builds.
func writeTestAgeKey(t *testing.T) string {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	path := filepath.Join(t.TempDir(), "key.txt")
	body := "# created: test\n# public key: " + id.Recipient().String() + "\n" + id.String() + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return path
}

func TestCredentials_Rotate_NoopToAgeV2(t *testing.T) {
	pool, cleanup := requirePool(t)
	defer cleanup()
	ctx := context.Background()

	tid, slug := newTenantID(t, "cred-rotate")
	if _, err := EnsureTenant(ctx, pool, tid, slug, ""); err != nil {
		t.Fatalf("tenant: %v", err)
	}
	aid := uuid.New()
	if _, err := CreateAccount(ctx, pool, aid, tid, "zoho_cliq", "default", "ext-rot", "n", nil); err != nil {
		t.Fatalf("acct: %v", err)
	}

	old := crypto.NewNoopCipher("dev")
	payload := CredentialPayload{
		AccessToken:  "access-rot-1",
		RefreshToken: "refresh-rot-1",
		ExpiresAt:    time.Now().Add(time.Hour).UTC().Truncate(time.Second),
		Extras:       map[string]string{"api_domain": "https://www.zohoapis.com"},
	}
	if err := PutCredential(ctx, pool, old, aid, "oauth2_refresh", payload); err != nil {
		t.Fatalf("put (noop): %v", err)
	}

	var (
		preKeyVersion int
		preRotatedAt  time.Time
	)
	if err := pool.QueryRow(ctx,
		"SELECT key_version, rotated_at FROM credentials WHERE account_id=$1", aid).
		Scan(&preKeyVersion, &preRotatedAt); err != nil {
		t.Fatalf("pre-rotation read: %v", err)
	}
	if preKeyVersion != 0 {
		t.Errorf("pre-rotation key_version: %d, want 0", preKeyVersion)
	}

	keyPath := writeTestAgeKey(t)
	newCipher, err := crypto.NewAgeFileCipher(keyPath, 2)
	if err != nil {
		t.Fatalf("new age cipher: %v", err)
	}

	// Small sleep so rotated_at advances by at least 1 second (NOW() column
	// resolution is microseconds, but be defensive on slow CI).
	time.Sleep(10 * time.Millisecond)

	if err := RotateCredential(ctx, pool, newCipher, old, aid); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	var (
		postKeyVersion int
		postRotatedAt  time.Time
	)
	if err := pool.QueryRow(ctx,
		"SELECT key_version, rotated_at FROM credentials WHERE account_id=$1", aid).
		Scan(&postKeyVersion, &postRotatedAt); err != nil {
		t.Fatalf("post-rotation read: %v", err)
	}
	if postKeyVersion != 2 {
		t.Errorf("post-rotation key_version: %d, want 2", postKeyVersion)
	}
	if !postRotatedAt.After(preRotatedAt) {
		t.Errorf("rotated_at did not advance: pre=%v post=%v", preRotatedAt, postRotatedAt)
	}

	// Decrypt with the new cipher and compare to the original plaintext.
	row, err := GetCredential(ctx, pool, newCipher, aid, nil)
	if err != nil {
		t.Fatalf("get after rotate: %v", err)
	}
	if row.KeyVersion != 2 {
		t.Errorf("row.KeyVersion: %d, want 2", row.KeyVersion)
	}
	if row.Plaintext.AccessToken != payload.AccessToken {
		t.Errorf("access mismatch after rotate: %q vs %q", row.Plaintext.AccessToken, payload.AccessToken)
	}
	if row.Plaintext.RefreshToken != payload.RefreshToken {
		t.Errorf("refresh mismatch after rotate")
	}
	if row.Plaintext.Extras["api_domain"] != "https://www.zohoapis.com" {
		t.Errorf("extras mismatch: %+v", row.Plaintext.Extras)
	}
}
