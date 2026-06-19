---
title: Codebase Summary
description: Repository layout and role-based monorepo structure of github.com/crashchat-ai/mio.
---

# MIO — Codebase Summary

**Last updated:** 2026-05-13 | **Module:** github.com/crashchat-ai/mio | **License:** Apache-2.0

---

## Repository Layout

**Role-based monorepo** (Option B: grouped by responsibility, not service).

```
mio/
├── channels/              # In-tree messaging adapters. Per-adapter subpackage.
│   ├── zohocliq/         # Zoho Cliq (active: webhook + OAuth + send)
│   └── all/all.go        # Barrel: blank-imports all adapters for compilation
├── pkg/                  # Shared libraries (minimal: no utils/common/helpers rule).
│   └── channels/         # Public adapter contract (adapter.go, inbound_adapter.go, credential_adapter.go, registry.go, store.go, delivery_error.go)
├── services/             # Long-running headless/data-plane binaries (Go).
│   ├── gateway/          # Main API service. Inbound webhook handler + outbound sender pool + admin control plane + embedded NATS option.
│   ├── sink-gcs/         # GCS archiver consumer. Cold storage + analytics substrate.
│   └── media-vault/      # Attachment sidecar. Fetches within platform TTL, persists to GCS.
├── ui/                   # Human-facing operator surfaces.
│   ├── tui/              # Terminal UI admin client (bubbletea). Read-only v1.
│   └── web/              # Operator web admin: API-only Go BFF (cmd/mio-web) + standalone React SPA (app/), single-origin reverse proxy.
├── ee/                   # Commercial overlay (build-tag-gated, //go:build ee). OSS must compile without it.
├── sdks/                 # Distributable client libraries.
│   ├── go/               # Go SDK (separate module: github.com/crashchat-ai/mio/sdk-go). Thin NATS wrapper for consumers.
│   └── python/           # Python SDK (async-only, uv-managed). Same surface as sdk-go, for AI integration.
├── examples/
│   └── echo-consumer/    # Example Python consumer proving the loop.
├── proto/                # Protobuf definitions (buf-managed). mio/v1/ (locked for POC), mio/admin/v1/, channels.yaml registry.
├── tools/                # Code generation: genchanneltypes, proto-roundtrip.
├── deploy/
│   ├── local/            # docker-compose stack (NATS 2.10, Postgres 16, MinIO, services).
│   ├── charts/           # Helm charts (7: mio-nats, mio-jetstream-bootstrap, mio-gateway, mio-web, mio-media-vault, mio-sink-gcs, mio-echo-consumer).
│   ├── gke/              # GKE POC setup.sh
│   ├── fluxcd/           # GitOps overlay (external infra repo reconciliation).
│   └── appdata/          # Persistent storage for local dev (NATS, Postgres).
├── hack/                 # Dev-only scratch. Not shipped, not tested. playground/ is gitignored.
├── docs/                 # Documentation (architecture, deployment, journals, roadmap, standards).
├── .workbench/                # Local planning artifacts: plans, reports, journals (P0–P11+).
├── Makefile              # 40+ build, test, lint, deploy targets.
├── go.work               # Go 1.25 workspace (root + sdks/go).
├── .env.example          # Environment variables template.
├── README.md             # Top-level overview.
└── CONTRIBUTING.md       # Governance rules (attributes promotion, channel_type registry).
```

## Workspace & Module Structure

**Go workspace** (`go.work`), single root module + one separate SDK module:
1. **Root module** (`github.com/crashchat-ai/mio`) — `services/`, `channels/`, `tools/`, `proto/gen/go`, `pkg/`, `ee/`
2. **SDK module** (`github.com/crashchat-ai/mio/sdk-go`) — `sdks/go/` (separate module, importable independently)
   - Root `go.mod` carries `replace github.com/crashchat-ai/mio/sdk-go => ./sdks/go` for workspace builds

**Python workspace** (`sdks/python/pyproject.toml`):
- `sdks/python/` — uv-managed project (async-only SDK)
- `examples/echo-consumer/` — Example consumer project (also uv-managed)

---

## Component Deep-Dive

### `services/gateway/` — Main API Service

