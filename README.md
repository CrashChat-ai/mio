# MIO — Messaging I/O for AI Agents

MIO is a messaging I/O platform that connects AI agents to the chat channels
customers actually live in (Zoho Cliq first, then Slack, Telegram, Discord, …)
and gives them a clean, channel-agnostic envelope to receive messages and
respond back.

> Channels are messy. Agents shouldn't care.

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

| Component | Lang | Role |
|---|---|---|
| `gateway/` | Go | Stateless. One handler per channel for inbound; one consumer pool per channel for outbound. Per-workspace rate limits. |
| `sdk-go/` | Go | Thin NATS wrapper. Idempotency, OTel, Prometheus, schema-version checks. |
| `sdk-py/` | Python | Async-only NATS wrapper for AI service integration (LangGraph-compatible). |
| `proto/` | Protobuf | Canonical schema. `Message`, `SendCommand`, `Attachment`, `Capabilities`. `buf`-managed, versioned. |
| `sink-gcs/` | Go | Consumer that writes raw payloads to GCS. Cold storage + analytics substrate. |
| `media-vault/` | Go | Attachment ingestion and storage service. Fetches within platform TTL, persists to GCS, enriches messages. |
| `tui/` | Go | Terminal UI for admin server (bubbletea). Read-only v1. |
| `examples/echo-consumer/` | Python | Tiny stub proving the loop. |
| `deploy/local/` | — | `docker-compose.yml` + Postgres init + dev secrets for local dev. |
| `deploy/charts/` | — | Helm charts (6 sub-charts) for GKE deployment. |

## Stack

- **Bus**: NATS JetStream (3-replica on GKE; cloud-agnostic)
- **Schema**: Protobuf via `buf` (lint STANDARD, breaking WIRE_JSON)
- **Storage**: Postgres + pgvector (operational); GCS (raw + attachments)
- **Platform**: GKE for POC; only K8s primitives, no managed lock-in
- **Local dev**: `docker compose` brings up NATS + Postgres + MinIO

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
- [x] **P2** — SDKs (`sdk-go`, `sdk-py`)
- [x] **P3** — Gateway + Zoho Cliq inbound
- [x] **P4** — `examples/echo-consumer/`
- [x] **P5** — Outbound path → Cliq
- [x] **P6** — `mio-sink-gcs`
- [x] **P7** — Helm charts + NATS on GKE
- [x] **P8** — POC deploy on GKE
- [x] **P9** — Attachment persistence ✅
- [ ] **P10** — BigQuery sink / lakehouse (planned)
- [ ] **P11** — Channel registry control plane (planned)
- [ ] Second channel adapter (Slack) — open
- [ ] TUI write operations — open

## License

Apache-2.0
