package zohocliq

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTokenSourceRefreshesOnEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"fresh","expires_in":3600}`))
	}))
	defer srv.Close()

	ts := newTokenSource("cid", "csec", "rt", withOAuthURL(srv.URL))
	tok, err := ts.Get(t.Context())
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if tok != "fresh" {
		t.Errorf("token = %q, want fresh", tok)
	}
}

func TestTokenSourceCachesWithinSafetyWindow(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"access_token":"a","expires_in":3600}`))
	}))
	defer srv.Close()

	ts := newTokenSource("c", "s", "r", withOAuthURL(srv.URL))
	for range 5 {
		_, _ = ts.Get(t.Context())
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("OAuth hits = %d, want 1 (caching)", got)
	}
}

func TestTokenSourceInvalidateForcesRefresh(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"access_token":"a","expires_in":3600}`))
	}))
	defer srv.Close()

	ts := newTokenSource("c", "s", "r", withOAuthURL(srv.URL))
	_, _ = ts.Get(t.Context())
	ts.Invalidate()
	_, _ = ts.Get(t.Context())
	if got := hits.Load(); got != 2 {
		t.Errorf("OAuth hits after invalidate = %d, want 2", got)
	}
}

func TestTokenSourceReturnsRefreshErrorOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	defer srv.Close()

	ts := newTokenSource("c", "s", "r", withOAuthURL(srv.URL))
	_, err := ts.Get(t.Context())
	if err == nil {
		t.Fatal("expected refreshError")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error = %q, want 401 status", err.Error())
	}
}

func TestTokenSourceNilOnAllEmpty(t *testing.T) {
	if ts := newTokenSource("", "", ""); ts != nil {
		t.Errorf("expected nil, got %+v", ts)
	}
}

func TestStaticTokenSourceIgnoresInvalidate(t *testing.T) {
	ts := staticTokenSource("static")
	ts.Invalidate()
	tok, err := ts.Get(t.Context())
	if err != nil || tok != "static" {
		t.Errorf("static token after Invalidate: tok=%q err=%v", tok, err)
	}
}

func TestTokenSourceRefreshTTLSubtracts30s(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"a","expires_in":3600}`))
	}))
	defer srv.Close()

	ts := newTokenSource("c", "s", "r", withOAuthURL(srv.URL))
	_, _ = ts.Get(t.Context())
	ttl := time.Until(ts.expiresAt)
	if ttl > 3600*time.Second-29*time.Second || ttl < 3600*time.Second-31*time.Second {
		t.Errorf("ttl = %v, want ~3570s (3600 - 30s safety)", ttl)
	}
}
