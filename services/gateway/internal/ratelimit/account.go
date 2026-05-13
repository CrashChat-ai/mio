// Package ratelimit provides a per-account token-bucket rate limiter with
// TTL-based eviction of idle buckets.
//
// Key design choices (per research Q1):
//   - In-process golang.org/x/time/rate.Limiter — no Redis dependency for POC.
//   - Key is account_id by default; adapters may return a composite key via
//     Adapter.RateLimitKey to express per-conversation fairness (e.g. Slack).
//   - Eviction goroutine runs every 60s; buckets idle > ttl are removed.
//   - mio_gateway_ratelimit_buckets_active gauge and _evicted_total counter
//     let us detect a dead eviction goroutine before it causes an OOM.
package ratelimit

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/time/rate"
)

const (
	defaultRate  = rate.Limit(5)    // tokens/sec per bucket
	defaultBurst = 10               // burst size
	defaultTTL   = 10 * time.Minute // idle eviction threshold
	evictTick    = 60 * time.Second // eviction scan interval
)

// Limiter is a per-key token-bucket rate limiter with TTL eviction.
// Safe for concurrent use.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*rate.Limiter
	lastUse map[string]time.Time
	r       rate.Limit
	burst   int
	ttl     time.Duration
	logger  *slog.Logger

	// metrics
	active  prometheus.Gauge
	evicted prometheus.Counter
}

// New constructs a Limiter and starts the background eviction goroutine.
// ctx cancellation stops the eviction goroutine.
func New(ctx context.Context, reg prometheus.Registerer, logger *slog.Logger) *Limiter {
	if logger == nil {
		logger = slog.Default()
	}
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}

	factory := promauto.With(reg)
	l := &Limiter{
		buckets: make(map[string]*rate.Limiter),
		lastUse: make(map[string]time.Time),
		r:       defaultRate,
		burst:   defaultBurst,
		ttl:     defaultTTL,
		logger:  logger,
		active: factory.NewGauge(prometheus.GaugeOpts{
			Name: "mio_gateway_ratelimit_buckets_active",
			Help: "Current number of active per-account rate-limit buckets.",
		}),
		evicted: factory.NewCounter(prometheus.CounterOpts{
			Name: "mio_gateway_ratelimit_buckets_evicted_total",
			Help: "Total per-account rate-limit buckets evicted after idle TTL.",
		}),
	}

	go l.evictLoop(ctx)
	return l
}

// Allow reports whether the key's bucket has a token available.
// The bucket is created on first use. Empty key panics — callers must
// provide at least account_id.
func (l *Limiter) Allow(key string) bool {
	if key == "" {
		panic("ratelimit: Allow called with empty key")
	}
	b := l.bucket(key)
	return b.Allow()
}

// Reserve reserves a token, returning the delay until the token is available.
// Use this to implement Nak-with-delay on rate-limited messages.
func (l *Limiter) Reserve(key string) *rate.Reservation {
	if key == "" {
		panic("ratelimit: Reserve called with empty key")
	}
	return l.bucket(key).Reserve()
}

// bucket returns (or creates) the rate.Limiter for the given key.
func (l *Limiter) bucket(key string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		b = rate.NewLimiter(l.r, l.burst)
		l.buckets[key] = b
		l.active.Inc()
	}
	l.lastUse[key] = time.Now()
	return b
}

// evictLoop scans for idle buckets every evictTick and removes them.
// Recovers from panics so a bug in scan doesn't kill the gateway.
func (l *Limiter) evictLoop(ctx context.Context) {
	ticker := time.NewTicker(evictTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.runEviction()
		}
	}
}

func (l *Limiter) runEviction() {
	defer func() {
		if r := recover(); r != nil {
			l.logger.Error("ratelimit: eviction goroutine panic recovered",
				"panic", r)
		}
	}()

	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	var evictCount int
	for key, last := range l.lastUse {
		if now.Sub(last) > l.ttl {
			delete(l.buckets, key)
			delete(l.lastUse, key)
			evictCount++
		}
	}
	if evictCount > 0 {
		l.active.Sub(float64(evictCount))
		l.evicted.Add(float64(evictCount))
		l.logger.Info("ratelimit: evicted idle buckets",
			"count", evictCount,
			"remaining", len(l.buckets))
	}
}

// BucketCount returns the current number of active buckets (for testing).
func (l *Limiter) BucketCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
