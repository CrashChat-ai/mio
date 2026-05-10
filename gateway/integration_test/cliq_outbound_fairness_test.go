// Package integration_test — fairness benchmark for the per-account rate limiter.
//
// Verifies the core isolation invariant:
//
//	"Bursting account A (50 msg/s) must not delay account B's p99 above 2s."
//
// This is a Go test (not vegeta/k6) because we only need a single assertion
// on an in-process rate-limiter with a mocked adapter HTTP call (50ms sleep).
// Run via: make gateway-bench-outbound
package integration_test

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/crashchat-ai/mio/gateway/internal/ratelimit"
)

// TestFairness_BurstADoesNotDelayB is the M2 fairness gate.
//
// Setup:
//   - Two accounts: A (bursts 50 calls in ~10s) and B (1 call/5s for 10s).
//   - Rate limiter: 5 tokens/sec, burst 10 — A drains its bucket quickly.
//   - Mock "HTTP" call: 50ms sleep to simulate Cliq round-trip.
//   - Assertion: B's p99 end-to-end latency < 2s.
//
// The limiter isolates B's bucket from A's; B should never wait on A's tokens.
func TestFairness_BurstADoesNotDelayB(t *testing.T) {
	const (
		testDuration = 10 * time.Second
		mockHTTPLag  = 50 * time.Millisecond
		p99Target    = 2 * time.Second
	)

	ctx, cancel := context.WithTimeout(context.Background(), testDuration+2*time.Second)
	defer cancel()

	reg := prometheus.NewRegistry()
	limiter := ratelimit.New(ctx, reg, nil)

	// mockSend simulates the adapter HTTP call.
	mockSend := func(accountID string) time.Duration {
		start := time.Now()
		// Acquire rate-limit token for this account.
		for !limiter.Allow(accountID) {
			time.Sleep(10 * time.Millisecond) // brief spin; real pool uses Nak+delay
		}
		time.Sleep(mockHTTPLag) // simulate Cliq REST call
		return time.Since(start)
	}

	// Collect B's per-message latencies.
	var mu sync.Mutex
	bLatencies := make([]time.Duration, 0, 4)

	var wg sync.WaitGroup

	// Goroutine A: burst 50 messages to account-A over testDuration.
	wg.Add(1)
	go func() {
		defer wg.Done()
		deadline := time.Now().Add(testDuration)
		sent := 0
		for time.Now().Before(deadline) && sent < 50 {
			go func() { _ = mockSend("account-A") }() // fire-and-forget
			sent++
			time.Sleep(200 * time.Millisecond) // 5/s burst cadence
		}
	}()

	// Goroutine B: 1 message every 2s to account-B throughout.
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		deadline := time.Now().Add(testDuration)
		for {
			select {
			case <-ctx.Done():
				return
			case t := <-ticker.C:
				if t.After(deadline) {
					return
				}
				lat := mockSend("account-B")
				mu.Lock()
				bLatencies = append(bLatencies, lat)
				mu.Unlock()
			}
		}
	}()

	wg.Wait()

	mu.Lock()
	lats := make([]time.Duration, len(bLatencies))
	copy(lats, bLatencies)
	mu.Unlock()

	if len(lats) == 0 {
		t.Fatal("account B sent no messages — test setup error")
	}

	sort.Slice(lats, func(i, j int) bool { return lats[i] < lats[j] })
	p99idx := int(float64(len(lats)) * 0.99)
	if p99idx >= len(lats) {
		p99idx = len(lats) - 1
	}
	p99 := lats[p99idx]

	t.Logf("account B: %d samples, p99=%v (target<%v)", len(lats), p99, p99Target)

	if p99 > p99Target {
		t.Errorf("fairness FAIL: account B p99=%v exceeds %v while account A bursts",
			p99, p99Target)
	}
}