**Binaries:**
- `cmd/gateway` — Production inbound/outbound server (HTTP + gRPC health). Connects to external NATS cluster + Postgres.
- `cmd/all-in-one` — Demo binary with embedded NATS JetStream (memory or file-backed). Single-binary for laptop demos.
- `cmd/admin` — Control-plane gRPC server (connect-go on loopback:9090 by default).

**Admin Control Plane** (`internal/admin/`):
- **RPCs** (connect-go, loopback-only by default, CIDR allowlist via auth.go):
  - `CreateTenant`, `ListTenants`, `GetTenant` — tenant lifecycle and lookup
  - `ListChannelTypes` — registered channel adapters and capabilities
  - `StartInstall`, `CompleteInstall` — operator-driven OAuth install dance
  - `ListAccounts` — account enumeration per tenant
  - `DisableAccount`, `RotateCredential` — existing write operations
  - `TailMessages` — streaming tail of inbound messages (debugging)
  - `install_stash` OAuth flow with `purgeExpired` ticker — clean up old credentials
- **Observability:** Prometheus instruments wired on all admin RPCs (request duration, error rates)
- **Auth:** CIDR allowlist (loopback-only by default). Set `ADMIN_LISTEN_ADDR` to override.

**Key internal packages:**
- `internal/channels/zohocliq/` — Zoho Cliq adapter (only concrete implementation)
  - `inbound.go` — Webhook handler, signature verify, normalize to Message
  - `oauth.go` — OAuth token fetch, refresh, storage
  - `sender.go` — SendCommand → Cliq API calls
  - `capabilities.go` — Cliq feature flags (reactions, threads, edits)
- `internal/sender/` — Outbound dispatcher
  - `dispatch.go` — Stateless: pull SendCommand → rate limit → call adapter → ack/nak
  - `pool.go` — Consumer pool per adapter
  - `rate_limit.go` — Token bucket per account_id (bucket size/refill per channel)
- `internal/store/` — Data layer (pgx)
  - `message.go` — Idempotency upsert (account_id, source_message_id)
  - `credentials.go` — OAuth token storage + refresh
  - `migrations/` — Flyway-style SQL (golang-migrate)
- `internal/runtime/` — Orchestration
  - `run.go` — Boot gateway, start NATS client, wire consumers, health checks
- `internal/config/` — Env var parsing
- `internal/crypto/` — Credential encryption (AgeFileCipher v2)
  - `cipher.go` — Interface: `Encrypt(plaintext) → ciphertext`, `Decrypt(ciphertext) → plaintext`
  - `age_file_cipher.go` — age envelope (age binary + private key file, rotatable)
  - `noop_cipher.go` — No-op (dev only, logs warning if used in prod)
- `internal/nats/` — NATS utilities
  - `embedded.go` — Embedded JetStream server (all-in-one binary, memory or file-backed)
  - `guardrail.go` — Guards against memory storage in production (panics if `MIO_ENV=prod` and `--storage memory`)
- `internal/server/` — HTTP server (chi router)
  - `/health` liveness/readiness
  - `/webhooks/{channel-slug}` inbound POST (signed)
- `internal/ratelimit/` — Per-account token bucket
- `internal/reconciler/` — Channel-agnostic history reconciliation runner
  - Calls `pkg/channels.HistoryAdapter`
  - Reuses `channels.Store` idempotency and publishes fresh rows to `MESSAGES_INBOUND`
  - Persists cursor/status in `source_reconcile_cursors`

**Key types:**
- `Adapter` interface — `Send(context, SendCommand) error` + `Capabilities()`
- `Dispatcher` — Pulls MESSAGES_OUTBOUND, routes to adapter pools
- `Cipher` interface — Encrypt/decrypt credentials at rest

**Conventions:**
- Snake case file names (`outbound.go`, `rate_limit.go`)
- Errors wrapped with context: `fmt.Errorf("...%w", err)` + custom error types (e.g., `DeliveryError` with `IsRetryable()`, `IsRateLimited()`)
- Config from env vars (no YAML; secrets via file mounts for webhook secret, age identity)

### `channels/` — In-Tree Messaging Adapters

**Role:** One subdirectory per adapter. Register via `init()`. Compile into gateway binaries via `channels/all/all.go`.

**Today:** Only `zohocliq/` (active). Slack/Telegram/Discord reserved for future phases.

