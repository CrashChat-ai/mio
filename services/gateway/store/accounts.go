package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Account is the row shape returned by the account store functions.
// Provider distinguishes vendor variants of the same channel_type
// (whatsapp_cloud vs whatsapp_360 under channel_type='whatsapp').
type Account struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	ChannelType string
	Provider    string
	ExternalID  string
	DisplayName string
	Attributes  map[string]string // currently always {}; admin RPCs set
	DisabledAt  *time.Time
	CreatedAt   time.Time
}

// ErrAccountNotFound is returned by GetAccount when the lookup misses.
var ErrAccountNotFound = errors.New("store: account not found")

// CreateAccount inserts an account row. Returns ErrAccountDuplicate if the
// 4-column uniqueness (tenant, channel_type, provider, external_id) trips.
// provider defaults to "default" when empty so callers that don't care
// about multi-vendor channels pass it transparently.
//
// attributes is marshalled to JSONB; pass nil for an empty {}.
func CreateAccount(
	ctx context.Context,
	pool *pgxpool.Pool,
	id, tenantID uuid.UUID,
	channelType, provider, externalID, displayName string,
	attributes map[string]string,
) (Account, error) {
	if provider == "" {
		provider = "default"
	}
	attrsJSON, err := marshalAttrs(attributes)
	if err != nil {
		return Account{}, fmt.Errorf("store: create account attrs: %w", err)
	}
	const q = `
INSERT INTO accounts (id, tenant_id, channel_type, provider, external_id, display_name, attributes)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
RETURNING id, tenant_id, channel_type, provider, external_id, display_name, attributes, disabled_at, created_at`
	var a Account
	var attrsRaw []byte
	if err := pool.QueryRow(ctx, q, id, tenantID, channelType, provider, externalID, displayName, attrsJSON).
		Scan(&a.ID, &a.TenantID, &a.ChannelType, &a.Provider, &a.ExternalID, &a.DisplayName, &attrsRaw, &a.DisabledAt, &a.CreatedAt); err != nil {
		return Account{}, fmt.Errorf("store: create account: %w", err)
	}
	a.Attributes, _ = unmarshalAttrs(attrsRaw)
	return a, nil
}

// ListAccounts returns all accounts for a tenant, newest first.
func ListAccounts(ctx context.Context, pool *pgxpool.Pool, tenantID uuid.UUID) ([]Account, error) {
	const q = `
SELECT id, tenant_id, channel_type, provider, external_id, display_name, attributes, disabled_at, created_at
FROM accounts
WHERE tenant_id = $1
ORDER BY created_at DESC`
	rows, err := pool.Query(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: list accounts: %w", err)
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		var a Account
		var attrsRaw []byte
		if err := rows.Scan(&a.ID, &a.TenantID, &a.ChannelType, &a.Provider, &a.ExternalID, &a.DisplayName, &attrsRaw, &a.DisabledAt, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: list accounts scan: %w", err)
		}
		a.Attributes, _ = unmarshalAttrs(attrsRaw)
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetAccount fetches a single account by id. ErrAccountNotFound on miss.
func GetAccount(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (Account, error) {
	const q = `
SELECT id, tenant_id, channel_type, provider, external_id, display_name, attributes, disabled_at, created_at
FROM accounts
WHERE id = $1`
	var a Account
	var attrsRaw []byte
	err := pool.QueryRow(ctx, q, id).Scan(&a.ID, &a.TenantID, &a.ChannelType, &a.Provider, &a.ExternalID, &a.DisplayName, &attrsRaw, &a.DisabledAt, &a.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Account{}, ErrAccountNotFound
		}
		return Account{}, fmt.Errorf("store: get account: %w", err)
	}
	a.Attributes, _ = unmarshalAttrs(attrsRaw)
	return a, nil
}

// DisableAccount soft-deletes an account (disabled_at = NOW()). Idempotent.
func DisableAccount(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) error {
	const q = `UPDATE accounts SET disabled_at = NOW() WHERE id = $1 AND disabled_at IS NULL`
	if _, err := pool.Exec(ctx, q, id); err != nil {
		return fmt.Errorf("store: disable account: %w", err)
	}
	return nil
}
