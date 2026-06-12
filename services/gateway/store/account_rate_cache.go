package store

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const rateCacheTTL = 60 * time.Second

// AccountRateCache serves per-account rate overrides to the sender pool
// without a DB hit per send. Zero/absent override returns (0, false) so the
// pool falls through to capability/default rates.
type AccountRateCache struct {
	pool *pgxpool.Pool

	mu      sync.Mutex
	entries map[string]rateEntry
}

type rateEntry struct {
	perSecond float64
	fetched   time.Time
}

func NewAccountRateCache(pool *pgxpool.Pool) *AccountRateCache {
	return &AccountRateCache{pool: pool, entries: make(map[string]rateEntry)}
}

func (c *AccountRateCache) RateFor(ctx context.Context, accountID string) (float64, bool) {
	c.mu.Lock()
	e, ok := c.entries[accountID]
	fresh := ok && time.Since(e.fetched) < rateCacheTTL
	c.mu.Unlock()
	if fresh {
		return e.perSecond, e.perSecond > 0
	}

	var perSecond float64
	err := c.pool.QueryRow(ctx,
		`SELECT rate_limit_per_second FROM accounts WHERE id = $1`, accountID).Scan(&perSecond)
	if err != nil {
		// Miss or DB error: cache nothing, fall through to defaults.
		return 0, false
	}

	c.mu.Lock()
	c.entries[accountID] = rateEntry{perSecond: perSecond, fetched: time.Now()}
	c.mu.Unlock()
	return perSecond, perSecond > 0
}