**Each adapter exports:**
- Inbound handler: webhook → normalized Message
- Outbound sender: SendCommand → platform API call
- Capability flags: reactions, threads, edits, etc.
- OAuth flow: token fetch, refresh, storage (via pkg/channels contract)
- Optional history adapter: provider API history → normalized messages

**Pattern:** Adapters are NOT packages; they're top-level `services/gateway/internal/channels/{name}/` during POC. After P5 generalization, they graduate to `channels/{name}/` as public in-tree packages (no separate module—stays in root module).

### `pkg/channels/` — Adapter Contract

**Public interface** (exported from root module, versioned):

```go
type Adapter interface {
  Send(context, SendCommand) error
  Capabilities() proto.ChannelCapabilities
}

type InboundAdapter interface {
  ParseWebhook([]byte, signature) (Message, error)
  VerifySignature([]byte, signature) bool
}

type CredentialAdapter interface {
  StoreCredential(account_id, channel_type, value) error
  GetCredential(account_id, channel_type) (value, error)
  RefreshToken(account_id) error
}

type HistoryAdapter interface {
  FetchHistory(context.Context, HistoryRequest) (HistoryPage, error)
}

type DeliveryError interface {
  error
  IsRetryable() bool
  IsRateLimited() bool
}
```

Gateway + media-vault depend on `pkg/channels` only; they never import from `channels/{specific}/`. Registry pattern enables swappable implementations.

### `sdks/go/` — Go SDK

**Public API:**
```go
type Client struct { … }
type Options struct { … }
type Delivery struct { … }

func NewClient(opts Options) (*Client, error)
func (c *Client) Consume(ctx context.Context, opts ConsumerOptions) (<-chan *Delivery, error)
func (c *Client) Publish(ctx context.Context, msg *mio.Message, opts PublishOptions) error
func (c *Client) SubscribeAndConsume(ctx context.Context) — convenience wrapper
func (c *Client) Close() error
```

**Features:**
- Thin wrapper over `nats.go` v1.52
- Schema-version check on publish (verifies server has proto/mio/v1)
- MaxAckPending=1 default (single-flight ordering)
- OTel trace propagation
- Prometheus metrics (consume rate, publish latency)

**Key types:**
- `Delivery` — wraps nats.Msg, provides `Ack()`, `Nak()`

### `sdks/python/` — Python SDK

**Async-only surface** (by design, for LangGraph compatibility):
```python
client = await Client.from_options(options)
async for delivery in client.consume(...):
    msg = delivery.message  # mio_pb2.Message
    await delivery.ack()

await client.publish(cmd, **options)  # mio_pb2.SendCommand
await client.close()
```

**Features:**
- `nats-py` async client
- Same schema-version check + OTel + Prometheus as sdk-go
- MaxAckPending=1 default
- Deliberately async-only; no sync wrapper

### `services/sink-gcs/` — GCS Archiver Consumer

**Role:** Pull from MESSAGES_INBOUND stream, write raw NDJSON to GCS (cold storage + analytics).

**Flow:**
1. Pull NDJSON-per-message from JetStream
2. Buffer in memory (configurable flush triggers)
3. Flush to GCS on: 16 MB size **OR** 1 minute elapsed **OR** SIGTERM
4. Write path: `gs://bucket/mio/channel_type={slug}/date=YYYY-MM-DD/{batch-id}.ndjson`
5. Ack only after successful GCS write (at-least-once delivery guarantees dups in lake)

**Key code:**
- `internal/consumer/` — JetStream pull loop
- `internal/writer/` — NDJSON marshaling (UseProtoNames: true for snake_case keys)
- `internal/storage/gcs.go` — GCS bucket operations

**Schema contract:** `services/sink-gcs/sql/messages_schema.json` defines BQ columns. Proto changes must include DDL updates (CI guard: `check-proto-drift.sh` fails PRs with mismatches).

### `services/media-vault/` — Attachment Sidecar

**Role:** Pull from MESSAGES_INBOUND, fetch attachment bytes within platform TTL, persist to GCS, enrich, publish to MESSAGES_INBOUND_ENRICHED.

**Flow:**
1. Pull messages from MESSAGES_INBOUND (durable consumer, MaxAckPending=N)
2. For each message with attachments:
   - Fetch bytes from platform URL (Cliq, etc.) — race against TTL expiry
   - Deduplicate by content_sha256
   - Write to GCS (content-addressed: `channel_type=.../date=.../sha256[:2]/sha256{ext}`)
   - Enrich Attachment.storage_key + content_sha256
