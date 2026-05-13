# MIO — Code Standards & Governance

**Last updated:** 2026-05-13  
**Scope:** Go, Python, Protobuf, shell, Helm | **Enforced via:** CI gates, code review

---

## Language & Tool Versions

Pinned in `.mise.toml`:

| Tool | Version | Purpose |
|---|---|---|
| Go | 1.25 | Gateway, SDK, sink-gcs, media-vault, tui, tools |
| Python | 3.12 | SDK, examples, tools |
| Protoc | 27 | Proto file compilation |
| buf | latest | Proto linting, codegen, breaking changes |
| golangci-lint | 2.6.0 | Go code quality (gateway + sdk-go) |
| ruff | latest | Python linting + formatting |
| uv | latest | Python package manager (sdk-py, echo-consumer) |

**Development setup:**
```bash
mise install   # Installs all pinned versions
mise tasks     # See available task aliases
```

---

## File Naming & Organization

### Go Files

- **Convention:** Snake case (e.g., `outbound.go`, `rate_limit.go`, `idempotency_test.go`)
- **Structure per package:**
  ```
  package/
  ├── {primary_concern}.go      # Main logic
  ├── {secondary_concern}.go    # Supporting logic
  ├── {concern}_test.go         # Table-driven tests
  └── internal_helper.go        # Private helpers (unexported)
  ```
- **File limit:** Keep under 200 LOC per file (split large logic into focused modules)
- **Examples:** gateway/internal/sender/dispatch.go, gateway/internal/store/message.go

### Python Files

- **Convention:** Kebab-case for modules, snake_case for functions/classes (PEP 8)
- **Examples:** `sdk-py/mio/client.py`, `examples/echo-consumer/src/echo.py`

### Proto Files

- **Location:** `proto/mio/v1/` (v1 locked for POC)
- **Naming:** Singular, descriptive (e.g., `message.proto`, `send_command.proto`, NOT messages.proto)
- **No enum bloat:** Use constants in code until threshold met (see attributes promotion rule)

### Config & Data Files

- **Helm values:** `deploy/charts/{component}/values.yaml` (YAML)
- **Migrations:** `gateway/internal/store/migrations/{version}__{slug}.sql` (golang-migrate format)
- **Env example:** `.env.example` (KV pairs, no shell syntax)
- **Proto registry:** `proto/channels.yaml` (YAML, single source of truth)

---

## Linting & Formatting

### Go

**Tool:** golangci-lint 2.6.0 (configured in `golangci-lint.yml` if present, else defaults)

**Command:**
```bash
cd gateway && golangci-lint run ./...
cd sdk-go && golangci-lint run ./...
```

**Enforced rules:**
- No unused variables/imports (import analysis)
- No shadowed variables (govet)
- No nil pointer dereferences (staticcheck)
- Error handling: all non-ignored errors must be handled
- Format via gofmt (standard)

**CI gate:** `test-gateway` job fails PR on any golangci-lint errors.

### Python

**Tools:** ruff (lint + format)

**Commands:**
```bash
ruff check sdk-py examples/echo-consumer          # Lint
ruff format --check sdk-py examples/echo-consumer  # Format check
ruff format sdk-py examples/echo-consumer          # Auto-format
```

**Enforced rules:**
- PEP 8 style
- Unused imports removed
- Line length 88 (ruff default)
- Async/await correctness (sdk-py constraint)

**CI gate:** `test-python` job fails PR on ruff errors.

### Proto

**Tool:** buf lint + buf breaking

**Commands:**
```bash
buf lint proto                           # STANDARD ruleset
buf breaking --against ".git#branch=main"  # WIRE_JSON ruleset
```

**Exceptions (documented in `buf.yaml`):**
- `RESERVED_MESSAGE_NO_DELETE` ignored for `message.proto` field 17 + `send_command.proto` field 15 (safe slot promotions from reserved)

**CI gate:** `test-proto` job fails PR on lint/breaking violations.

---

## Repository Conventions

### Import Paths

- **Go:** Module prefixed (e.g., `github.com/crashchat-ai/mio/gateway/internal/sender`)
- **Python:** Relative imports within package (e.g., `from .client import Client`)

### Error Handling

**Go:**

