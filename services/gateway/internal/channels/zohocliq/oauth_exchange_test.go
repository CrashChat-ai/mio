package zohocliq

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crashchat-ai/mio/services/gateway/internal/sender"
)

// stubTokenEndpoint serves both refresh_token + authorization_code grants.
// behave selects the response shape; reqs counts requests for assertions.
type behaveFn func(t *testing.T, r *http.Request, w http.ResponseWriter)

func newStubTokenEndpoint(t *testing.T, behave behaveFn) (string, *atomic.Int32) {
	t.Helper()
	var reqs atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs.Add(1)
		behave(t, r, w)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &reqs
}

// buildTokenCredentialsForTest constructs a tokenCredentials whose adapter's
// tokenSource.oauthURL points at the stub server. Mirrors the live wiring.
func buildTokenCredentialsForTest(t *testing.T, oauthURL string) *tokenCredentials {
	t.Helper()
	ts := newTokenSource("client-id", "client-secret", "refresh-token",
		withOAuthURL(oauthURL))
	a := &Adapter{tokens: ts}
	return &tokenCredentials{adapter: a}
}

func TestExchangeCode_Happy(t *testing.T) {
	t.Setenv("CLIQ_CLIENT_ID", "client-id")
	t.Setenv("CLIQ_CLIENT_SECRET", "client-secret")
	t.Setenv("CLIQ_REDIRECT_URI", "http://localhost:9090/oauth/callback")

	oauthURL, _ := newStubTokenEndpoint(t, func(t *testing.T, r *http.Request, w http.ResponseWriter) {
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Errorf("content-type: %q", got)
		}
		_ = r.ParseForm()
		if got := r.Form.Get("grant_type"); got != "authorization_code" {
			t.Errorf("grant_type: %q", got)
		}
		if got := r.Form.Get("code"); got != "auth-code-1" {
			t.Errorf("code: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600,"api_domain":"https://www.zohoapis.com","token_type":"Bearer"}`)
	})

	tc := buildTokenCredentialsForTest(t, oauthURL)
	cred, err := tc.ExchangeCode(context.Background(), "auth-code-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cred.AccessToken != "access-1" {
		t.Errorf("access: %q", cred.AccessToken)
	}
	if cred.RefreshToken != "refresh-1" {
		t.Errorf("refresh: %q", cred.RefreshToken)
	}
	if cred.Extras["api_domain"] != "https://www.zohoapis.com" {
		t.Errorf("api_domain extras missing: %+v", cred.Extras)
	}
	if cred.Extras["token_type"] != "Bearer" {
		t.Errorf("token_type extras missing: %+v", cred.Extras)
	}
	if time.Until(cred.ExpiresAt) <= 0 {
		t.Errorf("expires_at must be in the future: %v", cred.ExpiresAt)
	}
}

func TestExchangeCode_NonOK(t *testing.T) {
	t.Setenv("CLIQ_CLIENT_ID", "client-id")
	t.Setenv("CLIQ_CLIENT_SECRET", "client-secret")
	t.Setenv("CLIQ_REDIRECT_URI", "http://localhost:9090/oauth/callback")

	oauthURL, _ := newStubTokenEndpoint(t, func(_ *testing.T, r *http.Request, w http.ResponseWriter) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_code"}`))
	})

	tc := buildTokenCredentialsForTest(t, oauthURL)
	cred, err := tc.ExchangeCode(context.Background(), "bad-code")
	if err == nil {
		t.Fatalf("expected error for non-OK; got cred=%+v", cred)
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should mention status code: %v", err)
	}
	if cred.AccessToken != "" || cred.RefreshToken != "" {
		t.Errorf("partial credential leaked: %+v", cred)
	}
}

func TestExchangeCode_MalformedJSON(t *testing.T) {
	t.Setenv("CLIQ_CLIENT_ID", "client-id")
	t.Setenv("CLIQ_CLIENT_SECRET", "client-secret")
	t.Setenv("CLIQ_REDIRECT_URI", "http://localhost:9090/oauth/callback")

	oauthURL, _ := newStubTokenEndpoint(t, func(_ *testing.T, r *http.Request, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json {`))
	})

	tc := buildTokenCredentialsForTest(t, oauthURL)
	_, err := tc.ExchangeCode(context.Background(), "code-1")
	if err == nil {
		t.Fatal("expected JSON parse error")
	}
	if !strings.Contains(err.Error(), "parse json") {
		t.Errorf("error should mention parse: %v", err)
	}
}

func TestExchangeCode_ContextCanceled(t *testing.T) {
	t.Setenv("CLIQ_CLIENT_ID", "client-id")
	t.Setenv("CLIQ_CLIENT_SECRET", "client-secret")
	t.Setenv("CLIQ_REDIRECT_URI", "http://localhost:9090/oauth/callback")

	// Endpoint records hits but never responds before client-side ctx fires.
	// Using a pre-canceled ctx makes the failure deterministic without
	// needing the server to detect the closed connection.
	oauthURL, _ := newStubTokenEndpoint(t, func(_ *testing.T, r *http.Request, w http.ResponseWriter) {
		<-r.Context().Done()
	})

	tc := buildTokenCredentialsForTest(t, oauthURL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := tc.ExchangeCode(ctx, "code-1")
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected ctx.Canceled, got %v", err)
	}
}

func TestExchangeCode_MissingConfig(t *testing.T) {
	t.Setenv("CLIQ_CLIENT_ID", "")
	t.Setenv("CLIQ_CLIENT_SECRET", "")
	t.Setenv("CLIQ_REDIRECT_URI", "")

	tc := &tokenCredentials{}
	if _, err := tc.ExchangeCode(context.Background(), "code-1"); err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestRefreshCredential_DelegatesToTokenSource(t *testing.T) {
	t.Setenv("CLIQ_CLIENT_ID", "client-id")
	t.Setenv("CLIQ_CLIENT_SECRET", "client-secret")

	oauthURL, reqs := newStubTokenEndpoint(t, func(t *testing.T, r *http.Request, w http.ResponseWriter) {
		_ = r.ParseForm()
		if got := r.Form.Get("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type: %q", got)
		}
		if got := r.Form.Get("refresh_token"); got != "rt-1" {
			t.Errorf("refresh_token: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"access_token":"fresh-access","expires_in":3600}`)
	})

	tc := buildTokenCredentialsForTest(t, oauthURL)
	cur := tokenCredEpsilon(t, "rt-1")
	out, err := tc.RefreshCredential(context.Background(), cur)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.AccessToken != "fresh-access" {
		t.Errorf("access: %q", out.AccessToken)
	}
	if out.RefreshToken != "rt-1" {
		t.Errorf("refresh preserved: %q", out.RefreshToken)
	}
	if reqs.Load() != 1 {
		t.Errorf("expected exactly 1 OAuth request, got %d", reqs.Load())
	}
}

// tokenCredEpsilon is a tiny helper that builds a sender.Credential with
// just the RefreshToken populated. Inline to avoid leaking helper into prod.
func tokenCredEpsilon(t *testing.T, refresh string) sender.Credential {
	t.Helper()
	return sender.Credential{RefreshToken: refresh}
}