3. Publish enriched Message to MESSAGES_INBOUND_ENRICHED
4. Ack original message only after enriched publish succeeds (idempotent re-run safe)

**Key packages:**
- `internal/worker/` — Main loop
- `internal/storage/` — Abstract Storage interface (GCS, S3 ready)
- `internal/fetcher/zohocliq/` — Zoho-specific fetch with OAuth token refresh
- `internal/publisher/` — NATS publish wrapper
- `internal/dedup/` — Content-address deduplication (SHA256)
- `internal/gdpr/` — Delete-by-account operations
- `internal/lifecycle/` — Object expiry (7-day rule matching JetStream)
- `cmd/media-vault` — Main service binary
- `cmd/mio-media-cli` — CLI for operations (signed-url generation, GDPR deletes)

**CLI examples:**
```bash
mio-media-cli signed-url gs://bucket/mio/attachments/... --ttl=1h
mio-media-cli gdpr-delete --account-id=abc123
```

### `ui/tui/` — Terminal UI Admin Client

**Status:** Just-scaffolded bubbletea TUI.

**Current state:**
- Connect-go client to admin server (default `ADMIN_URL=http://127.0.0.1:9090`)
- Read-only v1 (inspect messages, list channels, view consumers)
- No write ops yet

**Key code:**
- `cmd/mio-tui` — Binary entry point
- `internal/client/` — connect-go stub wrappers

### `examples/echo-consumer/` — Example Consumer

**Purpose:** Minimal Python POC using sdk-py.

**Flow:**
1. Consume from MESSAGES_INBOUND_ENRICHED (read-only for POC)
2. Echo text → SendCommand
3. Publish back to MESSAGES_OUTBOUND

**Notable pattern:**
- Uses `python-ulid` for idempotency (source_message_id)
- Async/await (sdk-py constraint)
- Dry-run mode (no publish, just log)

### `proto/` — Protocol Definitions

**Files:**
- `mio/v1/message.proto` — Inbound envelope (tenant → account → conversation → message, attachments, relation)
- `mio/v1/send_command.proto` — Outbound envelope (mirror of Message scope, edit support)
- `mio/v1/rich_content.proto` — Channel-agnostic outbound cards, blocks, and buttons
- `mio/v1/attachment.proto` — File/image/link carrier (storage_key, content_sha256)
- `mio/v1/sender.proto` — Message author (platform-specific user ID, display name)
- `mio/v1/enums.proto` — ConversationKind, PeerKind
- `mio/v1/relation.proto` — MessageRelation (replies, edits, reactions, pins)
- `mio/v1/presence.proto` — Typing/online state (not on streams yet)
- `mio/v1/capabilities.proto` — ChannelCapabilities (reactions, threads, edits flags)
- `mio/admin/v1/admin.proto` — AdminService RPC (tenants, channel installs, accounts, credential rotation, TailMessages)

**Conventions:**
- Fields 1–15: single-byte tags (hot path)
- Reserved fields: Message.18 (is_summary)
- Never reuse field numbers; use `reserved N;` instead

**Codegen:**
- `buf.yaml` — lint STANDARD, breaking WIRE_JSON (exceptions: reserved slot promotions)
- `buf.gen.yaml` — outputs Go (source_relative) + Python + Connect-Go stubs to `proto/gen/`

**Registry:**
- `channels.yaml` — Single source of truth for channel_type strings
  - Active: `zoho_cliq`
  - Planned: `slack`, `telegram`, `discord`
  - Rule: Renames via `deprecated_aliases` only (never in-place)

### `tools/` — Code Generation

**`genchanneltypes`:**
- Input: `proto/channels.yaml`
- Output: `sdks/go/channeltypes.go` (const registry), `sdks/python/mio/channeltypes.py`
- Run: `go run ./tools/genchanneltypes/` (or `make proto-gen`)

**`proto-roundtrip`:**
- Test: Go ↔ Python protobuf wire format parity
- Run: `go run ./tools/proto-roundtrip/`
- Enforces that both SDKs marshal/unmarshal identically

### `deploy/local/` — Docker Compose

