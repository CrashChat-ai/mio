package zohocliq

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// newTestAdapter creates an Adapter pointed at a test Cliq server. The
// tokenSource is wired to a stub OAuth server that mints `access-token` —
// this keeps the auth path real (single code path through doWithSelfHeal)
// instead of letting tests bypass it.
func newTestAdapter(t *testing.T, cliqBaseURL string) (*Adapter, *atomic.Int32) {
	t.Helper()
	oauthSrv, oauthCount := stubOAuthServer(t, "access-token", 3600)
	return &Adapter{
		baseURL: cliqBaseURL,
		botName: "test-bot",
		tokens: newTokenSource("client-id", "client-secret", "refresh-token",
			withOAuthURL(oauthSrv.URL)),
		httpClient: &http.Client{Timeout: 5 * time.Second},
		logger:     slog.Default(),
	}, oauthCount
}

// testCmd builds a SendCommand with the cliq_channel_name attribute set so
// Send() doesn't short-circuit on missing channel name.
func testCmd(id, text string) *miov1.SendCommand {
	return &miov1.SendCommand{
		Id:                     id,
		ConversationExternalId: "chat-abc",
		Text:                   text,
		Attributes:             map[string]string{"cliq_channel_name": "test-channel"},
	}
}

func TestSend_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Zoho-oauthtoken access-token" {
			t.Errorf("expected Zoho-oauthtoken access-token, got %s", r.Header.Get("Authorization"))
		}
		// Bot endpoint returns 204 No Content on success.
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	a, _ := newTestAdapter(t, srv.URL)
	cmd := testCmd("cmd-1", "hello")

	extID, err := a.Send(context.Background(), cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if extID != "cmd-1" {
		t.Fatalf("expected cmd-1 (synthesised), got %s", extID)
	}
}

func TestSend_MissingChannelName(t *testing.T) {
	a, _ := newTestAdapter(t, "http://unused")
	cmd := &miov1.SendCommand{Id: "cmd-2", Text: "hi"}
	_, err := a.Send(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error for missing cliq_channel_name attribute")
	}
}

func TestSend_HTTP429_ReturnsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()

	a, _ := newTestAdapter(t, srv.URL)
	cmd := testCmd("cmd-429", "hi")

	_, err := a.Send(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error")
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *HTTPError, got %T", err)
	}
	if !httpErr.IsRateLimited() {
		t.Fatal("expected IsRateLimited() true")
	}
	if httpErr.RetryAfterSeconds() != 7 {
		t.Fatalf("expected RetryAfter=7, got %d", httpErr.RetryAfterSeconds())
	}
}

func TestSend_HTTP5xx_IsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	a, _ := newTestAdapter(t, srv.URL)
	cmd := testCmd("cmd-5xx", "hi")
	_, err := a.Send(context.Background(), cmd)
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *HTTPError, got %T", err)
	}
	if !httpErr.IsRetryable() {
		t.Fatal("expected IsRetryable() true for 500")
	}
	if httpErr.IsRateLimited() {
		t.Fatal("expected IsRateLimited() false for 500")
	}
}

// TestSend_SecondConsecutive401Terminates verifies that a 401 from a fresh
// token surfaces as terminal auth failure (not retried into infinite loop).
// Replaces the old TestSend_HTTP401_NotRetryable — same intent, but now
// confirms self-heal also gave up after one retry.
func TestSend_SecondConsecutive401Terminates(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	a, _ := newTestAdapter(t, srv.URL)
	cmd := testCmd("cmd-401-401", "hi")

	_, err := a.Send(context.Background(), cmd)
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *HTTPError, got %T", err)
	}
	if httpErr.IsRetryable() {
		t.Fatal("expected IsRetryable() false for 401")
	}
	if httpErr.StatusCode() != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", httpErr.StatusCode())
	}
	// Self-heal must have run exactly once: attempt 1 (401) → invalidate →
	// attempt 2 (401) → surface. Total Cliq attempts = 2.
	if got := attempts.Load(); got != 2 {
		t.Fatalf("expected exactly 2 Cliq attempts (1 + self-heal retry), got %d", got)
	}
}

// TestSend_SelfHealsOn401 verifies that a single transient 401 is recovered
// transparently — Cliq returns 401 first, then 200 after token re-fetch.
func TestSend_SelfHealsOn401(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent) // 204 = success on bot endpoint
	}))
	defer srv.Close()

	a, oauthCount := newTestAdapter(t, srv.URL)
	cmd := testCmd("cmd-heal", "hi")

	extID, err := a.Send(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected self-heal recovery, got error: %v", err)
	}
	if extID != "cmd-heal" {
		t.Fatalf("expected cmd-heal, got %q", extID)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("expected 2 Cliq attempts (1 fail + 1 heal), got %d", got)
	}
	// OAuth server must have been hit twice: cold cache + post-invalidate refetch.
	if got := oauthCount.Load(); got != 2 {
		t.Fatalf("expected 2 OAuth refreshes (initial + post-invalidate), got %d", got)
	}
}

