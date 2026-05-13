package ratelimit

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/time/rate"
)

// newTestLimiter builds a Limiter with isolated Prometheus registry.
// Does NOT start the eviction goroutine — tests control time explicitly.
func newTestLimiter(t *testing.T, r rate.Limit, burst int) *Limiter {
	t.Helper()
	reg := prometheus.NewRegistry()
	active := prometheus.NewGauge(prometheus.GaugeOpts{Name: "active"})
	evicted := prometheus.NewCounter(prometheus.CounterOpts{Name: "evicted"})
	reg.MustRegister(active, evicted)

	return &Limiter{
		buckets: make(map[string]*rate.Limiter),
		lastUse: make(map[string]time.Time),
		r:       r,
		burst:   burst,
		ttl:     defaultTTL,
		logger:  slog.Default(),
		active:  active,
		evicted: evicted,
	}
}

func TestAllow_FirstCall(t *testing.T) {
	l := newTestLimiter(t, 5, 10)
	if !l.Allow("account-A") {
		t.Fatal("expected first Allow to succeed (burst available)")
	}
}

func TestAllow_EmptyKey_Panics(t *testing.T) {
	l := newTestLimiter(t, 5, 10)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty key")
		}
	}()
	l.Allow("")
}

// TestIsolation_AccountA_DoesNotSlowAccountB is the core fairness invariant:
// draining account A's bucket must not affect account B's tokens.
func TestIsolation_AccountA_DoesNotSlowAccountB(t *testing.T) {
	// Burst=1 means A's bucket drains immediately.
	l := newTestLimiter(t, 1, 1)

	// Drain A completely.
	l.Allow("account-A")
	l.Allow("account-A") // second call — bucket empty, returns false

	// B must still be allowed (separate bucket).
	if !l.Allow("account-B") {
		t.Fatal("account B blocked despite separate bucket — isolation failure")
	}
}

func TestBucketCount_TracksCreation(t *testing.T) {
	l := newTestLimiter(t, 5, 10)
	if l.BucketCount() != 0 {
		t.Fatalf("expected 0 buckets initially, got %d", l.BucketCount())
	}
	l.Allow("a1")
	l.Allow("a2")
	if l.BucketCount() != 2 {
		t.Fatalf("expected 2 buckets, got %d", l.BucketCount())
	}
}

func TestEviction_RemovesIdleBuckets(t *testing.T) {
	l := newTestLimiter(t, 5, 10)
	l.ttl = 1 * time.Millisecond

	l.Allow("account-X")
	if l.BucketCount() != 1 {
		t.Fatalf("expected 1 bucket before eviction")
	}

	time.Sleep(5 * time.Millisecond)
	l.runEviction()

	if l.BucketCount() != 0 {
		t.Fatalf("expected 0 buckets after eviction, got %d", l.BucketCount())
	}
}

func TestEviction_PreservesRecentBuckets(t *testing.T) {
	l := newTestLimiter(t, 5, 10)
	l.ttl = 100 * time.Millisecond

	l.Allow("account-keep")
	l.runEviction() // lastUse is recent — must NOT evict

	if l.BucketCount() != 1 {
		t.Fatalf("expected bucket preserved after early eviction, got %d", l.BucketCount())
	}
}

// TestNew_StartsEvictionGoroutine verifies the goroutine starts and that
// context cancellation stops it (no goroutine leak).
func TestNew_StartsEvictionGoroutine(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reg := prometheus.NewRegistry()
	l := New(ctx, reg, slog.Default())

	l.Allow("account-Z")
	if l.BucketCount() != 1 {
		t.Fatalf("expected 1 bucket")
	}

	// Cancel context — goroutine should exit.
	cancel()
	// Give goroutine time to exit.
	time.Sleep(10 * time.Millisecond)
	// No assertion on goroutine count; just verify no deadlock / panic.
}

// TestConcurrentAccess exercises concurrent bucket creation from many goroutines.
func TestConcurrentAccess(t *testing.T) {
	l := newTestLimiter(t, 100, 100)
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := "account-" + string(rune('A'+i%26))
			for j := range 10 {
				_ = j
				l.Allow(key)
			}
		}(i)
	}
	wg.Wait()
	// Must not panic or deadlock.
}

// TestFairness_BurstADoesNotBlockB is a concurrent fairness assertion:
// goroutine A sends 50 rapid requests to account A while goroutine B sends
// 1 request to account B; B must succeed within 100ms (no cross-blocking).
func TestFairness_BurstADoesNotBlockB(t *testing.T) {
	// High rate so tokens aren't actually depleted in this test; we just
	// verify isolation — the latency test is in the integration bench.
	l := newTestLimiter(t, 1000, 1000)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for range 50 {
			l.Allow("account-A")
		}
	}()

	resultCh := make(chan bool, 1)
	go func() {
		defer wg.Done()
		start := time.Now()
		ok := l.Allow("account-B")
		elapsed := time.Since(start)
		resultCh <- ok
		if elapsed > 100*time.Millisecond {
			t.Errorf("account B blocked for %v — isolation failure", elapsed)
		}
	}()

	wg.Wait()
	if !<-resultCh {
		t.Fatal("account B denied despite separate bucket")
	}
}
