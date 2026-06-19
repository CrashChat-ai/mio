---
title: Adapter Authoring Guide
description: Build a production-ready MIO channel adapter end-to-end, verified against the pkg/channels contract and the reference Zoho Cliq implementation.
---

# Adapter Authoring Guide

This guide walks through building a production-ready channel adapter for MIO by implementing a hypothetical Slack adapter. Every code reference is verified against the actual contract in `pkg/channels/` and the reference Zoho Cliq implementation in `channels/zohocliq/`.

**Target audience:** Community members integrating new chat platforms.

**Success definition:** You can scaffold a new adapter touching ONLY `channels/<slug>/` + `channels/all/all.go` + `proto/channels.yaml`, following this guide alone.

---

## 1. The Adapter Contract

All adapters implement the `Adapter` interface defined in `pkg/channels/adapter.go`:

```go
type Adapter interface {
    Send(ctx context.Context, cmd *miov1.SendCommand) (externalID string, err error)
    Edit(ctx context.Context, cmd *miov1.SendCommand) error
    ChannelType() string
    MaxDeliver() int
    RateLimitKey(cmd *miov1.SendCommand) string
    Capabilities() *miov1.ChannelCapabilities
    Inbound() InboundAdapter
    Credentials() CredentialAdapter
}
```

| Method | Purpose | Returns |
|--------|---------|---------|
| `Send` | Deliver a new outbound message to the platform. Return the platform's message ID for future edits. | `(externalID, error)` |
| `Edit` | Update an existing message in-place. The platform ID is in `cmd.EditOfExternalId`. | `error` |
| `ChannelType` | Registry slug (e.g., `"slack"`). Must match `proto/channels.yaml` exactly (lowercase, underscores). | `string` |
| `MaxDeliver` | Override the NATS consumer max-delivery limit. Cliq uses `5` (default); flaky channels use higher values. | `int` |
| `RateLimitKey` | Return a rate-limit bucket key for fairness. Return `""` for per-account fairness; Slack adapters return `"<account_id>:<conversation_id>"` for per-channel fairness. No wire format changes. | `string` |
| `Capabilities` | Return the static ChannelCapabilities for this platform (message limits, supported features, auth scheme). Must be a stable pointer; callers treat it as read-only. Regression test pattern catches drift. | `*miov1.ChannelCapabilities` |
| `Inbound` | Return the webhook handler (signature verify, normalize, handshake). Return `nil` for outbound-only adapters. | `InboundAdapter` |
| `Credentials` | Return the OAuth/token lifecycle handler. Always non-nil; OAuth-free adapters return a handler whose `AuthorizeURL` is `""` and `ExchangeCode` returns an error. | `CredentialAdapter` |

### Inbound Path

The `InboundAdapter` interface (`pkg/channels/inbound_adapter.go`) handles webhook verification and normalization:

```go
type InboundAdapter interface {
    VerifySignature(headers http.Header, rawBody []byte) error
    Normalize(rawBody []byte) (*miov1.Message, error)
    HandleHandshake(w http.ResponseWriter, r *http.Request) bool
}
```

| Method | Purpose | Returns |
|--------|---------|---------|
| `VerifySignature` | Validate the webhook signature (HMAC, RSA, etc.). Return `nil` on success; callers respond `401` on error and emit `bad_signature` metrics. Dev mode (empty secret) may return `nil` unconditionally (emit a startup warning, not per-request). | `error` |
| `Normalize` | Parse the webhook body and produce a canonical `mio.v1.Message`. Return `nil, nil` only on success. Soft failures (unknown operation, missing fields) must wrap the error with `channels.ErrNormalizeSoft` so the handler responds `200` (platform does not retry). Parse failures (malformed JSON) stay unwrapped and map to `400`. | `(*mio.v1.Message, error)` |
| `HandleHandshake` | Intercept platform-specific handshakes (Slack's `url_verification`, Telegram's `setWebhook` ping). Write to `w` and return `true` to short-circuit the handler. Return `false` for normal message webhooks. | `bool` |