// TestSend_RefreshFailureSurfacesAsRefreshFailed verifies the OAuth-endpoint
// error path: when refresh itself fails, the pool sees a refreshError whose
// Reason() returns "refresh_failed" — distinguishable from regular auth
// failures so on-call knows to rotate the refresh token, not the access token.
func TestSend_RefreshFailureSurfacesAsRefreshFailed(t *testing.T) {
	// Cliq server irrelevant — refresh fails before we even build the request.
	cliqSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("Cliq must not be hit when refresh fails")
		w.WriteHeader(http.StatusOK)
	}))
	defer cliqSrv.Close()

	// OAuth server returns 401 (revoked refresh token).
	oauthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer oauthSrv.Close()

	a := &Adapter{
		baseURL: cliqSrv.URL,
		botName: "test-bot",
		tokens: newTokenSource("client-id", "client-secret", "refresh-token",
			withOAuthURL(oauthSrv.URL)),
		httpClient: &http.Client{Timeout: 5 * time.Second},
		logger:     slog.Default(),
	}
	cmd := testCmd("cmd-refresh-fail", "hi")

	_, err := a.Send(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error from failed refresh")
	}
	var rerr *refreshError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *refreshError, got %T (%v)", err, err)
	}
	if rerr.Reason() != "refresh_failed" {
		t.Fatalf("expected Reason()=refresh_failed, got %q", rerr.Reason())
	}
}

// TestNewAdapter_PanicsOnPartialOAuthConfig verifies the fail-fast behavior:
// if 1 or 2 of the 3 OAuth env vars are set (typo / partial deploy), the
// constructor panics rather than booting into a 401 storm.
func TestNewAdapter_PanicsOnPartialOAuthConfig(t *testing.T) {
	cases := []struct {
		name string
		envs map[string]string
	}{
		{"only client_id", map[string]string{"CLIQ_CLIENT_ID": "x"}},
		{"client_id + secret", map[string]string{"CLIQ_CLIENT_ID": "x", "CLIQ_CLIENT_SECRET": "y"}},
		{"only refresh_token", map[string]string{"CLIQ_REFRESH_TOKEN": "z"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Clear all then set only the case's vars.
			t.Setenv("CLIQ_CLIENT_ID", "")
			t.Setenv("CLIQ_CLIENT_SECRET", "")
			t.Setenv("CLIQ_REFRESH_TOKEN", "")
			for k, v := range tc.envs {
				t.Setenv(k, v)
			}
			defer func() {
				if r := recover(); r == nil {
					t.Fatal("expected NewAdapter to panic on partial config")
				}
			}()
			_ = NewAdapter()
		})
	}
}

// TestNewAdapter_AllEmptyReturnsNilTokens verifies that with NO OAuth env vars,
// the adapter constructs cleanly but with tokens=nil — Send/Edit will return
// an explicit error. This keeps test imports of the package working.
func TestNewAdapter_AllEmptyReturnsNilTokens(t *testing.T) {
	t.Setenv("CLIQ_CLIENT_ID", "")
	t.Setenv("CLIQ_CLIENT_SECRET", "")
	t.Setenv("CLIQ_REFRESH_TOKEN", "")
	a := NewAdapter()
	if a.tokens != nil {
		t.Fatal("expected tokens=nil with no OAuth env")
	}
	cmd := testCmd("cmd-no-tokens", "hi")
	_, err := a.Send(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error when tokens not configured")
	}
}

func TestEdit_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	a, _ := newTestAdapter(t, srv.URL)
	cmd := &miov1.SendCommand{
		Id:                     "cmd-edit",
		ConversationExternalId: "chat-abc",
		EditOfExternalId:       "cliq-msg-999",
		Text:                   "updated text",
	}
	if err := a.Edit(context.Background(), cmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEdit_MissingExternalID(t *testing.T) {
	a, _ := newTestAdapter(t, "http://unused")
	cmd := &miov1.SendCommand{
		Id:                     "cmd-edit",
		ConversationExternalId: "chat-abc",
		// EditOfExternalId intentionally empty
	}
	if err := a.Edit(context.Background(), cmd); err == nil {
		t.Fatal("expected error for missing edit_of_external_id")
	}
}

// TestEdit_SelfHealsOn401 verifies symmetry — Edit also recovers from a
// stale-token 401 (Send and Edit share doWithSelfHeal).
func TestEdit_SelfHealsOn401(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	a, _ := newTestAdapter(t, srv.URL)
	cmd := &miov1.SendCommand{
		Id:                     "cmd-edit-heal",
		ConversationExternalId: "chat-abc",
		EditOfExternalId:       "cliq-msg-1",
		Text:                   "edited",
	}
	if err := a.Edit(context.Background(), cmd); err != nil {
		t.Fatalf("expected self-heal recovery, got %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("expected 2 Cliq attempts, got %d", got)
	}
}

func TestAdapter_Interface(t *testing.T) {
	a, _ := newTestAdapter(t, "http://unused")
	if a.ChannelType() != cliqChannelType {
		t.Fatalf("expected %s, got %s", cliqChannelType, a.ChannelType())
	}
	if a.MaxDeliver() != cliqMaxDeliver {
		t.Fatalf("expected %d, got %d", cliqMaxDeliver, a.MaxDeliver())
	}
	cmd := &miov1.SendCommand{AccountId: "acct-1"}
	if key := a.RateLimitKey(cmd); key != "" {
		t.Fatalf("expected empty rate limit key, got %q", key)
	}
}
