package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crashchat-ai/mio/services/gateway/internal/crypto"
)

// CredentialPayload is the plaintext shape stored under credentials.ciphertext
// after encryption. Mirrors channels.Credential but lives in store to avoid an
// import cycle (store has no dependency on the gateway-internal sender).
//
// JSON-marshalled before encryption; the cipher sees an opaque blob.
type CredentialPayload struct {
	AccessToken  string            `json:"access_token"`
	RefreshToken string            `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time         `json:"expires_at,omitempty"`
	Extras       map[string]string `json:"extras,omitempty"`
}

// CredentialRow is the persisted shape excluding ciphertext (which is
// decrypted lazily in GetCredential).
type CredentialRow struct {
	AccountID  uuid.UUID
	AuthKind   string
	KeyVersion int
	ExpiresAt  *time.Time
	RotatedAt  time.Time
	Plaintext  CredentialPayload
}

// ErrCredentialNotFound is returned by GetCredential when no row exists.
var ErrCredentialNotFound = errors.New("store: credential not found")

// PutCredential encrypts payload via cipher and upserts into credentials.
// key_version is written from cipher.KeyVersion() verbatim so rotation
// detection is row-level. expires_at column mirrors payload.ExpiresAt
// (admin uses it to schedule refreshes); rotated_at stamps NOW().
func PutCredential(
	ctx context.Context,
	pool *pgxpool.Pool,
	cipher crypto.Cipher,
	accountID uuid.UUID,
	authKind string,
	payload CredentialPayload,
) error {
	plain, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("store: put credential marshal: %w", err)
	}
	cipherText, err := cipher.Encrypt(plain)
	if err != nil {
		return fmt.Errorf("store: put credential encrypt: %w", err)
	}
	var expiresAt *time.Time
	if !payload.ExpiresAt.IsZero() {
		t := payload.ExpiresAt
		expiresAt = &t
	}
	const q = `
INSERT INTO credentials (account_id, auth_kind, ciphertext, key_version, expires_at, rotated_at)
VALUES ($1, $2, $3, $4, $5, NOW())
ON CONFLICT (account_id) DO UPDATE SET
  auth_kind   = EXCLUDED.auth_kind,
  ciphertext  = EXCLUDED.ciphertext,
  key_version = EXCLUDED.key_version,
  expires_at  = EXCLUDED.expires_at,
  rotated_at  = NOW()`
	if _, err := pool.Exec(ctx, q, accountID, authKind, cipherText, cipher.KeyVersion(), expiresAt); err != nil {
		return fmt.Errorf("store: put credential: %w", err)
	}
	return nil
}

// GetCredential reads and decrypts a credential row. Returns
// ErrCredentialNotFound on miss. Logs (does not error) when the stored
// key_version differs from cipher.KeyVersion() — the rotation signal for
// callers + operator dashboards.
func GetCredential(
	ctx context.Context,
	pool *pgxpool.Pool,
	cipher crypto.Cipher,
	accountID uuid.UUID,
	logger *slog.Logger,
) (CredentialRow, error) {
	if logger == nil {
		logger = slog.Default()
	}
	const q = `
SELECT account_id, auth_kind, ciphertext, key_version, expires_at, rotated_at
FROM credentials
WHERE account_id = $1`
	var row CredentialRow
	var cipherText []byte
	err := pool.QueryRow(ctx, q, accountID).Scan(
		&row.AccountID, &row.AuthKind, &cipherText, &row.KeyVersion, &row.ExpiresAt, &row.RotatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CredentialRow{}, ErrCredentialNotFound
		}
		return CredentialRow{}, fmt.Errorf("store: get credential: %w", err)
	}
	if row.KeyVersion != cipher.KeyVersion() {
		logger.Warn("store: credential key_version drift — rotation pending",
			"account_id", accountID,
			"stored_key_version", row.KeyVersion,
			"current_key_version", cipher.KeyVersion())
	}
	plain, err := cipher.Decrypt(cipherText)
	if err != nil {
		return CredentialRow{}, fmt.Errorf("store: get credential decrypt: %w", err)
	}
	if err := json.Unmarshal(plain, &row.Plaintext); err != nil {
		return CredentialRow{}, fmt.Errorf("store: get credential unmarshal: %w", err)
	}
	return row, nil
}

// RotateCredential re-encrypts an existing credential under cipher and
// updates key_version + rotated_at. The plaintext payload is preserved;
// only the encryption envelope changes. Use this to migrate rows from
// keyVersion N to N+1 after deploying a new cipher.
func RotateCredential(
	ctx context.Context,
	pool *pgxpool.Pool,
	cipher crypto.Cipher,
	oldCipher crypto.Cipher,
	accountID uuid.UUID,
) error {
	// Read with old cipher; write with new.
	row, err := GetCredential(ctx, pool, oldCipher, accountID, nil)
	if err != nil {
		return fmt.Errorf("store: rotate credential read: %w", err)
	}
	if err := PutCredential(ctx, pool, cipher, accountID, row.AuthKind, row.Plaintext); err != nil {
		return fmt.Errorf("store: rotate credential write: %w", err)
	}
	return nil
}