```go
// Good: wrapped with context
return fmt.Errorf("failed to publish message: %w", err)

// Good: custom error type with predicates
type DeliveryError struct {
    Code  string
    Inner error
}
func (e *DeliveryError) IsRetryable() bool { return e.Code == "5xx" }
func (e *DeliveryError) IsRateLimited() bool { return e.Code == "429" }

// Bad: unhandled
_ = someFunc()

// Bad: string-wrapped (loses context)
return errors.New(fmt.Sprintf("error: %v", err))
```

**Python:**

```python
# Good: custom exception class
class DeliveryError(Exception):
    def __init__(self, code: str, inner: Exception):
        self.code = code
        self.inner = inner
    @property
    def is_retryable(self) -> bool:
        return self.code in ("5xx", "timeout")

# Good: re-raise with context
try:
    await nats_client.publish(...)
except Exception as e:
    raise DeliveryError("publish_failed", e) from e
```

### Configuration

**Environment variables (no YAML):**
- All runtime config from env vars (12-factor)
- Prefix: `MIO_` for MIO-specific vars (e.g., `MIO_ENV`, `MIO_TENANT_ID`)
- Port overrides: `.env.local` (sourced, not committed)

**Secrets (file mounts, not env):**
- Webhook signature key: `/etc/mio/secrets/webhook-secret` (mounted by K8s Secret)
- Age identity for credential encryption: `$HOME/.age/id` or `MIO_AGE_IDENTITY_FILE`
- OAuth tokens: encrypted at rest in Postgres, never in logs

---

## Adapter Pattern (Enforced)

### Interface

```go
// gateway/internal/sender/adapter.go
type Adapter interface {
    // Send delivers a command to the channel API.
    // Return error must implement IsRetryable(), IsRateLimited() predicates.
    Send(ctx context.Context, cmd *mio.SendCommand) error

    // Capabilities advertises what this adapter supports.
    Capabilities() *mio.ChannelCapabilities
}
```

### Rules

1. **No adapter-specific branches in dispatcher.** All send logic lives in the adapter's `Send()` method.
   - **CI guard:** `make gateway-dispatch-lint` fails PR if `dispatch.go` contains channel names (zoho, slack, cliq, telegram, discord)

2. **Self-registration.** Adapter registers itself at `init()` time:
   ```go
   // gateway/internal/channels/zohocliq/inbound.go
   func init() {
       channels.Register("zoho_cliq", &adapter{})
   }
   ```

3. **Zero globals per adapter.** All state (HTTP client, OAuth token manager) is owned by the Adapter instance.

4. **Rate limiter key is configurable.** Default `account_id`, but adapter can override (e.g., Slack: `account_id:conversation_external_id` for per-channel fairness).

### Adding a New Adapter

1. Create `gateway/internal/channels/{slug}/` directory
2. Implement `Adapter` interface (inbound webhook handler + outbound Send)
3. Register in `init()`
4. Add entry to `proto/channels.yaml` with status (planned/active)
5. PR includes: code + tests + CONTRIBUTING.md audit (attributes + channel_type registry)

---

## Subject Grammar & Registry

**Grammar:**
```
mio.<direction>.<channel_type>.<account_id>.<conversation_id>[.<message_id>]
```

**Dimensions:**
| Part | Type | Example | Rationale |
|---|---|---|---|
| direction | enum | inbound, inbound_enriched, outbound | Stream-per-direction for clean subject prefixes |
| channel_type | string (registry) | zoho_cliq, slack | From proto/channels.yaml (underscore in wire format) |
| account_id | UUID | {uuid} | Per-account rate limits, idempotency scoping |
| conversation_id | UUID | {uuid} | Future: per-conversation shard when throughput demands |
| message_id | optional UUID | {uuid} | Only on outbound (for edits/deletes) |

**Registry enforcement:**
- **Source of truth:** `proto/channels.yaml` (name field = wire value)
- **Adding a channel:** Entry in `channels.yaml` first (status: planned/active), then implement adapter
- **Renaming:** Add old name to `deprecated_aliases` (never rename in-place — breaks GCS partitions + BigQuery filters)
- **CI gate:** PR fails if code introduces unknown `channel_type` strings

---

## Metrics & Observability

### Label Discipline (Cardinality Bounds)

**Allowed labels (all applications):**
- `channel_type` — bounded by registry (~10 values)
- `direction` — inbound, inbound_enriched, outbound (3 values)
- `outcome` — success, retryable_error, permanent_error, rate_limited (4 values)