### Error Handling in Delivery

Adapters signal delivery outcomes via the `DeliveryError` interface (`pkg/channels/delivery_error.go`):

```go
type DeliveryError interface {
    error
    IsRetryable() bool
    IsRateLimited() bool
    RetryAfterSeconds() int
}
```

The sender pool (`services/gateway/internal/sender/`) routes based on these signals:

| `IsRetryable()` | `IsRateLimited()` | `RetryAfterSeconds()` | Action |
|---|---|---|---|
| `true` | — | — | **Nak** (requeue for retry, increments delivery count) |
| `false` | `true` | `>0` | **Retry-After** (wait N seconds, then Nak) |
| `false` | `true` | `0` | **Nak** (requeue with backoff) |
| `false` | `false` | — | **Term** (final, record error, do not retry) |

**Soft vs Hard failures:** A Slack adapter receiving a 4xx response (e.g., `channel_not_found`) must return `IsRetryable()=false, IsRateLimited()=false` to signal permanent failure. A transient network error (`ECONNRESET`) returns `IsRetryable()=true` to trigger retry.

### Credential Lifecycle

The `CredentialAdapter` interface (`pkg/channels/credential_adapter.go`) handles OAuth or token refresh:

```go
type CredentialAdapter interface {
    AuthorizeURL(state string) string
    ExchangeCode(ctx context.Context, code string) (Credential, error)
    RefreshCredential(ctx context.Context, cur Credential) (Credential, error)
}
```

