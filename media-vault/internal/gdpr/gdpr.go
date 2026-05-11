// Package gdpr implements right-to-erasure sweeps over the attachment
// storage prefix.
//
// Strategy: List the prefix, Stat each object to read its metadata, Delete
// matching objects with bounded concurrency. O(N Stat) for the prefix; for
// POC volumes (≤1M objects) acceptable. Scale via a side-table later.
//
// Filter variants:
//
//   - DeleteByAccount       — account_id match (legacy entry point)
//   - DeleteByTenant        — tenant_id match (multi-tenant deployments)
//   - DeleteByConversation  — conversation_id match (narrowest forensic case)
//
// All three share the same iterator / concurrency / dry-run plumbing through
// the internal deleteByFilter core; differences are confined to the predicate
// closure and the log field name.
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

// matcher returns (matches, fieldValue, fieldName) for a candidate Object.
// fieldValue / fieldName are surfaced in delete log lines so an operator can
// trace which identifier triggered each deletion.
type matcher func(o storage.Object) (matches bool, fieldName, fieldValue string)

// DeleteByAccount removes every attachment under prefix where object metadata
// account_id == accountID. Existing entry point — keep stable.
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

// DeleteByTenant removes every attachment under prefix where object metadata
// tenant_id == tenantID. Multi-tenant deployments use this for tenant offboard.
//
// Note: this matches only objects written after the metadata-enrichment
// rollout (object metadata is forward-only). Pre-rollout objects have no
// tenant_id and will not be matched.
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

// DeleteByConversation removes every attachment under prefix where object
// metadata conversation_id == conversationID. Narrowest forensic / right-to-
// erasure variant — useful when a single thread must be purged without
// touching the rest of an account's data.
//
// Note: forward-only, same caveat as DeleteByTenant.
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

// deleteByFilter is the shared engine: List → Stat-if-needed → match → Delete,
// with bounded concurrency and dry-run support.
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

	// needsStat triggers an extra Stat round-trip only for objects with NO
	// owner metadata at all (List returned an Object with every owner field
	// empty). For GCS, List already populates all metadata that the object
	// carries, so:
	//   - post-enrichment objects: all four fields set      → returns false
	//   - pre-enrichment objects:  account_id only          → returns false
	//   - malformed / pathological objects: zero fields set → returns true
	// In other words: returns false whenever ANY owner field is set, which
	// covers every realistic GCS object. Other backends that don't surface
	// metadata via List may need the Stat fallback.
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