**Forbidden (cardinality bombs):**
- `account_id`, `tenant_id`, `conversation_id`, `message_id` — unique per entity
- `user_id`, `request_id` — unique per request

**Phase-specific bounded extras** (documented in code):
- `http_status` — only bucketed (2xx, 4xx, 429, 5xx, network)
- `reason` — bounded enum (e.g., InvalidSignature, NotFound, RateLimited, InternalError)

### Key Metrics

| Metric | Owner | Labels | SLO |
|---|---|---|---|
| `mio_gateway_inbound_latency_seconds{channel_type,direction,outcome}` | gateway | {channel_type, outcome} | p99 < 500ms |
| `mio_gateway_outbound_send_total{channel_type,direction,outcome}` | gateway | {channel_type, outcome} | N/A |
| `mio_jetstream_consumer_lag{stream,consumer}` | NATS exporter | {stream, consumer} | Monitor AI consumer lag |
| `mio_sink_gcs_bytes_written_total{channel_type}` | sink-gcs | {channel_type} | Throughput tracking |
| `mio_idempotency_dedup_total{channel_type}` | gateway | {channel_type} | Redelivery rate sanity |

---

## Idempotency

**Two-layer defense:**

1. **NATS dedup (short window):**
   - Header: `Nats-Msg-Id` (deterministic value, e.g., source_message_id)
   - Window: 2 minutes
   - Catches: gateway retries

2. **Postgres unique constraint (authoritative):**
   - Constraint: UNIQUE(account_id, source_message_id)
   - Catches: channel-level redeliveries past NATS window

**Gateway flow:**
```
1. HMAC verify signature
2. INSERT OR IGNORE (account_id, source_message_id) → returns 1 or 0 rows
3. If 1 row: publish to NATS (include Nats-Msg-Id header)
4. If 0 rows: silent 200 OK (dedup)
5. Respond within channel deadline (≤5s)
```

---

## Proto Field Numbering Policy

**Rules:**
- Fields 1–15: single-byte tags (hot path, use for frequently-set fields)
- Fields 16+: multi-byte tags (use sparingly)
- **Never reuse a field number.** If removing: add `reserved N;` to the message

**Current reservations:**
- `Message` field 17: reserved for `MessageRelation` (P5 future, edit/reaction linkage)
- `Message` field 18: reserved for `is_summary` (message compaction flag, future)
- `SendCommand` field 15: reserved for `MessageRelation`

**When adding a field:**
- Check `reserved` list first
- Pick the next available number
- Document in comments if reserved slot

---

## Attributes Promotion Rule (Enforced)

The `attributes map<string,string>` on Message and SendCommand is an escape hatch for channel-specific data.

**Promotion threshold:**
- Any `attributes` key read by ≥2 consumers **OR** written by ≥2 channel adapters must be promoted to a named, typed proto field

**Until promotion:**
- Define named constants (never inline string literals)

**Go example:**
```go
// Good
const AttrZohoCliqWorkspace = "zoho_cliq_workspace"
msg.Attributes[AttrZohoCliqWorkspace] = workspaceID

// Bad
msg.Attributes["zoho_cliq_workspace"] = workspaceID  // Literal scattered across files
```

**Python example:**
```python
# Good
ATTR_ZOHO_CLIQ_WORKSPACE = "zoho_cliq_workspace"
msg.attributes[ATTR_ZOHO_CLIQ_WORKSPACE] = workspace_id
```

**Rationale:** Attributes stored verbatim in GCS archive + BigQuery external tables. Renames after 2+ consumers require dual-read migrations (same scar as goclaw migration 58).

---

## Testing Practices

### Go

**Style:** Table-driven tests

```go
func TestSendCommand(t *testing.T) {
    tests := []struct {
        name    string
        input   *mio.SendCommand
        want    *Result
        wantErr bool
    }{
        {
            name: "valid_command",
            input: &mio.SendCommand{...},
            want: &Result{...},
        },
        {
            name: "rate_limited",
            input: &mio.SendCommand{...},
            wantErr: true,
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // test body
        })
    }
}
```

**Coverage:** Aim for >80% on hot paths (inbound, outbound, idempotency). Unit tests require no live deps (no NATS/Postgres).

### Python

