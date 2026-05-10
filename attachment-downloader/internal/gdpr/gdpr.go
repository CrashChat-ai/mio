// Package gdpr implements right-to-erasure sweeps over the attachment
// storage prefix.
//
// Strategy: List the prefix, Stat each object to read account_id metadata,
// Delete matching objects with bounded concurrency. O(N Stat) for the prefix;
// for POC volumes (≤1M objects) acceptable. Scale via a side-table later.
package gdpr

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/crashchat-ai/mio/attachment-downloader/internal/storage"
)

// Stats summarises a sweep.
type Stats struct {
	Listed  int
	Matched int
	Deleted int
}

// DeleteByAccount removes every attachment under prefix where object metadata
// account_id == accountID.
//
// dryRun=true reports the count without deleting.
// concurrency caps in-flight Stat+Delete operations.
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
	if concurrency <= 0 {
		concurrency = 8
	}
	if log == nil {
		log = slog.Default()
	}

	out, errCh := s.List(ctx, prefix)

	var (
		mu      sync.Mutex
		stats   Stats
		sweepErr error
		wg      sync.WaitGroup
		tokens  = make(chan struct{}, concurrency)
	)

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

		// List may not surface metadata depending on backend; Stat is the
		// authoritative read. The GCS Object iterator returns full attrs,
		// so List already populates AccountID — fast-path it.
		ownerID := o.AccountID
		if ownerID == "" {
			obj, err := s.Stat(ctx, o.Key)
			if err != nil {
				mu.Lock()
				sweepErr = fmt.Errorf("gdpr: stat %s: %w", o.Key, err)
				mu.Unlock()
				return
			}
			ownerID = obj.AccountID
		}
		if ownerID != accountID {
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
		log.Info("gdpr: deleted", "key", o.Key, "account_id", accountID)
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