**5 services:**
1. `nats:4222` — NATS 2.10 with JetStream (persistent at `./appdata/nats`)
2. `postgres:5432` — Postgres 16, `mio` DB, user `mio_app`
3. `minio:9000` — MinIO object storage (S3-compatible, console `:9001`)
4. `gateway:8080` — mio-gateway (connects to Postgres + NATS)
5. `sink-gcs:async` — sink-gcs consumer (writes to MinIO)
6. `echo-consumer:async` — Example Python consumer

**Healthchecks:** All services have `healthcheck` defined; gateway depends on postgres + nats ready.

**Port overrides:** Via `.env.local` (POSTGRES_PORT, NATS_PORT, NATS_MON_PORT, MINIO_API_PORT, MINIO_CONSOLE_PORT).

### `ee/` — Commercial Overlay

**Policy:**
- Build-tag-gated: `//go:build ee`
- Not part of Apache-2.0 OSS distribution
- OSS code must NOT import from `ee/`
- Dep direction: `ee/` → `services/`, `pkg/`, `sdks/` only (no reverse deps)

**Today:** Placeholder (empty). Reserved for future commercial features (e.g., audit logs, advanced rate limiting, RBAC).

### `deploy/charts/` — Helm Charts (7)

1. **mio-nats** — NATS JetStream cluster (3-replica StatefulSet)
   - Upstream nats chart as dependency
   - `values.yaml` (GKE prod), `values-kind.yaml` (local kind cluster)
   - Config: JetStream mode, file store (PVC), cluster replication

2. **mio-jetstream-bootstrap** — JetStream verification (Job after owning services start)
   - Job template with RBAC (ServiceAccount, Role)
   - ConfigMap: expected stream definitions (MESSAGES_INBOUND, MESSAGES_INBOUND_ENRICHED, MESSAGES_OUTBOUND)
   - Durable consumer policy checks (media-vault, sender-pool, gcs-archiver, optional external consumers)

3. **mio-gateway** — Main API Deployment (2 replicas POC)
   - Deployment, Service, Ingress, ConfigMap, Secret, ServiceAccount, HPA, ServiceMonitor
   - Env: NATS cluster URL, Postgres DSN, webhook secret, age identity file
   - Readiness: `/health` gate
   - Metrics: ServiceMonitor for Prometheus scrape

4. **mio-source-reconciler** — History/backfill publisher (optional)
   - Binary: `services/gateway/cmd/source-reconciler`
   - Env: Postgres DSN, NATS URL, account id, conversation external id, optional cursor/window
   - Flow: provider history API → store dedupe → publish fresh `MESSAGES_INBOUND` → cursor/status update

5. **mio-media-vault** — Attachment persistence sidecar (1 replica POC)
   - Deployment, ServiceAccount, lifecycle init Job
   - Env: GCS bucket (Workload Identity), NATS cluster URL
   - Flow: consume MESSAGES_INBOUND → fetch platform attachments → enrich → publish MESSAGES_INBOUND_ENRICHED

6. **mio-sink-gcs** — Cold storage archiver (1 replica)
   - Deployment, ServiceAccount (Workload Identity to GCP SA), ServiceMonitor
   - Env: GCS bucket path, NATS cluster URL
   - Flow: consume MESSAGES_INBOUND → batch NDJSON → flush to GCS

7. **mio-echo-consumer** — Example Python consumer (1 replica, reference only)
   - Deployment, ServiceAccount
   - Env: NATS cluster URL, optional dry-run flag

**Image registry:** `ghcr.io/crashchat-ai/mio/{component}:{sha}` on main pushes, plus SemVer tags from `v*` releases. Helm charts publish as OCI artifacts under `ghcr.io/crashchat-ai/mio/charts` with chart version and appVersion equal to the release version. Infra repo bumps remain explicit.

### `docs/` — Documentation

**Core docs:**
- `system-architecture.md` — Component map, inbound/outbound flows, storage tiers, observability, open questions
- `deployment-guide.md` — GKE reference, cluster shape, secret rotation, HA upgrade path, attachment persistence flow
- `project-overview-pdr.md` — Vision, scope, functional/non-functional requirements, success metrics
- `code-standards.md` — Coding conventions, governance rules, adapter pattern, proto policy
- `codebase-summary.md` — This file

