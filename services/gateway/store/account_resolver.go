package store

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const resolverCacheTTL = 60 * time.Second

// ResolvedAccount is the routing identity stamped onto inbound messages.
type ResolvedAccount struct {
	TenantID  string
	AccountID string
}

// AccountResolver routes an inbound webhook to an account:
//  1. exactly one enabled account for the channel_type → that account,
//     unless both its external_id and the workspace key are set and differ
//  2. workspace key matches accounts.external_id → that account
//  3. otherwise not resolved (caller falls back to env identity or drops)
//
// A non-nil error means the lookup itself failed — callers must NOT treat
// that as "no match" (env fallback on a DB blip would misroute tenants).
// Account rows are cached per channel_type for resolverCacheTTL — webhook
// hot path must not query Postgres per request.
type AccountResolver struct {
	pool   *pgxpool.Pool
	logger *slog.Logger

	mu      sync.Mutex
	cache   map[string]resolverEntry
	nowFunc func() time.Time
}

type resolverEntry struct {
	accounts []resolverAccount
	fetched  time.Time
}

type resolverAccount struct {
	tenantID   string
	accountID  string
	externalID string
}

func NewAccountResolver(pool *pgxpool.Pool, logger *slog.Logger) *AccountResolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &AccountResolver{
		pool:    pool,
		logger:  logger,
		cache:   make(map[string]resolverEntry),
		nowFunc: time.Now,
	}
}

func (r *AccountResolver) Resolve(ctx context.Context, channelType, workspaceKey string) (ResolvedAccount, bool, error) {
	accounts, err := r.enabledAccounts(ctx, channelType)
	if err != nil {
		r.logger.Error("account resolver: query failed", "channel", channelType, "err", err)
		return ResolvedAccount{}, false, err
	}
	acct, ok := pickAccount(accounts, workspaceKey)
	if !ok {
		return ResolvedAccount{}, false, nil
	}
	return ResolvedAccount{TenantID: acct.tenantID, AccountID: acct.accountID}, true, nil
}

// pickAccount applies the routing rules over the enabled-account set.
func pickAccount(accounts []resolverAccount, workspaceKey string) (resolverAccount, bool) {
	if len(accounts) == 1 {
		a := accounts[0]
		if workspaceKey != "" && a.externalID != "" && a.externalID != workspaceKey {
			return resolverAccount{}, false
		}
		return a, true
	}
	if workspaceKey != "" {
		for _, a := range accounts {
			if a.externalID == workspaceKey {
				return a, true
			}
		}
	}
	return resolverAccount{}, false
}

func (r *AccountResolver) enabledAccounts(ctx context.Context, channelType string) ([]resolverAccount, error) {
	r.mu.Lock()
	entry, ok := r.cache[channelType]
	fresh := ok && r.nowFunc().Sub(entry.fetched) < resolverCacheTTL
	r.mu.Unlock()
	if fresh {
		return entry.accounts, nil
	}

	rows, err := r.pool.Query(ctx, `
SELECT tenant_id, id, external_id FROM accounts
WHERE channel_type = $1 AND disabled_at IS NULL`, channelType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []resolverAccount
	for rows.Next() {
		var a resolverAccount
		if err := rows.Scan(&a.tenantID, &a.accountID, &a.externalID); err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.cache[channelType] = resolverEntry{accounts: accounts, fetched: r.nowFunc()}
	r.mu.Unlock()
	return accounts, nil
}

// HasEnabledAccounts reports whether any enabled account exists — used at
// boot to decide if the env-var identity is still required.
func HasEnabledAccounts(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM accounts WHERE disabled_at IS NULL)`).Scan(&exists)
	return exists, err
}
