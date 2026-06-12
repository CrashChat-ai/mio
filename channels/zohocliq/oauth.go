package zohocliq

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/crashchat-ai/mio/pkg/channels"
)

// oauthBodyCap caps the OAuth-endpoint response body to 1 MB. Matches the
// inbound webhook handler's LimitReader so a misbehaving (or attacker-
// controlled, in a non-TLS deploy) token endpoint cannot OOM the gateway.
const oauthBodyCap = 1 << 20

// errBodyCap caps the portion of an upstream body included in a typed
// error message. Zoho occasionally echoes request params on 4xx; keep
// only enough for ops to triage without leaking the full body into log
// aggregators / admin-API responses.
const errBodyCap = 512

// Zoho OAuth authorize endpoint — pairs with the token endpoint in token.go.
// Override via withAuthorizeURL in tests.
const authorizeDefaultURL = "https://accounts.zoho.com/oauth/v2/auth"

// cliqOAuthScope is the OAuth scope required for bot sends plus history
// reconciliation. Operators can override it with CLIQ_OAUTH_SCOPE when they
// intentionally want write-only installs.
const cliqOAuthScope = "ZohoCliq.Webhooks.CREATE,ZohoCliq.Channels.READ,ZohoCliq.Messages.CREATE,ZohoCliq.Messages.READ"

// tokenCredentials wraps *Adapter to satisfy channels.CredentialAdapter.
// Reuses tokenSource for the refresh path; AuthorizeURL + ExchangeCode are
// net-new code paths (the existing adapter only refreshed long-lived
// refresh tokens; the OAuth bootstrap was operator-manual before this
// phase).
//
// AuthorizeURL / ExchangeCode read configuration from environment lazily so
// that tests can populate via t.Setenv without touching Adapter.
type tokenCredentials struct {
	adapter      *Adapter
	authorizeURL string // overridable for tests; empty falls back to authorizeDefaultURL
}

// AuthorizeURL builds the Zoho OAuth consent URL with the given CSRF state.
// state MUST be non-empty (caller passes a crypto/rand-generated nonce);
// returns an empty string if state is empty so callers cannot accidentally
// emit an unprotected URL.
//
// Reads from env (server-side, never client-side):
//
//	CLIQ_CLIENT_ID         — OAuth client id (also used by tokenSource refresh)
//	CLIQ_REDIRECT_URI      — admin's /oauth/callback URL (operator-configured)
//	CLIQ_OAUTH_SCOPE       — optional override; defaults to cliqOAuthScope
//
// Per Zoho OAuth docs (https://www.zoho.com/cliq/help/restapi/v2/),
// the consent URL is:
//
//	https://accounts.zoho.com/oauth/v2/auth
//	  ?response_type=code
//	  &client_id={id}
//	  &scope={scope}
//	  &redirect_uri={uri}
//	  &access_type=offline   (required to receive refresh_token)
//	  &prompt=consent        (force consent so refresh_token is reissued)
//	  &state={state}
func (t *tokenCredentials) AuthorizeURL(state string) string {
	if state == "" {
		return ""
	}
	clientID := os.Getenv("CLIQ_CLIENT_ID")
	redirectURI := os.Getenv("CLIQ_REDIRECT_URI")
	if clientID == "" || redirectURI == "" {
		return ""
	}
	scope := os.Getenv("CLIQ_OAUTH_SCOPE")
	if scope == "" {
		scope = cliqOAuthScope
	}
	q := url.Values{
		"response_type": {"code"},
		"client_id":     {clientID},
		"scope":         {scope},
		"redirect_uri":  {redirectURI},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
		"state":         {state},
	}
	base := t.authorizeURL
	if base == "" {
		base = authorizeDefaultURL
	}
	return base + "?" + q.Encode()
}