**Journals:**
- `journals/journal-260507-the-problem.md` — Pre-POC problem statement
- `journals/journal-writer-260510-0109-p9-attachment-persistence-shipped.md` — P9 completion log

---

## Data Model

**Four-tier addressing:**
```
tenant_id → account_id → conversation_id → message_id
```

**Idempotency:**
- NATS dedup: `Nats-Msg-Id` header within 2-minute `duplicate_window` (catches gateway retries)
- Postgres: UNIQUE(account_id, source_message_id) (authoritative, catches channel-level redeliveries)

**Key tables:**
- `messages` — (account_id, source_message_id, text, sender_id, conversation_id, received_at, ...)
- `attachments` — (message_id, filename, content_type, storage_key, content_sha256, ...)
- `credentials` — (account_id, channel_type, credential_type, encrypted_value, created_at, rotated_at, ...)
- (schema source: `services/gateway/store/migrations/`)

---

## Streams & Subjects

| Stream | Subject Pattern | Retention | Max Age | Purpose |
|---|---|---|---|---|
| `MESSAGES_INBOUND` | `mio.inbound.>` | limits (1GB per account) | 7d | Raw inbound. Published by gateway. Consumed by media-vault + sink-gcs. |
| `MESSAGES_INBOUND_ENRICHED` | `mio.inbound_enriched.>` | limits (1GB per account) | 7d | Enriched with persistent attachment URLs. Published by media-vault. Consumed by AI service. |
| `MESSAGES_OUTBOUND` | `mio.outbound.>` | workqueue | 24h | Drain semantics. Published by AI service. Consumed by gateway sender-pool. |

**Subject grammar:**
```
mio.<direction>.<channel_type>.<account_id>.<conversation_id>[.<message_id>]
```

**Examples:**
```
mio.inbound.zoho_cliq.<uuid>.<uuid>
mio.outbound.slack.<uuid>.<uuid>.<uuid>
```

---

## Local Development Quickstart

**Setup:**
```bash
cd /Users/vanducng/git/personal/agents/mio
mise install              # Pins Go 1.25, Python 3.12, buf, protoc 27
make up                   # docker-compose: NATS, Postgres, MinIO
make proto                # buf generate → proto/gen/
```

**Test:**
```bash
make test                 # All Go modules
make sdk-py-test          # Python SDK only
make gateway-test         # Gateway unit tests (no live deps)
make gateway-bench-outbound  # Fairness bench (account A burst, account B p99 < 2s)
```

**Run:**
```bash
# All-in-one demo (embedded NATS):
make run-laptop           # Memory storage
make run-laptop-persist   # File storage (./var/jetstream)

# Admin server:
make admin-run            # loopback:9090

# TUI:
make tui-run              # Connect to admin server
```

**Build images:**
```bash
make gateway-build-local      # gateway:dev
make sink-gcs-build-local     # sink-gcs:dev
make gateway-build            # gateway:$(git describe)
```

---

## Testing Topology

**Unit tests** (no live deps):
- `services/gateway/internal/...` → `go test ./...` (no NATS/Postgres)
- `sdks/go/...` → `go test ./...`
- `sdks/python/...` → `pytest -m "not integration"` (pytest markers)
- `services/sink-gcs/internal/...` → `go test ./...`

**Integration tests** (live deps required):
- Set `MIO_TEST_DSN="postgres://user:pass@localhost/mio"`
- `services/gateway/integration_test/...` → `go test ./...`
- `sdks/python/...` → `pytest -m integration`

**CI path filters** (`.github/workflows/ci.yaml`):
- `proto/**` → test-proto (buf lint + breaking)
- `services/gateway/**`, `sdks/go/**` → test-gateway (lint + go test)
- `sdks/python/**`, `examples/echo-consumer/**` → test-python (ruff + pytest)
- `deploy/charts/**` → helm-lint (all charts)
- `services/media-vault/**` → test-media-vault (go test)
- `services/sink-gcs/sql/**`, `proto/mio/v1/**` → test-bq-schema (schema drift check)

---

## References

- [System Architecture](system-architecture.md) — Design principles, component interaction
- [Code Standards](code-standards.md) — Governance, adapter pattern, proto policy
- [Deployment Guide](deployment-guide.md) — Operations, secret rotation, GKE reference
- [Project Overview](project-overview-pdr.md) — Requirements, success metrics
