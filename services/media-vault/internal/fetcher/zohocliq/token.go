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
)

// oauthDefaultURL is Zoho's OAuth token endpoint. Override via withOAuthURL in tests.
const oauthDefaultURL = "https://accounts.zoho.com/oauth/v2/token"

// refreshSafetyWindow is how long before expiresAt we proactively refresh.
// Zoho access tokens last 3600s; refreshing at 5 min remaining gives plenty
// of slack for clock skew and slow-refresh round trips.
const refreshSafetyWindow = 5 * time.Minute

// refreshHTTPTimeout caps the OAuth refresh request itself. Smaller than the
// fetch timeout so a slow Zoho OAuth endpoint does not stall a download.
const refreshHTTPTimeout = 5 * time.Second

// tokenSource caches a Zoho OAuth access token and refreshes it on demand
// using a long-lived refresh token. Safe for concurrent use.
//
// Concurrency model mirrors gateway/internal/channels/zohocliq/token.go: a
// single sync.Mutex protects (current, expiresAt) and serialises the refresh
// HTTP call. Cold-cache stampedes deduplicate naturally — only one goroutine
// holds the lock during refreshLocked; subsequent goroutines hit the cache
// check at the top of Get and return without re-fetching.
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
	static    bool // test-only: never refresh, ignore Invalidate
}

type tokenSourceOpt func(*tokenSource)

// withOAuthURL overrides the OAuth token endpoint. Test-only.
func withOAuthURL(u string) tokenSourceOpt {
	return func(t *tokenSource) { t.oauthURL = u }
}

// staticTokenSource returns a tokenSource pre-loaded with token and a
// far-future expiry, suitable for tests that don't want to mock the OAuth
// endpoint.
func staticTokenSource(token string) *tokenSource {
	return &tokenSource{current: token, expiresAt: time.Now().Add(24 * time.Hour), static: true}
}

// newTokenSource constructs a tokenSource. Returns nil when all three
// credentials are empty (caller decides whether that is fatal).
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

// Get returns a valid access token. Refreshes when missing or within
// refreshSafetyWindow of expiry. Returns *refreshError on OAuth failure so
// callers can distinguish "auth flow broken" from "Cliq API call failed".
func (t *tokenSource) Get(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.current != "" && time.Until(t.expiresAt) > refreshSafetyWindow {
		return t.current, nil
	}
	return t.refreshLocked(ctx)
}

// Invalidate clears the cached token. Used on 401 self-heal: when Cliq
// rejects a token we believed was valid (Zoho rotated it early), drop it
// and force a fresh refresh on the next Get.
func (t *tokenSource) Invalidate() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.static {
		return
	}
	t.current = ""
	t.expiresAt = time.Time{}
}

// refreshLocked posts to the OAuth endpoint and updates the cache.
// MUST be called with t.mu held.
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
		return "", &refreshError{Err: fmt.Errorf("build request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return "", &refreshError{Err: fmt.Errorf("http: %w", err)}
	}
	defer resp.Body.Close() //nolint:errcheck

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &refreshError{Status: resp.StatusCode, Body: string(body)}
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"` // Zoho sometimes returns 200 + {"error":"..."}
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", &refreshError{Status: resp.StatusCode, Body: string(body),
			Err: fmt.Errorf("parse response: %w", err)}
	}
	if parsed.AccessToken == "" {
		return "", &refreshError{Status: resp.StatusCode, Body: string(body),
			Err: fmt.Errorf("missing access_token in response (error=%q)", parsed.Error)}
	}

	// Subtract a 30s safety to avoid clock-skew expiry races with Cliq.
	ttl := time.Duration(parsed.ExpiresIn)*time.Second - 30*time.Second
	if ttl <= 0 {
		ttl = time.Minute
	}
	t.current = parsed.AccessToken
	t.expiresAt = time.Now().Add(ttl)
	t.logger.Info("cliq: token refreshed",
		"expires_in_seconds", parsed.ExpiresIn,
		"effective_ttl_seconds", int(ttl.Seconds()),
	)
	return t.current, nil
}

// refreshError represents an OAuth-refresh-endpoint failure. Distinct from
// fetcher.Error (Cliq REST API errors).
type refreshError struct {
	Status int
	Body   string
	Err    error
}

func (e *refreshError) Error() string {
	if e.Err != nil && e.Status == 0 {
		return fmt.Sprintf("cliq oauth refresh: %v", e.Err)
	}
	return fmt.Sprintf("cliq oauth refresh: http %d: %s", e.Status, e.Body)
}

func (e *refreshError) Unwrap() error { return e.Err }
