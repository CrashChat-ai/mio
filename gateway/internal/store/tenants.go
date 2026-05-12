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

// Tenant is the row shape returned by the tenant store functions.
// disabled_at is nil for active tenants; non-nil indicates soft-delete.
type Tenant struct {
	ID          uuid.UUID
	Slug        string
	Status      string
	DisplayName string // empty until populated by admin or migration
	DisabledAt  *time.Time
	CreatedAt   time.Time
}

// ErrTenantNotFound is returned by GetTenant when the lookup misses.
var ErrTenantNotFound = errors.New("store: tenant not found")

// EnsureTenant inserts (id, slug, display_name) or updates display_name if
// the slug already exists. Returns the canonical row. Idempotent — safe to
// call on every admin startup.
func EnsureTenant(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, slug, displayName string) (Tenant, error) {
	const q = `
INSERT INTO tenants (id, slug, status, display_name)
VALUES ($1, $2, 'active', $3)
ON CONFLICT (slug) DO UPDATE SET display_name = EXCLUDED.display_name
RETURNING id, slug, status, display_name, disabled_at, created_at`
	var t Tenant
	var displayNameRow *string
	if err := pool.QueryRow(ctx, q, id, slug, displayName).
		Scan(&t.ID, &t.Slug, &t.Status, &displayNameRow, &t.DisabledAt, &t.CreatedAt); err != nil {
		return Tenant{}, fmt.Errorf("store: ensure tenant: %w", err)
	}
	if displayNameRow != nil {
		t.DisplayName = *displayNameRow
	}
	return t, nil
}

// ListTenants returns tenants ordered by created_at desc. Disabled tenants
// are included; callers filter by DisabledAt as needed.
func ListTenants(ctx context.Context, pool *pgxpool.Pool) ([]Tenant, error) {
	const q = `
SELECT id, slug, status, display_name, disabled_at, created_at
FROM tenants
ORDER BY created_at DESC`
	rows, err := pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("store: list tenants: %w", err)
	}
	defer rows.Close()
	var out []Tenant
	for rows.Next() {
		var t Tenant
		var displayName *string
		if err := rows.Scan(&t.ID, &t.Slug, &t.Status, &displayName, &t.DisabledAt, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: list tenants scan: %w", err)
		}
		if displayName != nil {
			t.DisplayName = *displayName
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTenant fetches a single tenant by id. Returns ErrTenantNotFound on miss.
func GetTenant(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (Tenant, error) {
	const q = `
SELECT id, slug, status, display_name, disabled_at, created_at
FROM tenants
WHERE id = $1`
	var t Tenant
	var displayName *string
	err := pool.QueryRow(ctx, q, id).Scan(&t.ID, &t.Slug, &t.Status, &displayName, &t.DisabledAt, &t.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Tenant{}, ErrTenantNotFound
		}
		return Tenant{}, fmt.Errorf("store: get tenant: %w", err)
	}
	if displayName != nil {
		t.DisplayName = *displayName
	}
	return t, nil
}

// DisableTenant sets disabled_at = NOW() on the tenant row. Idempotent.
// Does NOT cascade to accounts — the admin layer disables accounts
// explicitly so the operator audit trail records each action.
func DisableTenant(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) error {
	const q = `UPDATE tenants SET disabled_at = NOW() WHERE id = $1 AND disabled_at IS NULL`
	tag, err := pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("store: disable tenant: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Either tenant doesn't exist or already disabled — both no-op safe.
		// Caller distinguishes via GetTenant if needed.
		return nil
	}
	return nil
}
