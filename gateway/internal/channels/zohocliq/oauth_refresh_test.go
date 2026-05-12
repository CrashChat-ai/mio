package zohocliq

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/crashchat-ai/mio/gateway/internal/sender"
)

// TestRefreshCredential_PreservesCallersRefreshToken pins the current
// behaviour: the input RefreshToken is preserved even when the OAuth
// response includes a rotated refresh_token. Zoho's docs describe refresh
// tokens as long-lived (rotation is rare in our tier), so the adapter
// keeps the caller-provided token rather than promoting any rotated value.
// If we ever flip to honoring rotation, this test must be updated.
func TestRefreshCredential_PreservesCallersRefreshToken(t *testing.T) {
	t.Setenv("CLIQ_CLIENT_ID", "client-id")
	t.Setenv("CLIQ_CLIENT_SECRET", "client-secret")

	oauthURL, _ := newStubTokenEndpoint(t, func(_ *testing.T, _ *http.Request, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"access_token":"new-access","refresh_token":"rotated-refresh","expires_in":3600}`)
	})

	tc := buildTokenCredentialsForTest(t, oauthURL)
	out, err := tc.RefreshCredential(context.Background(), sender.Credential{RefreshToken: "rt-original"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.AccessToken != "new-access" {
		t.Errorf("access: got %q", out.AccessToken)
	}
	if out.RefreshToken != "rt-original" {
		t.Errorf("refresh_token should be preserved unchanged; got %q", out.RefreshToken)
	}
}

// TestRefreshCredential_ExtrasIndependentlyMutable verifies the returned
// Credential's Extras map is not aliased to the caller's. Mutating one
// must not affect the other (caller may stash the input in long-lived
// state; the rotated credential gets written to the credentials table).
func TestRefreshCredential_ExtrasIndependentlyMutable(t *testing.T) {
	t.Setenv("CLIQ_CLIENT_ID", "client-id")
	t.Setenv("CLIQ_CLIENT_SECRET", "client-secret")

	oauthURL, _ := newStubTokenEndpoint(t, func(_ *testing.T, _ *http.Request, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"access_token":"new-access","expires_in":3600}`)
	})

	tc := buildTokenCredentialsForTest(t, oauthURL)
	cur := sender.Credential{
		RefreshToken: "rt-1",
		Extras:       map[string]string{"api_domain": "https://www.zohoapis.com"},
	}
	out, err := tc.RefreshCredential(context.Background(), cur)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Extras == nil {
		t.Fatal("Extras should be cloned, not nil")
	}
	out.Extras["api_domain"] = "https://eu.zohoapis.com"
	out.Extras["new_key"] = "new_value"
	if cur.Extras["api_domain"] != "https://www.zohoapis.com" {
		t.Errorf("caller's Extras was mutated through alias: %v", cur.Extras)
	}
	if _, ok := cur.Extras["new_key"]; ok {
		t.Errorf("caller's Extras grew through alias: %v", cur.Extras)
	}
}

// TestRefreshCredential_EmptyRefreshToken_Errors guards the precondition.
func TestRefreshCredential_EmptyRefreshToken_Errors(t *testing.T) {
	t.Setenv("CLIQ_CLIENT_ID", "client-id")
	t.Setenv("CLIQ_CLIENT_SECRET", "client-secret")
	tc := &tokenCredentials{adapter: &Adapter{tokens: &tokenSource{}}}
	if _, err := tc.RefreshCredential(context.Background(), sender.Credential{}); err == nil {
		t.Fatal("expected error for empty refresh_token")
	}
}

// TestRefreshCredential_MissingConfig errors when env is unset.
func TestRefreshCredential_MissingConfig(t *testing.T) {
	t.Setenv("CLIQ_CLIENT_ID", "")
	t.Setenv("CLIQ_CLIENT_SECRET", "")
	tc := &tokenCredentials{adapter: &Adapter{tokens: &tokenSource{}}}
	if _, err := tc.RefreshCredential(context.Background(), sender.Credential{RefreshToken: "rt-1"}); err == nil {
		t.Fatal("expected error for missing client_id/secret")
	}
}
