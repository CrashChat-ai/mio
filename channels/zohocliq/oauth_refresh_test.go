package zohocliq

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/crashchat-ai/mio/pkg/channels"
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
		_, _ = fmt.Fprintln(w, `{"access_token":"new-access","refresh_token":"rotated-refresh","expires_in":3600}`)
	})

	tc := buildTokenCredentialsForTest(t, oauthURL)
	out, err := tc.RefreshCredential(context.Background(), channels.Credential{RefreshToken: "rt-original"})
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
		_, _ = fmt.Fprintln(w, `{"access_token":"new-access","expires_in":3600}`)
	})

	tc := buildTokenCredentialsForTest(t, oauthURL)
	cur := channels.Credential{
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

// TestRefreshCredential_Preconditions covers the early-return paths
// before any HTTP request is made.
func TestRefreshCredential_Preconditions(t *testing.T) {
	tests := []struct {
		name      string
		clientID  string
		secret    string
		input     channels.Credential
		wantSubst string // substring expected in err.Error()
	}{
		{
			name:      "empty refresh_token rejects",
			clientID:  "client-id",
			secret:    "client-secret",
			input:     channels.Credential{},
			wantSubst: "empty refresh_token",
		},
		{
			name:      "missing client_id rejects",
			clientID:  "",
			secret:    "client-secret",
			input:     channels.Credential{RefreshToken: "rt-1"},
			wantSubst: "missing CLIQ_CLIENT_ID",
		},
		{
			name:      "missing client_secret rejects",
			clientID:  "client-id",
			secret:    "",
			input:     channels.Credential{RefreshToken: "rt-1"},
			wantSubst: "missing CLIQ_CLIENT_ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CLIQ_CLIENT_ID", tt.clientID)
			t.Setenv("CLIQ_CLIENT_SECRET", tt.secret)
			tc := &tokenCredentials{adapter: &Adapter{tokens: &tokenSource{}}}
			_, err := tc.RefreshCredential(context.Background(), tt.input)
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantSubst)
			}
			if got := err.Error(); !strings.Contains(got, tt.wantSubst) {
				t.Errorf("err %q missing substring %q", got, tt.wantSubst)
			}
		})
	}
}
