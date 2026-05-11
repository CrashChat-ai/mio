// Package gdpr implements right-to-erasure sweeps over the attachment
// storage prefix. List → Stat-if-needed → match → Delete with bounded
// concurrency. O(N Stat); scale via a side-table later.
package gdpr

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/crashchat-ai/mio/media-vault/internal/storage"
)

// Stats summarises a sweep.
type Stats struct {
	Listed  int
	Matched int
	Deleted int
}

// matcher returns (matches, fieldName, fieldValue) so the engine can log
// which identifier triggered a delete.
type matcher func(o storage.Object) (matches bool, fieldName, fieldValue string)

// DeleteByAccount removes objects under prefix whose account_id metadata
// matches accountID.
func DeleteByAccount(
	ctx context.Context,
	s storage.Storage,
	prefix, accountID string,
	dryRun bool,
	concurrency int,
	log *slog.Logger,
) (Stats, error) {
	if accountID == "" {
		return Stats{}, fmt.Errorf("gdpr: account_id is required")
	}
	return deleteByFilter(ctx, s, prefix, dryRun, concurrency, log, func(o storage.Object) (bool, string, string) {
		return o.AccountID == accountID, "account_id", accountID
	})
}

// DeleteByTenant removes objects under prefix whose tenant_id metadata
// matches tenantID. Forward-only: pre-enrichment objects have no tenant_id
// and will not be matched — fall back to DeleteByAccount for historical
// data.
func DeleteByTenant(
	ctx context.Context,
	s storage.Storage,
	prefix, tenantID string,
	dryRun bool,
	concurrency int,
	log *slog.Logger,
) (Stats, error) {
	if tenantID == "" {
		return Stats{}, fmt.Errorf("gdpr: tenant_id is required")
	}
	return deleteByFilter(ctx, s, prefix, dryRun, concurrency, log, func(o storage.Object) (bool, string, string) {
		return o.TenantID == tenantID, "tenant_id", tenantID
	})
}

// DeleteByConversation removes objects under prefix whose conversation_id
// metadata matches conversationID. Forward-only (same caveat as DeleteByTenant).
func DeleteByConversation(
	ctx context.Context,
	s storage.Storage,
	prefix, conversationID string,
	dryRun bool,
	concurrency int,
	log *slog.Logger,
) (Stats, error) {
	if conversationID == "" {
		return Stats{}, fmt.Errorf("gdpr: conversation_id is required")
	}
	return deleteByFilter(ctx, s, prefix, dryRun, concurrency, log, func(o storage.Object) (bool, string, string) {
		return o.ConversationID == conversationID, "conversation_id", conversationID
	})
}

func deleteByFilter(
	ctx context.Context,
	s storage.Storage,
	prefix string,
	dryRun bool,
	concurrency int,
	log *slog.Logger,
	match matcher,
) (Stats, error) {
	if concurrency <= 0 {
		concurrency = 8
	}
	if log == nil {
		log = slog.Default()
	}

	out, errCh := s.List(ctx, prefix)

	var (
		mu       sync.Mutex
		stats    Stats
		sweepErr error
		wg       sync.WaitGroup
		tokens   = make(chan struct{}, concurrency)
	)

	// Fall back to Stat only when List returned an Object with no owner
	// fields at all — for GCS, List populates whatever metadata is on the
	// object, so this is the rare "malformed" case. Other backends that
	// don't surface metadata via List rely on this fallback.
	needsStat := func(o storage.Object) bool {
		return o.TenantID == "" && o.AccountID == "" && o.ConversationID == "" && o.SourceMessageID == ""
	}

	processOne := func(o storage.Object) {
		defer wg.Done()
		defer func() { <-tokens }()

		mu.Lock()
		if sweepErr != nil {
			mu.Unlock()
			return
		}
		stats.Listed++
		mu.Unlock()

		if needsStat(o) {
			obj, err := s.Stat(ctx, o.Key)
			if err != nil {
				mu.Lock()
				sweepErr = fmt.Errorf("gdpr: stat %s: %w", o.Key, err)
				mu.Unlock()
				return
			}
			o = *obj
		}

		matched, fieldName, fieldValue := match(o)
		if !matched {
			return
		}

		mu.Lock()
		stats.Matched++
		mu.Unlock()

		if dryRun {
			return
		}
		if err := s.Delete(ctx, o.Key); err != nil {
			mu.Lock()
			sweepErr = fmt.Errorf("gdpr: delete %s: %w", o.Key, err)
			mu.Unlock()
			return
		}
		mu.Lock()
		stats.Deleted++
		mu.Unlock()
		log.Info("gdpr: deleted", "key", o.Key, fieldName, fieldValue)
	}

	for o := range out {
		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		case tokens <- struct{}{}:
		}
		wg.Add(1)
		go processOne(o)
	}
	wg.Wait()

	if listErr := <-errCh; listErr != nil {
		return stats, listErr
	}
	return stats, sweepErr
}
