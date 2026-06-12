package store

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DurableOutboundState is the Postgres-backed send_command_id → external_id
// map with the in-memory LRU as read-through cache. Survives restarts and
// lets any replica resolve edits for sends made by another.
//
// DB failures degrade to memory-only (logged); the pool's existing
// state_missing fallback covers the worst case.
type DurableOutboundState struct {
	pool   *pgxpool.Pool
	cache  *OutboundState
	logger *slog.Logger
}

func NewDurableOutboundState(pool *pgxpool.Pool, logger *slog.Logger) *DurableOutboundState {
	if logger == nil {
		logger = slog.Default()
	}
	return &DurableOutboundState{pool: pool, cache: NewOutboundState(), logger: logger}
}

func (s *DurableOutboundState) Set(ctx context.Context, sendCommandID, accountID, externalID string) {
	s.cache.Set(ctx, sendCommandID, accountID, externalID)
	_, err := s.pool.Exec(ctx, `
INSERT INTO outbound_state (send_command_id, account_id, external_id)
VALUES ($1, $2, $3)
ON CONFLICT (send_command_id) DO UPDATE SET external_id = EXCLUDED.external_id`,
		sendCommandID, accountID, externalID)
	if err != nil {
		s.logger.Warn("outbound_state: persist failed — memory-only for this entry",
			"send_command_id", sendCommandID, "err", err)
	}
}

func (s *DurableOutboundState) Get(ctx context.Context, sendCommandID string) (string, bool) {
	if extID, ok := s.cache.Get(ctx, sendCommandID); ok {
		return extID, true
	}
	var accountID, externalID string
	err := s.pool.QueryRow(ctx, `
SELECT account_id, external_id FROM outbound_state WHERE send_command_id = $1`,
		sendCommandID).Scan(&accountID, &externalID)
	if err != nil {
		return "", false
	}
	s.cache.Set(ctx, sendCommandID, accountID, externalID)
	return externalID, true
}

// CleanupLoop deletes rows older than maxAge every interval until ctx ends.
func (s *DurableOutboundState) CleanupLoop(ctx context.Context, interval, maxAge time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tag, err := s.pool.Exec(ctx,
				`DELETE FROM outbound_state WHERE created_at < now() - $1::interval`,
				maxAge.String())
			if err != nil {
				s.logger.Warn("outbound_state: cleanup failed", "err", err)
				continue
			}
			if tag.RowsAffected() > 0 {
				s.logger.Info("outbound_state: cleanup", "deleted", tag.RowsAffected())
			}
		}
	}
}