| Method | Purpose | Returns |
|--------|---------|---------|
| `AuthorizeURL` | Build the consent URL for the OAuth dance (e.g., Slack's `/oauth/v2/authorize?...`). The `state` parameter is caller-supplied, crypto-random, and must be included in the URL for CSRF protection. Return `""` for OAuth-free adapters. | `string` |
| `ExchangeCode` | Redeem an authorization code for a `Credential`. Called after the operator hits the `/oauth/callback` redirect. The returned Credential is opaque to the gateway; it is encrypted and persisted in the database. | `(Credential, error)` |
| `RefreshCredential` | Refresh an expiring token. For oauth2_refresh adapters, consume the `RefreshToken` and return a new Credential. For bot_token / hmac_webhook adapters, return the input unchanged. | `(Credential, error)` |

The `Credential` struct carries:

```go
type Credential struct {
    AccessToken  string
    RefreshToken string
    ExpiresAt    time.Time
    Extras       map[string]string  // escape hatch for adapter-private fields
}
```

Example: A Slack adapter stores `AccessToken`, `RefreshToken` (if refresh enabled), and `Extras["team_id"]` for multi-workspace routing.

---

## 2. Optional Interfaces

Some adapters need extra capabilities. Implement these only if required:

### `SecretConfigurable`

Let the gateway inject the file-mounted webhook secret at route-mount time:

```go
type SecretConfigurable interface {
    WithSecret(secret []byte) InboundAdapter
}
```

Example: A Slack adapter's `InboundAdapter` starts with a `nil` secret. At startup, the gateway calls `WithSecret(secret)` once per Slack account, returning a configured instance.

**File location:** `/etc/mio/secrets/<secret-name>`  
**Secret name convention:** Adapters without `SecretNamer` use `DefaultWebhookSecretName(channelType)`, which converts `slack` → `slack-webhook-secret`. Cliq's legacy mount (`cliq-webhook-secret`) is preserved via `WebhookSecretNames()`.

### `SecretNamer`

Declare custom secret file names (useful for renaming channels or honoring legacy mounts):

```go
type SecretNamer interface {
    WebhookSecretNames() []string
}
```

Example: Cliq returns `["cliq-webhook-secret", "zoho-cliq-webhook-secret"]` — first match wins.

### `RouteAliaser`

Declare extra webhook routes beyond the generic `/webhooks/<slug>`:

```go
type RouteAliaser interface {
    RouteAliases() []string
}
```

Example: Cliq's locked ingress route `/cliq` predates the generic router. Implement `RouteAliases()` to return `["/cliq"]` so both paths are mounted.

### `WorkspaceKeyer`

Expose the platform-side workspace identity for multi-account routing:

```go
type WorkspaceKeyer interface {
    WorkspaceKey(msg *miov1.Message) string
}
```

Return the platform's organization/workspace/team ID (stored in `msg.Attributes` during `Normalize`). The gateway uses this to route one webhook endpoint to the correct account when multiple Slack workspaces are installed.

Example: Cliq returns `msg.Attributes["cliq_org_id"]`.

---

## 3. Optional: History Reconciliation

For platforms where webhooks are not a complete source of truth (e.g., history may arrive out-of-order), implement `HistoryAdapter`:

```go
type HistoryAdapter interface {
    FetchHistory(ctx context.Context, req HistoryRequest) (HistoryPage, error)
}
```

This is **not** called from the hot-path webhook handler. It belongs to a separate background reconciler (planned for P13+). If you implement it now, you must also:

1. Set `HistorySupported: true` in `Capabilities()`
2. Populate `HistoryMessage` structs with `SourceMessageID`, `SenderExternalID`, `Text`, `SentAt`, `ParentExternalID` (for threading), and `Attachments`
3. Return `HistoryPage.NextCursor` for pagination

See `pkg/channels/history_adapter.go` for full signatures.

---

## 4. Building a Slack Adapter (Worked Example)

### Directory Structure

```
channels/slack/
├── adapter.go              # Adapter + InboundAdapter + CredentialAdapter impl
├── inbound.go              # VerifySignature, Normalize, HandleHandshake
├── oauth.go                # AuthorizeURL, ExchangeCode, RefreshCredential
├── signature.go            # HMAC-SHA256 verification helpers
├── normalize.go            # Webhook payload parsing and Message normalization
├── capabilities.go         # Hard-coded ChannelCapabilities
├── delivery_error.go       # DeliveryError impl for Slack HTTP errors
├── sender.go               # Send impl (API calls)
├── sender_edit.go          # Edit impl (message_update)
├── capabilities_test.go    # Regression test: Capabilities vs hard-coded expectation
├── fixtures/               # Webhook payloads for testing
│   ├── message_posted.json
│   ├── app_mention.json
│   ├── url_verification.json
│   └── signature_invalid.json
└── tests/                  # Unit tests (signature, normalize, send)
    ├── normalize_test.go
    ├── sender_test.go
    └── signature_test.go
```

### Step 1: Define Adapter + Registration

**`adapter.go`:**

```go
package slack

import (
    "context"
    "github.com/crashchat-ai/mio/pkg/channels"
    miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

const channelType = "slack"

type Adapter struct {
    // Slack API credentials; set once at init time from env vars
    clientID     string
    clientSecret string
    redirectURI  string
    signingKey   string // for webhook signature
}

func init() {
    // Register at startup so the gateway can discover this adapter
    channels.RegisterAdapter(&Adapter{
        clientID:     os.Getenv("SLACK_CLIENT_ID"),
        clientSecret: os.Getenv("SLACK_CLIENT_SECRET"),
        redirectURI:  os.Getenv("SLACK_REDIRECT_URI"),
        signingKey:   os.Getenv("SLACK_SIGNING_KEY"),
    })
}

func (a *Adapter) ChannelType() string { return channelType }

func (a *Adapter) MaxDeliver() int { return 5 } // Slack is reliable; default is fine

func (a *Adapter) RateLimitKey(cmd *miov1.SendCommand) string {
    // Per-channel fairness: different channels don't compete for quota
    return cmd.GetAccountId() + ":" + cmd.GetConversationExternalId()
}

func (a *Adapter) Capabilities() *miov1.ChannelCapabilities {
    return proto.Clone(slackCapabilities).(*miov1.ChannelCapabilities)
}

func (a *Adapter) Inbound() channels.InboundAdapter {
    return &slackInbound{signingKey: a.signingKey}
}

func (a *Adapter) Credentials() channels.CredentialAdapter {
    return &slackCredentials{adapter: a}
}

func (a *Adapter) Send(ctx context.Context, cmd *miov1.SendCommand) (string, error) {
    // Call Slack API: /api/chat.postMessage
    // Return platform message ID for outbound_state tracking
    // Implement DeliveryError for retry/rate-limit routing
}

func (a *Adapter) Edit(ctx context.Context, cmd *miov1.SendCommand) error {
    // Call Slack API: /api/chat.update with cmd.EditOfExternalId
    // Return DeliveryError
}
```

**Env var convention:**
- `SLACK_CLIENT_ID` — OAuth client ID
- `SLACK_CLIENT_SECRET` — OAuth client secret  
- `SLACK_REDIRECT_URI` — OAuth callback URL (e.g., `https://mio.example.com/oauth/callback`)
- `SLACK_SIGNING_KEY` — webhook signing secret

The gateway knows to load these based on the adapter's `ChannelType()`.

### Step 2: Webhook Signature Verification

**`signature.go`:**

```go
func (s *slackInbound) VerifySignature(headers http.Header, rawBody []byte) error {
    if s.signingKey == "" {
        // Dev mode (dev-only feature; emit startup warning, not per-request)
        return nil
    }
    
    // Slack uses: Slack-Request-Timestamp + Slack-Request-Signature (v0=...)
    timestamp := headers.Get("X-Slack-Request-Timestamp")
    signature := headers.Get("X-Slack-Request-Signature")
    
    // Verify: HMAC-SHA256(f"v0:{timestamp}:{body}", secret) == signature
    // Return ErrBadSignature on mismatch
}
```

### Step 3: Normalize to mio.v1.Message

**`normalize.go`:**

```go
func (s *slackInbound) Normalize(rawBody []byte) (*miov1.Message, error) {
    var payload SlackEvent
    if err := json.Unmarshal(rawBody, &payload); err != nil {
        return nil, fmt.Errorf("slack: parse: %w", err)
    }
    
    // Handle soft failures (unknown event type, missing fields)
    if payload.Type == "unknown_event" {
        return nil, fmt.Errorf("%w: slack: unknown event type", channels.ErrNormalizeSoft)
    }
    
    msg := &miov1.Message{
        SchemaVersion:          1,
        ChannelType:            "slack",
        ConversationExternalId: payload.Channel,
        ConversationKind:       "channel", // or "dm"
        SourceMessageId:        payload.Ts, // Slack message timestamp
        SenderExternalId:       payload.User,
        SenderDisplayName:      payload.Username,
        Text:                   payload.Text,
        ReceivedAt:             timestamppb.Now(),
        Attributes: map[string]string{
            "slack_team_id": payload.TeamID,
            "slack_channel":  payload.ChannelName,
        },
    }
    
    // Handle threading (Slack: thread_ts)
    if payload.ThreadTs != "" {
        msg.ThreadRootMessageId = payload.ThreadTs
    }
    
    // Handle reactions, attachments, etc.
    // See channels/zohocliq/normalize.go for the full pattern
    
    return msg, nil
}
```

**Soft error pattern:** Any condition that should not cause the platform to retry (unknown operation, missing fields) is wrapped with `channels.ErrNormalizeSoft`. The HTTP handler responds `200` for these, preventing the platform from hammering the webhook.

### Step 4: Capability Declaration & Regression Test

**`capabilities.go`:**

```go
var slackCapabilities = &miov1.ChannelCapabilities{
    SupportsEdit:      true,      // Slack supports message updates
    SupportsDelete:    true,       // Slack supports message deletion
    SupportsReactions: true,       // Slack supports emoji reactions
    SupportsThreads:   true,       // Slack supports threaded replies
    SupportsTyping:    false,      // Not yet implemented
    SupportsPresence:  false,      // Not yet implemented
    AllowedAttachments: []miov1.Attachment_Kind{
        miov1.Attachment_KIND_IMAGE,
        miov1.Attachment_KIND_FILE,
        miov1.Attachment_KIND_AUDIO,
        miov1.Attachment_KIND_VIDEO,
        miov1.Attachment_KIND_LINK,
    },
    MaxTextBytes:        4000,  // Slack message character limit
    RateLimitPerSecond:  1,     // Slack default rate limit
    RateLimitScope:      "account",
    AuthKind:            "oauth2_refresh",
    EditWindowSeconds:   0,     // Slack allows editing any time (admins can edit old)
    DeleteWindowSeconds: 0,
}

func (a *Adapter) Capabilities() *miov1.ChannelCapabilities {
    return proto.Clone(slackCapabilities).(*miov1.ChannelCapabilities)
}
```

**`capabilities_test.go` (regression test):**

```go
func TestCapabilities_Verbatim(t *testing.T) {
    a := &Adapter{}
    got := a.Capabilities()
    
    want := &miov1.ChannelCapabilities{
        SupportsEdit:      true,
        SupportsDelete:    true,
        SupportsReactions: true,
        // ... all fields ...
    }
    
    if !proto.Equal(got, want) {
        t.Fatalf("ChannelCapabilities drift detected.\n got:  %+v\n want: %+v", got, want)
    }
}
```

This test forces you to explicitly update the expectation when you add a new feature. It catches silent drift in CI.

### Step 5: Delivery Error Routing

**`delivery_error.go`:**

```go
type SlackHTTPError struct {
    statusCode  int
    retryAfter  int // seconds, if present
    slackError  string
}

func (e *SlackHTTPError) IsRetryable() bool {
    // 5xx errors and network timeouts are retryable
    return e.statusCode >= 500 || e.statusCode == 0
}

func (e *SlackHTTPError) IsRateLimited() bool {
    return e.statusCode == 429
}

func (e *SlackHTTPError) RetryAfterSeconds() int {
    return e.retryAfter
}

func (e *SlackHTTPError) Error() string {
    return fmt.Sprintf("slack: HTTP %d: %s", e.statusCode, e.slackError)
}
```

The sender pool routes based on these signals:
- `IsRetryable()=true` → Nak (requeue for retry, increments delivery count)
- `IsRateLimited()=true` → Retry-After or Nak with backoff
- `IsRetryable()=false, IsRateLimited()=false` → Term (final, no retry)

### Step 6: OAuth Credential Exchange

**`oauth.go`:**

```go
type slackCredentials struct {
    adapter *Adapter
}

func (sc *slackCredentials) AuthorizeURL(state string) string {
    // Build Slack's OAuth consent URL
    return fmt.Sprintf(
        "https://slack.com/oauth/v2/authorize?client_id=%s&scope=..&redirect_uri=%s&state=%s",
        sc.adapter.clientID,
        url.QueryEscape(sc.adapter.redirectURI),
        state,
    )
}

func (sc *slackCredentials) ExchangeCode(ctx context.Context, code string) (channels.Credential, error) {
    // POST https://slack.com/api/oauth.v2.access
    // with code, client_id, client_secret, redirect_uri
    // Return Credential{ AccessToken, RefreshToken, ExpiresAt, Extras }
}

func (sc *slackCredentials) RefreshCredential(ctx context.Context, cur channels.Credential) (channels.Credential, error) {
    if cur.RefreshToken == "" {
        return cur, nil // Non-refresh OAuth or bot token
    }
    // POST https://slack.com/api/oauth.v2.access with refresh_token
    // Return new Credential with updated AccessToken + ExpiresAt
}
```

---

## 5. Testing Checklist

### Normalize Fixtures

Create webhook payloads in `channels/slack/fixtures/`:

**`message_posted.json`:**
```json
{
  "token": "...",
  "type": "event_callback",
  "event": {
    "type": "message",
    "user": "U123456",
    "text": "Hello world",
    "ts": "1234567890.123456",
    "channel": "C123456",
    "event_ts": "1234567890.123456"
  },
  "team_id": "T123456"
}
```

**Test that `Normalize` produces the correct `mio.v1.Message`:**

```go
func TestNormalize_MessagePosted(t *testing.T) {
    raw, _ := os.ReadFile("fixtures/message_posted.json")
    inbound := &slackInbound{}
    msg, err := inbound.Normalize(raw)
    
    assert.NoError(t, err)
    assert.Equal(t, "slack", msg.ChannelType)
    assert.Equal(t, "U123456", msg.SenderExternalId)
    assert.Equal(t, "Hello world", msg.Text)
}
```

### Signature Verification

Test both valid and invalid signatures:

```go
func TestVerifySignature_Valid(t *testing.T) {
    inbound := &slackInbound{signingKey: "secret"}
    headers := http.Header{
        "X-Slack-Request-Timestamp": {"1234567890"},
        "X-Slack-Request-Signature": {"v0=..."},
    }
    err := inbound.VerifySignature(headers, []byte("body"))
    assert.NoError(t, err)
}

func TestVerifySignature_Invalid(t *testing.T) {
    inbound := &slackInbound{signingKey: "secret"}
    headers := http.Header{
        "X-Slack-Request-Timestamp": {"1234567890"},
        "X-Slack-Request-Signature": {"v0=invalid"},
    }
    err := inbound.VerifySignature(headers, []byte("body"))
    assert.Error(t, err)
}
```

### Capability Regression Test

The test in Step 4 catches any silent drift in `Capabilities()` between code and expectations.

### Delivery Error Routing

Test that `Send` errors are correctly routed:

```go
func TestSend_RateLimitedError(t *testing.T) {
    err := &SlackHTTPError{statusCode: 429, retryAfter: 30}
    assert.True(t, err.IsRateLimited())
    assert.Equal(t, 30, err.RetryAfterSeconds())
}

func TestSend_TransientError(t *testing.T) {
    err := &SlackHTTPError{statusCode: 500}
    assert.True(t, err.IsRetryable())
}

func TestSend_PermanentError(t *testing.T) {
    err := &SlackHTTPError{statusCode: 403, slackError: "channel_not_found"}
    assert.False(t, err.IsRetryable())
}
```

---

## 6. Integration: Registration & Wiring

### Step 1: Register in `channels/all/all.go`

Add a blank import so the adapter's `init()` block runs:

```go
package all

import (
    _ "github.com/crashchat-ai/mio/channels/slack"
    _ "github.com/crashchat-ai/mio/channels/zohocliq"
)
```

### Step 2: Add to `proto/channels.yaml`

The channel-type codegen tool reads this to populate SDK-level constants:

```yaml
channels:
  - slug: slack
    description: "Slack"
    api_url_base: "https://slack.com/api"
  - slug: zoho_cliq
    description: "Zoho Cliq"
    api_url_base: "https://www.zoho.com/cliq"
```

Run `make proto-gen` to regenerate `sdks/go/channeltypes.go` and `sdks/python/mio/channeltypes.py`.

### Step 3: Secrets Mount

At deployment time, mount the webhook secret:

```bash
kubectl create secret generic mio-gateway-secrets \
  --from-file=slack-webhook-secret=/path/to/secret
```

Or locally in `deploy/local/secrets/`:

```bash
echo "shared_secret_here" > deploy/local/secrets/slack-webhook-secret
```

The gateway's HTTP handler calls `adapter.Inbound().WithSecret(secret)` at startup, configuring all webhook routes with their respective secrets.

---

## 7. The Litmus Test: Zero Core Edits

**Your new adapter is production-ready when:**

1. **All code lives in `channels/slack/`** — no new files in `services/gateway/internal/`
2. **Only two files outside `channels/` are touched:**
   - `channels/all/all.go` — one blank import
   - `proto/channels.yaml` — one YAML entry
3. **No `server.go` switch statement.** All inbound routing is registry-driven.
4. **No `config.go` hardcode.** All secrets are loaded dynamically via `RegisteredAdapters()`.

To verify, run:

```bash
make gateway-dispatch-lint
```

This CI guard fails if any channel names appear in `dispatch.go`, `server.go`, or `config.go`.

**What Zoho Cliq touches** (as proof the contract works):

```
channels/zohocliq/
├── adapter.go
├── inbound.go
├── oauth.go
├── signature.go
├── normalize.go
├── capabilities.go
├── delivery_error.go
├── sender.go
├── sender_edit.go
├── capabilities_test.go
├── history.go  # optional
├── history_test.go
└── tests/

channels/all/all.go  # blank import

proto/channels.yaml  # entry for zoho_cliq
```

**No edits to:**
- `services/gateway/internal/server/server.go`
- `services/gateway/internal/config/config.go`
- `services/gateway/internal/runtime/gateway.go`

---

## 8. Rate Limiting & Precedence

Adapters declare their rate limit in `Capabilities().RateLimitPerSecond`. The limiter respects this hierarchy:

1. **Account-level override** (from P11 admin mutation) — highest priority
2. **Adapter's capability advertised limit** — default
3. **Global default** (5 tokens/second) — fallback

Example: If Slack adapter returns `RateLimitPerSecond: 1` and the account override is `2`, the limiter uses `2`.

The gateway's rate-limit engine (`internal/ratelimit/account.go`) consumes these dynamically. No recompile needed to adjust per-adapter limits.

---

## 9. Attribute Escape Hatch

Sometimes adapters need to attach platform-specific metadata to messages. Use the `Attributes` map on `mio.v1.Message`:

```go
msg.Attributes = map[string]string{
    "slack_team_id":     payload.TeamID,
    "slack_channel":     payload.ChannelName,
    "slack_ts":          payload.Ts,
    "conversation_display_name": conversationName,
}
```

The gateway preserves these in the `MESSAGES_INBOUND` stream. Consumers can access them; new attributes are added to the wire schema as-needed (additive, breaking-free).

For widely-used attributes (e.g., `conversation_display_name`), use the well-known constant:

```go
msg.Attributes[channels.AttrConversationDisplayName] = displayName
```

---

## 10. Optional: Message History Reconciliation

If your platform's webhooks can arrive out-of-order or incompletely, implement `HistoryAdapter` to backfill:

```go
func (a *Adapter) FetchHistory(ctx context.Context, req channels.HistoryRequest) (channels.HistoryPage, error) {
    // Fetch messages from Slack's API (e.g., /api/conversations.history)
    // Return a HistoryPage with normalized HistoryMessage structs
    // Include NextCursor for pagination
}
```

Set `HistorySupported: true` in `Capabilities()`. A background reconciler (future phase) will call this to fill gaps.

---

## 11. Summary

You now have the complete contract and patterns. To add a new adapter:

1. Create `channels/<slug>/` with an `Adapter` implementation
2. Implement `InboundAdapter` (verify signature, normalize, handshake)
3. Implement `CredentialAdapter` (OAuth dance + refresh)
4. Implement `DeliveryError` for `Send` outcomes (retryable, rate-limited, final)
5. Hard-code `Capabilities()` + regression test
6. Add one blank import to `channels/all/all.go`
7. Add one entry to `proto/channels.yaml`
8. Run `make proto-gen`
9. Run `make gateway-dispatch-lint` to verify zero core edits

That's it. No hand-waving. Follow this guide and the codebase, and your adapter is community-ready.

For questions, refer to `pkg/channels/*.go` for the authoritative contract and `channels/zohocliq/` for a complete, tested reference implementation.
