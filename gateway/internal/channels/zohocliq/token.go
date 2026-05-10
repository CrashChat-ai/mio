package zohocliq

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Zoho OAuth token endpoint. Override via newTokenSource opts in tests.
const oauthDefaultURL = "https://accounts.zoho.com/oauth/v2/token"

// refreshSafetyWindow is how long before expiresAt we proactively refresh.
// Zoho access tokens last 3600s; refreshing at 5 min remaining gives plenty
// of slack for clock skew and slow-refresh round trips.
const refreshSafetyWindow = 5 * time.Minute

// refreshHTTPTimeout caps the OAuth refresh request. Smaller than the send
// timeout (10s) so a slow Zoho OAuth endpoint does not stall the send pipeline.
const refreshHTTPTimeout = 5 * time.Second

// cliqTokenRefreshTotal counts OAuth refresh attempts by outcome.
// Labels:
//   - result: "ok" — refresh succeeded, new access token cached
//             "fail" — refresh endpoint returned non-2xx or network error
var cliqTokenRefreshTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "mio_gateway_cliq_token_refresh_total",
	Help: "Zoho Cliq OAuth access-token refresh attempts, by outcome.",
}, []string{"result"})

// tokenSource caches a Zoho OAuth access token and refreshes it on demand
// using a long-lived refresh token. Safe for concurrent use.
//
// Concurrency model: a single sync.Mutex protects (current, expiresAt) and
// also serialises the refresh HTTP call. Cold-cache stampede (N goroutines
// asking for the first token) deduplicates naturally — the lock is held
// throughout Get, so once goroutine A finishes refreshLocked and writes
// (current, expiresAt), goroutine B acquires the lock, hits the cache check
// at the top of Get, and returns the cached token without reaching the
// refresh path.
type tokenSource struct {
	clientID     string
	clientSecret string
	refreshToken string
	httpClient   *http.Client
	oauthURL     string
	logger       *slog.Logger

	mu        sync.Mutex
	current   string
	expiresAt time.Time
}

// tokenSourceOpt is a functional option for newTokenSource — keeps the
// constructor signature stable while allowing test overrides.
type tokenSourceOpt func(*tokenSource)

// withOAuthURL overrides the OAuth token endpoint. Test-only.
func withOAuthURL(u string) tokenSourceOpt {
	return func(t *tokenSource) { t.oauthURL = u }
}