**Tool:** pytest with markers

```python
@pytest.mark.asyncio
async def test_consume_valid():
    # Test body

@pytest.mark.integration
async def test_with_live_nats():
    # Requires NATS_URL env var
```

**Invoke:**
```bash
pytest tests/ -m "not integration"  # Unit tests only
pytest tests/ -m integration        # Integration tests only
```

---

## Commit Message Style

**Format:** Conventional commits

```
<type>(<scope>): <subject>

<body>

<footer>
```

- **Type:** feat, fix, docs, refactor, test, chore, perf, ci
- **Scope:** Component (gateway, sdk-go, sdk-py, sink-gcs, media-vault, tui, proto, deploy, docs)
- **Subject:** Imperative, present tense, lowercase, no period (≤50 chars)
- **Body:** Optional, wrap at 72 chars, explain "why" not "what"
- **Footer:** Fixes #123, Closes #456 (GitHub issue references)

**Examples:**
```
feat(gateway): add per-account rate limiter for Slack adapter
fix(sdk-py): handle async close in consumer cleanup
docs(deployment): add secret rotation runbook
refactor(sender): extract rate_limit to separate module
test(gateway): add fairness benchmark for multi-tenant workload
```

**CI enforces:** Conventional format via commit-lint (if configured); at minimum, no AI references in messages.

---

## Security

### Secrets Management

**Never in logs or env:**
- Webhook signature keys
- OAuth tokens
- Database passwords
- Private keys

**Allowed patterns:**
- File mounts (K8s Secret → volume)
- Environment variable (non-secrets only)
- Encrypted at rest (age cipher for credentials in Postgres)

### Signature Verification

**Go example (Cliq):**
```go
// gateway/internal/channels/zohocliq/inbound.go
func verifySignature(body []byte, signature, secret string) error {
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write(body)
    expected := hex.EncodeToString(mac.Sum(nil))
    if !subtle.ConstantTimeCompare([]byte(signature), []byte(expected)) {
        return ErrBadSignature
    }
    return nil
}
```

### Admin Server Auth

**Loopback-only + CIDR allowlist** (default: 127.0.0.1/32)

```go
// gateway/internal/admin/auth.go
func isAllowed(ip string) bool {
    // Check ADMIN_ALLOWED_CIDR env var (comma-separated)
    // Default: 127.0.0.1/32
}
```

---

## Deployment & CI/CD

### CI Path Filters

Defined in `.github/workflows/ci.yaml` (dorny/paths-filter):

| Path | Triggers | Job |
|---|---|---|
| `proto/**`, `buf.yaml`, `buf.gen.yaml` | test-proto | buf lint + breaking |
| `gateway/**`, `sdk-go/**` | test-gateway | golangci-lint + go test |
| `sdk-py/**`, `examples/echo-consumer/**` | test-python | ruff + pytest |
| `deploy/charts/**` | helm-lint | helm lint all charts |
| `media-vault/**` | test-media-vault | go test |
| `sink-gcs/sql/**`, `proto/mio/v1/**` | test-bq-schema | schema drift check |

### Image Tagging

**Registry:** ghcr.io/crashchat-ai/mio

**Per-component images:**
- `ghcr.io/crashchat-ai/mio/gateway:{sha}`
- `ghcr.io/crashchat-ai/mio/sink-gcs:{sha}`
- `ghcr.io/crashchat-ai/mio/media-vault:{sha}`
- `ghcr.io/crashchat-ai/mio/echo-consumer:{sha}`

**Tag policy:**
- Always tag by commit SHA (immutable, traceable)
- Also tag `:main` on main branch (rolling, for dev)
- Never `:latest` on non-tag pushes (prevents surprise upgrades)

### Pre-commit Checks

**Recommended (not enforced locally, but enforced in CI):**
```bash
make lint          # buf lint + go vet
make test          # go test ./... (all modules)
make gateway-test  # gateway unit tests only
```

**Before push:**
```bash
git status         # Ensure no untracked secrets
make lint
make test
git push
```

---

## References

- [CONTRIBUTING.md](../CONTRIBUTING.md) — Attributes promotion, channel_type registry, proto field numbers
- [System Architecture](system-architecture.md) — Design invariants
- [Codebase Summary](codebase-summary.md) — Component layout
- Makefile — Build targets (lint, test, build-local, etc.)
