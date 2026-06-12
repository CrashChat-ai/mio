# MIO — Open Channel Gateway

MIO is an open-source channel gateway that connects your app (AI agents, workflows, services)
to the chat platforms your customers actually use: Zoho Cliq, Slack, Telegram, Discord, and more.
It normalizes messy channel webhooks into a clean, durable envelope and routes messages via NATS JetStream.

> Build once, integrate anywhere. Your code stays channel-agnostic.

**Position:** Community adopters integrate any chat surface once via the adapter contract (`pkg/channels/`).
Operator console ([mio-web](./ui/web)) manages accounts and credentials. Reference consumer
([channel-pulse](https://github.com/crashchat-ai/channel-pulse)) demonstrates AI integration.

## Why decoupled

Every channel webhook has a hard ack deadline (Slack and Discord: 3s).
LLM calls take 2–30s. Coupling them drops messages on the first slow run.

So MIO sits in between:

```
Channel webhook ─► mio-gateway ─► NATS JetStream ─► AI consumer
                       ▲                                │
                       └──── mio-gateway sender ◄───────┘
```

Gateway acks fast, durably persists to a bus, AI service consumes on its
own schedule. Bonus: replay for prompt iteration, failure isolation,
independent scaling, new consumers (analytics, archive) without touching
the receiver.

## Components

The repo is organised by role (services / ui / sdks / channels / shared libs).
Migration history: see `.work/plans/260513-0833-repo-layout-option-b/`.

| Component | Lang | Role |
|---|---|---|
| `services/gateway/` | Go | Stateless ingress/egress. Per-channel handler + consumer pool, per-workspace rate limits. |
| `services/media-vault/` | Go | Attachment ingestion within platform TTL → GCS, message enrichment. |
| `services/sink-gcs/` | Go | Raw-payload sink for cold storage + analytics substrate. |
| `ui/tui/` | Go | Admin terminal UI (bubbletea), read-only v1. Connect-RPC client to admin server. |
| `ui/web/` | Go/TS | Reserved for the operator web admin BFF + embedded React SPA. |
| `sdks/go/` | Go | Thin NATS wrapper. Idempotency, OTel, Prometheus, schema-version checks. Importable as `github.com/crashchat-ai/mio/sdk-go` (module path preserved despite directory move). |
| `sdks/python/` | Python | Async-only NATS wrapper for AI service integration (LangGraph-compatible). |
| `channels/` | Go | In-tree channel adapters (Cliq today; Slack, Telegram, … planned). Each adapter registers via `init()`; barrel package `channels/all` blank-imports them. |
| `pkg/channels/` | Go | Public adapter contract (Adapter, InboundAdapter, CredentialAdapter, DeliveryError interfaces). Gateway + media-vault depend on this only. |
| `proto/` | Protobuf | Canonical schema. `Message`, `SendCommand`, `Attachment`, `Capabilities`, `AdminService`. `buf`-managed, versioned. |
| `pkg/` | Go | Shared internal libraries (code lands here only when at least two callers genuinely share it). |
| `ee/` | — | Build-tag-gated commercial overlay (`//go:build ee`); not part of OSS Apache-2.0 distribution. |
| `tools/` | Go | Build/codegen helpers (`genchanneltypes`, `proto-roundtrip`). |
| `examples/` | Polyglot | Sample consumers (e.g. `echo-consumer/`). |
| `deploy/` | — | Helm charts (`deploy/charts/`), local docker-compose (`deploy/local/`), fluxcd manifests (`deploy/fluxcd/`). |
| `docs/` | — | Project documentation. |
| `hack/` | — | Dev-only scratch, spikes, playground. Not shipped, not tested. |

## Stack

- **Bus**: NATS JetStream (cloud-agnostic; 3-replica on GKE, embedded or external)
- **Schema**: Protobuf via `buf` (lint STANDARD, breaking WIRE_JSON)
- **Storage**: Postgres (operational); S3/MinIO/GCS (attachments + message archive)
- **Platform**: Kubernetes-ready (Helm charts); local dev via docker-compose or all-in-one binary
- **SDK**: Go + Python async/await (LangGraph-compatible)

## Quickstart

```bash
git clone https://github.com/crashchat-ai/mio.git
cd mio
mise install            # pins Go 1.25, Python 3.12, buf, protoc 27
make up                 # NATS + Postgres + MinIO (all three healthy)
make proto              # buf generate → proto/gen/
```

### Port collisions

Default ports: Postgres **5432**, NATS **4222** + **8222**, MinIO **9000** + **9001**.

If any collide with existing local services:

```bash
cp .env.example .env.local
# edit .env.local, then:
export $(grep -v '^#' .env.local | xargs)
make up
```

See `.env.example` for the full list of overridable variables.

## Design rules — non-negotiable

1. **Gateway is dumb.** Validate signature, normalize, publish, ack. No business logic.
2. **Consumers talk to NATS directly via the SDK.** No proxy service.
3. **Idempotency at the edge.** `(account_id, source_message_id)` unique constraint.
4. **Per-workspace rate limits, not global.** One chatty tenant must not starve others.
5. **Per-thread ordering** via single-replica AI consumer with `MaxAckPending=1`.
6. **Two-step UX for slow LLM calls.** Send "thinking…" immediately, edit-in-place when answered.

## Documentation

- [Project Overview & PDR](docs/project-overview-pdr.md) — Vision, scope, requirements
- [System Architecture](docs/system-architecture.md) — Design doc, component map, inbound/outbound flows
- [Code Standards](docs/code-standards.md) — Coding conventions, governance rules
- [Deployment Guide](docs/deployment-guide.md) — GKE reference, secret rotation, operations
- [Project Roadmap](docs/project-roadmap.md) — Phased build plan, status tracker
- [Codebase Summary](docs/codebase-summary.md) — Directory layout, component deep-dive
- [Contributing](CONTRIBUTING.md) — Attributes promotion, channel_type registry, proto field numbers

## Status

POC. Phase tracker:

- [x] **P0** — Repo scaffold
- [x] **P1** — Proto v1 envelope
- [x] **P2** — SDKs (`sdks/go`, `sdks/python`)
- [x] **P3** — Gateway + Zoho Cliq inbound
- [x] **P4** — `examples/echo-consumer/`
- [x] **P5** — Outbound path → Cliq
- [x] **P6** — `mio-sink-gcs`
- [x] **P7** — Helm charts + NATS on GKE
- [x] **P8** — POC deploy on GKE
- [x] **P9** — Attachment persistence
- [x] **P9.5** — Admin control plane (connect-rpc) + TUI (bubbletea, read-only v1) + embedded NATS option + role-based monorepo layout ✅
- [ ] **P10** — BigQuery sink / lakehouse (planned)
- [ ] **P11** — Channel registry control plane + TUI write ops (planned)
- [ ] Second channel adapter (Slack) — open

## License

Apache-2.0