// newTokenSource constructs a tokenSource. clientID / clientSecret /
// refreshToken are required; constructor returns nil only if all three are
// empty (caller decides whether that is fatal).
func newTokenSource(clientID, clientSecret, refreshToken string, opts ...tokenSourceOpt) *tokenSource {
	if clientID == "" && clientSecret == "" && refreshToken == "" {
		return nil
	}
	t := &tokenSource{
		clientID:     clientID,
		clientSecret: clientSecret,
		refreshToken: refreshToken,
		httpClient:   &http.Client{Timeout: refreshHTTPTimeout},
		oauthURL:     oauthDefaultURL,
		logger:       slog.Default(),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Get returns a valid access token. If the cached token is missing or within
// refreshSafetyWindow of expiry, fetches a fresh one from the OAuth endpoint.
// Errors from the refresh endpoint are returned as *refreshError so callers
// can distinguish "OAuth flow broken" from "Cliq API call failed".
func (t *tokenSource) Get(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.current != "" && time.Until(t.expiresAt) > refreshSafetyWindow {
		return t.current, nil
	}
	return t.refreshLocked(ctx)
}

// Invalidate clears the cached token. The next Get call will force a fresh
// fetch from the OAuth endpoint. Used by the adapter's self-heal path: when
// Cliq returns 401 on a token we thought was valid (Zoho rotated it early),
// invalidate and retry once.
func (t *tokenSource) Invalidate() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.current = ""
	t.expiresAt = time.Time{}
}

// refreshLocked posts to the OAuth endpoint and updates the cache.
// MUST be called with t.mu held. Stampede dedupe falls out naturally:
// when N goroutines call Get on a cold cache, only one acquires the lock
// at a time. The first one runs this function (one OAuth request); the
// next one hits the cache check at the top of Get and never reaches here.
func (t *tokenSource) refreshLocked(ctx context.Context) (string, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {t.clientID},
		"client_secret": {t.clientSecret},
		"refresh_token": {t.refreshToken},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.oauthURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		cliqTokenRefreshTotal.WithLabelValues("fail").Inc()
		return "", &refreshError{Err: fmt.Errorf("build request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		cliqTokenRefreshTotal.WithLabelValues("fail").Inc()
		return "", &refreshError{Err: fmt.Errorf("http: %w", err)}
	}
	defer resp.Body.Close() //nolint:errcheck

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		cliqTokenRefreshTotal.WithLabelValues("fail").Inc()
		return "", &refreshError{Status: resp.StatusCode, Body: string(body)}
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"` // present on JSON-200 error responses (Zoho quirk)
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		cliqTokenRefreshTotal.WithLabelValues("fail").Inc()
		return "", &refreshError{Status: resp.StatusCode, Body: string(body),
			Err: fmt.Errorf("parse response: %w", err)}
	}
	// Zoho sometimes returns 200 with {"error":"..."} body. Treat as failure.
	if parsed.AccessToken == "" {
		cliqTokenRefreshTotal.WithLabelValues("fail").Inc()
		return "", &refreshError{Status: resp.StatusCode, Body: string(body),
			Err: fmt.Errorf("missing access_token in response (error=%q)", parsed.Error)}
	}

	// Subtract a 30s safety to avoid clock-skew expiry races with Cliq.
	ttl := time.Duration(parsed.ExpiresIn)*time.Second - 30*time.Second
	if ttl <= 0 {
		// Defensive: if Zoho returned an absurdly short TTL, default to 1 min.
		// We will refresh again within the safety window on next Get.
		ttl = time.Minute
	}
	t.current = parsed.AccessToken
	t.expiresAt = time.Now().Add(ttl)
	cliqTokenRefreshTotal.WithLabelValues("ok").Inc()
	t.logger.Info("cliq: token refreshed",
		"expires_in_seconds", parsed.ExpiresIn,
		"effective_ttl_seconds", int(ttl.Seconds()),
	)
	return t.current, nil
}

// refreshError represents an OAuth-refresh-endpoint failure. Distinct from
// HTTPError (which is a Cliq REST API failure). The pool's classify4xx will
// map this to reason="refresh_failed" so on-call can tell "rotate refresh
// token" failures apart from "rotate access token" failures.
type refreshError struct {
	Status int    // HTTP status from the OAuth endpoint (0 if network error)
	Body   string // raw response body for logging
	Err    error  // underlying error (network / parse), if any
}

func (e *refreshError) Error() string {
	if e.Err != nil && e.Status == 0 {
		return fmt.Sprintf("cliq oauth refresh: %v", e.Err)
	}
	return fmt.Sprintf("cliq oauth refresh: http %d: %s", e.Status, e.Body)
}

func (e *refreshError) Unwrap() error { return e.Err }

// IsRetryable returns false. Tradeoff: during a Zoho OAuth outage we will
// Term incoming messages instead of Nak'ing them. Rationale:
//   - 401/4xx from OAuth means credentials are wrong → no retry can help
//   - 5xx from OAuth means Zoho is down → JS redelivery would just hammer
//     a broken upstream; better to drop and rely on operator alerting
//     (the distinct reason="refresh_failed" label paged separately from
//     reason="auth" gives ops the signal to investigate, not bot-loop-retry)
//
// If a future production deployment needs at-least-once during OAuth blips,
// flip this to `return e.Status >= 500` — pool will Nak with jitter.
func (e *refreshError) IsRetryable() bool { return false }

// IsRateLimited returns false — Zoho OAuth doesn't rate-limit in our usage.
func (e *refreshError) IsRateLimited() bool { return false }

// RetryAfterSeconds returns 0 — never used.
func (e *refreshError) RetryAfterSeconds() int { return 0 }

// Reason returns "refresh_failed" so the pool's classifier can label this
// distinctly from generic 401/403/etc auth failures.
func (e *refreshError) Reason() string { return "refresh_failed" }
