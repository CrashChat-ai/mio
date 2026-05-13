package zohocliq

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stubOAuthServer returns an httptest.Server that responds to refresh-token
// requests with a JSON body containing access_token + expires_in. Counts
// requests via the atomic counter so tests can assert dedupe behaviour.
func stubOAuthServer(t *testing.T, accessToken string, expiresIn int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("expected form content-type, got %q", r.Header.Get("Content-Type"))
		}
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" {
			t.Errorf("expected grant_type=refresh_token, got %q", r.Form.Get("grant_type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":%q,"expires_in":%d,"api_domain":"https://www.zohoapis.com","token_type":"Bearer"}`,
			accessToken, expiresIn)
	}))
	t.Cleanup(srv.Close)
	return srv, &count
}

func newTestTokenSource(t *testing.T, oauthURL string) *tokenSource {
	t.Helper()
	return newTokenSource("client-id", "client-secret", "refresh-token",
		withOAuthURL(oauthURL))
}

func TestTokenSource_GetCachesUntilExpiry(t *testing.T) {
	srv, count := stubOAuthServer(t, "access-1", 3600)
	ts := newTestTokenSource(t, srv.URL)

	// First call: cold cache → must hit OAuth server.
	tok1, err := ts.Get(context.Background())
	if err != nil {
		t.Fatalf("first Get: unexpected error: %v", err)
	}
	if tok1 != "access-1" {
		t.Fatalf("first Get: expected access-1, got %q", tok1)
	}
	if got := count.Load(); got != 1 {
		t.Fatalf("expected 1 OAuth request after first Get, got %d", got)
	}

	// Second call within TTL: must serve from cache.
	tok2, err := ts.Get(context.Background())
	if err != nil {
		t.Fatalf("second Get: unexpected error: %v", err)
	}
	if tok2 != tok1 {
		t.Fatalf("expected cached token %q, got %q", tok1, tok2)
	}
	if got := count.Load(); got != 1 {
		t.Fatalf("expected still 1 OAuth request after cached Get, got %d", got)
	}
}

func TestTokenSource_RefreshesNearExpiry(t *testing.T) {
	srv, count := stubOAuthServer(t, "access-1", 3600)
	ts := newTestTokenSource(t, srv.URL)

	// Seed cache with a token that expires within the safety window.
	ts.mu.Lock()
	ts.current = "stale"
	ts.expiresAt = time.Now().Add(4 * time.Minute) // < refreshSafetyWindow (5m)
	ts.mu.Unlock()

	tok, err := ts.Get(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "access-1" {
		t.Fatalf("expected refreshed token access-1, got %q", tok)
	}
	if got := count.Load(); got != 1 {
		t.Fatalf("expected 1 OAuth request (refresh near expiry), got %d", got)
	}
}

func TestTokenSource_ConcurrentGetsDedupe(t *testing.T) {
	// OAuth server adds a small delay so the stampede actually contends on
	// the mutex — without the delay, goroutines may serialise too cleanly
	// for the test to be meaningful.
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-x","expires_in":3600}`))
	}))
	defer srv.Close()

	ts := newTestTokenSource(t, srv.URL)

	const N = 10
	var wg sync.WaitGroup
	wg.Add(N)
	tokens := make([]string, N)
	errs := make([]error, N)
	for i := range N {
		go func(i int) {
			defer wg.Done()
			tokens[i], errs[i] = ts.Get(context.Background())
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
		if tokens[i] != "access-x" {
			t.Fatalf("goroutine %d: expected access-x, got %q", i, tokens[i])
		}
	}
	if got := count.Load(); got != 1 {
		t.Fatalf("expected exactly 1 OAuth request (stampede dedupe), got %d", got)
	}
}

func TestTokenSource_RefreshFailureReturnsRefreshError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	defer srv.Close()

	ts := newTestTokenSource(t, srv.URL)
	_, err := ts.Get(context.Background())
	if err == nil {
		t.Fatal("expected error from failed refresh")
	}
	var rerr *refreshError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *refreshError, got %T (%v)", err, err)
	}
	if rerr.Status != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rerr.Status)
	}
	if rerr.Reason() != "refresh_failed" {
		t.Fatalf("expected Reason()=refresh_failed, got %q", rerr.Reason())
	}
	if rerr.IsRetryable() {
		t.Fatal("refreshError must never be retryable")
	}
}

func TestTokenSource_RefreshFailureOn200WithErrorBody(t *testing.T) {
	// Zoho quirk: sometimes returns HTTP 200 with {"error":"..."} body.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"invalid_code"}`))
	}))
	defer srv.Close()

	ts := newTestTokenSource(t, srv.URL)
	_, err := ts.Get(context.Background())
	if err == nil {
		t.Fatal("expected error when access_token missing in 200 body")
	}
	var rerr *refreshError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *refreshError, got %T", err)
	}
}

func TestTokenSource_InvalidateForcesRefetch(t *testing.T) {
	srv, count := stubOAuthServer(t, "access-1", 3600)
	ts := newTestTokenSource(t, srv.URL)

	// Warm the cache.
	if _, err := ts.Get(context.Background()); err != nil {
		t.Fatalf("warm Get: %v", err)
	}
	if got := count.Load(); got != 1 {
		t.Fatalf("expected 1 OAuth request after warm Get, got %d", got)
	}

	ts.Invalidate()

	// Next Get must hit the server again.
	if _, err := ts.Get(context.Background()); err != nil {
		t.Fatalf("post-invalidate Get: %v", err)
	}
	if got := count.Load(); got != 2 {
		t.Fatalf("expected 2 OAuth requests after Invalidate+Get, got %d", got)
	}
}

func TestNewTokenSource_AllEmptyReturnsNil(t *testing.T) {
	if ts := newTokenSource("", "", ""); ts != nil {
		t.Fatalf("expected nil for all-empty creds, got %+v", ts)
	}
}
