package zohocliq

import (
	"net/url"
	"strings"
	"testing"
)

func TestAuthorizeURL_StateEncoded(t *testing.T) {
	t.Setenv("CLIQ_CLIENT_ID", "client-1")
	t.Setenv("CLIQ_REDIRECT_URI", "http://localhost:9090/oauth/callback")

	tc := &tokenCredentials{}
	urlStr := tc.AuthorizeURL("abc/123 with space")
	if urlStr == "" {
		t.Fatal("expected non-empty AuthorizeURL")
	}

	u, err := url.Parse(urlStr)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	got := u.Query().Get("state")
	if got != "abc/123 with space" {
		t.Fatalf("state round-trip failed: got %q, want %q", got, "abc/123 with space")
	}
}

func TestAuthorizeURL_EmptyStateRejected(t *testing.T) {
	t.Setenv("CLIQ_CLIENT_ID", "client-1")
	t.Setenv("CLIQ_REDIRECT_URI", "http://localhost:9090/oauth/callback")

	tc := &tokenCredentials{}
	if urlStr := tc.AuthorizeURL(""); urlStr != "" {
		t.Fatalf("empty state must yield empty URL, got %q", urlStr)
	}
}

func TestAuthorizeURL_RedirectURI(t *testing.T) {
	const redirect = "https://admin.example.com/oauth/callback?path=ok"
	t.Setenv("CLIQ_CLIENT_ID", "client-1")
	t.Setenv("CLIQ_REDIRECT_URI", redirect)

	tc := &tokenCredentials{}
	urlStr := tc.AuthorizeURL("state-1")
	u, err := url.Parse(urlStr)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	got := u.Query().Get("redirect_uri")
	if got != redirect {
		t.Fatalf("redirect_uri: got %q, want %q", got, redirect)
	}

	// Sanity: required params present, base looks right.
	if u.Scheme != "https" || !strings.HasSuffix(u.Host, "zoho.com") {
		t.Fatalf("base looks wrong: scheme=%q host=%q", u.Scheme, u.Host)
	}
	for _, k := range []string{"client_id", "scope", "access_type", "prompt", "response_type"} {
		if u.Query().Get(k) == "" {
			t.Errorf("missing required query param: %s", k)
		}
	}
	if got := u.Query().Get("access_type"); got != "offline" {
		t.Errorf("access_type: got %q, want offline", got)
	}
	if got := u.Query().Get("response_type"); got != "code" {
		t.Errorf("response_type: got %q, want code", got)
	}
}

func TestAuthorizeURL_MissingConfigYieldsEmpty(t *testing.T) {
	// No env set — must yield empty.
	t.Setenv("CLIQ_CLIENT_ID", "")
	t.Setenv("CLIQ_REDIRECT_URI", "")

	tc := &tokenCredentials{}
	if got := tc.AuthorizeURL("state-1"); got != "" {
		t.Fatalf("missing config should yield empty URL, got %q", got)
	}
}
