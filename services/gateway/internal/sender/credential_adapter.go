package sender

import (
	"context"
	"time"
)

// Credential carries an opaque per-account credential the adapter needs to
// authenticate outbound calls. The fields are a superset across the three
// known flavors:
//   - oauth2_refresh (Zoho Cliq, Slack): AccessToken + RefreshToken + ExpiresAt
//   - bot_token (Telegram, Discord bot): AccessToken only; RefreshToken empty
//   - hmac_webhook (inbound-only): no AccessToken; Extras carries the shared
//     secret keyed by adapter-defined name (e.g. "webhook_secret")
//
// Extras is the escape hatch for adapter-private fields (Zoho's
// `api_domain`, Slack's `team_id`, etc.). Promote a key to a typed field
// once two adapters store the same key.
//
// Credential is what gets encrypted and persisted in the `credentials`
// table (see Phase 3). The admin API never returns the plaintext to
// callers; rotations are server-side only.
type Credential struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	Extras       map[string]string
}

// CredentialAdapter encapsulates the OAuth / bot-token / webhook-secret
// lifecycle for a single channel adapter. Adapters return their
// CredentialAdapter via Adapter.Credentials().
//
// AuthorizeURL + ExchangeCode together implement the OAuth 2.0 authorization-
// code flow. They are paired: AuthorizeURL builds the consent URL the operator
// visits, and ExchangeCode redeems the resulting `code` for a Credential.
// Non-OAuth adapters return informative errors from these methods (typed
// sentinels so the admin layer can branch on auth_kind).
//
// RefreshCredential refreshes an expiring access token. For
// oauth2_refresh adapters, this is the long-running steady-state operation
// (every ~hour). For bot_token / hmac_webhook adapters, RefreshCredential
// returns the input unchanged.
type CredentialAdapter interface {
	// AuthorizeURL synthesises the platform's consent URL with the given
	// CSRF state. The admin API uses this for StartInstall.
	//
	// state MUST be non-empty (crypto-random caller-supplied). Implementations
	// return an empty string when their auth_kind has no authorize step.
	AuthorizeURL(state string) string

	// ExchangeCode redeems an authorization code for a Credential.
	// Called by CompleteInstall after the operator hits /oauth/callback.
	// The returned Credential is opaque to the caller; the admin layer
	// passes it through to the encrypted store.
	ExchangeCode(ctx context.Context, code string) (Credential, error)

	// RefreshCredential refreshes an expiring credential. For
	// oauth2_refresh, the input's RefreshToken is consumed and a new
	// Credential is returned. For other flavors, returns cur unchanged.
	RefreshCredential(ctx context.Context, cur Credential) (Credential, error)
}