// ExchangeCode redeems an authorization code for an OAuth credential.
// POSTs to the Zoho token endpoint with grant_type=authorization_code.
//
// Returns:
//   - on 2xx with access_token: Credential populated with access + refresh +
//     expires_at (now + expires_in - 30s clock-skew safety).
//   - on non-2xx: typed error including status + body for ops to triage.
//   - on malformed JSON: typed parse error; no partial credential persisted.
//   - on ctx cancellation: ctx.Err() surfaces unchanged.
//
// Reads CLIQ_CLIENT_ID / CLIQ_CLIENT_SECRET / CLIQ_REDIRECT_URI from env.
// The OAuth token endpoint is the same as token.go's refresh path
// (oauthDefaultURL or overridden via the tokenSource).
func (t *tokenCredentials) ExchangeCode(ctx context.Context, code string) (channels.Credential, error) {
	clientID := os.Getenv("CLIQ_CLIENT_ID")
	clientSecret := os.Getenv("CLIQ_CLIENT_SECRET")
	redirectURI := os.Getenv("CLIQ_REDIRECT_URI")
	if clientID == "" || clientSecret == "" || redirectURI == "" {
		return channels.Credential{}, fmt.Errorf("zohocliq: ExchangeCode: missing CLIQ_CLIENT_ID/CLIQ_CLIENT_SECRET/CLIQ_REDIRECT_URI")
	}
	if code == "" {
		return channels.Credential{}, fmt.Errorf("zohocliq: ExchangeCode: empty code")
	}

	tokenURL := oauthDefaultURL
	if t.adapter != nil && t.adapter.tokens != nil && t.adapter.tokens.oauthURL != "" {
		tokenURL = t.adapter.tokens.oauthURL
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"redirect_uri":  {redirectURI},
		"code":          {code},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return channels.Credential{}, fmt.Errorf("zohocliq: ExchangeCode: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	httpClient := http.DefaultClient
	if t.adapter != nil && t.adapter.tokens != nil && t.adapter.tokens.httpClient != nil {
		httpClient = t.adapter.tokens.httpClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return channels.Credential{}, fmt.Errorf("zohocliq: ExchangeCode: http: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, _ := io.ReadAll(io.LimitReader(resp.Body, oauthBodyCap))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return channels.Credential{}, fmt.Errorf("zohocliq: ExchangeCode: http %d: %s",
			resp.StatusCode, truncate(string(body), errBodyCap))
	}

	var parsed struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		APIDomain    string `json:"api_domain"`
		TokenType    string `json:"token_type"`
		Error        string `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return channels.Credential{}, fmt.Errorf("zohocliq: ExchangeCode: parse json: %w", err)
	}
	if parsed.AccessToken == "" {
		return channels.Credential{}, fmt.Errorf("zohocliq: ExchangeCode: missing access_token (error=%q)", parsed.Error)
	}

	ttl := time.Duration(parsed.ExpiresIn)*time.Second - 30*time.Second
	if ttl <= 0 {
		ttl = time.Minute
	}
	cred := channels.Credential{
		AccessToken:  parsed.AccessToken,
		RefreshToken: parsed.RefreshToken,
		ExpiresAt:    time.Now().Add(ttl),
		Extras:       map[string]string{},
	}
	if parsed.APIDomain != "" {
		cred.Extras["api_domain"] = parsed.APIDomain
	}
	if parsed.TokenType != "" {
		cred.Extras["token_type"] = parsed.TokenType
	}
	return cred, nil
}

// RefreshCredential refreshes an OAuth access token using the supplied
// credential's RefreshToken. Delegates to tokenSource.refreshLocked via a
// transient tokenSource that mirrors the stored credential — keeps the
// proven refresh code path as a single implementation.
//
// Returns a new Credential with rotated AccessToken + ExpiresAt. The input's
// RefreshToken is preserved (Zoho re-uses refresh tokens until explicitly
// revoked); Extras is copied to avoid alias-mutation between caller and
// returned Credential.
func (t *tokenCredentials) RefreshCredential(ctx context.Context, cur channels.Credential) (channels.Credential, error) {
	if cur.RefreshToken == "" {
		return channels.Credential{}, fmt.Errorf("zohocliq: RefreshCredential: empty refresh_token")
	}
	clientID := os.Getenv("CLIQ_CLIENT_ID")
	clientSecret := os.Getenv("CLIQ_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		return channels.Credential{}, fmt.Errorf("zohocliq: RefreshCredential: missing CLIQ_CLIENT_ID/CLIQ_CLIENT_SECRET")
	}

	oauthURL := oauthDefaultURL
	if t.adapter != nil && t.adapter.tokens != nil && t.adapter.tokens.oauthURL != "" {
		oauthURL = t.adapter.tokens.oauthURL
	}
	ts := newTokenSource(clientID, clientSecret, cur.RefreshToken, withOAuthURL(oauthURL))
	if ts == nil {
		return channels.Credential{}, fmt.Errorf("zohocliq: RefreshCredential: build token source")
	}
	access, err := ts.Get(ctx)
	if err != nil {
		return channels.Credential{}, fmt.Errorf("zohocliq: RefreshCredential: %w", err)
	}
	ts.mu.Lock()
	expiresAt := ts.expiresAt
	ts.mu.Unlock()

	var extras map[string]string
	if cur.Extras != nil {
		extras = maps.Clone(cur.Extras)
	}
	out := channels.Credential{
		AccessToken:  access,
		RefreshToken: cur.RefreshToken,
		ExpiresAt:    expiresAt,
		Extras:       extras,
	}
	return out, nil
}

// truncate caps s to at most n bytes. Used for embedding upstream bodies
// in typed errors without leaking the full body into logs/admin responses.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}
