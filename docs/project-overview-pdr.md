---
title: Project Overview & PDR
description: Problem, motivation, and product development requirements for the MIO messaging I/O platform.
---

# MIO — Project Overview & Product Development Requirements

**Last updated:** 2026-05-13 (P9.5 admin control plane + TUI shipped)  
**Status:** POC, locked design for core inbound/outbound path, admin introspection, control plane ready for tenant management

---

## Problem & Motivation

Every chat platform webhook has a hard ack deadline:

- Slack: 3 seconds
- Discord: 3 seconds
- Zoho Cliq: ~5 seconds
- Telegram: ~30 seconds (loose, but retry-heavy)

LLM-driven responses take 2–30 seconds. Coupling the webhook ack to an LLM call means:

1. First slow run drops the message (timeout)
2. Channel retries fire while the first request is still in-flight
3. Duplicate consumers corrupt the AI conversation state
4. One chatty user starves others (no per-tenant fairness)

**MIO's answer:** Decouple the transport layer from intelligence. Gateway acks fast within the channel deadline, durably persists to a bus, AI service consumes on its own schedule.

---

## Vision & Scope

**What MIO is:**
- A messaging I/O platform that normalizes inbound messages from any chat channel into a canonical envelope
- A durable bus (NATS JetStream) that decouples receive from processing
- A sender pool that dispatches outbound commands back to channels
- A sidecar (media-vault) that persists attachments across platform TTL boundaries
- A thin SDK for AI services to consume/publish without importing channel-specific code

**What MIO is not:**
- An AI agent framework (that's MIU's job)
- A UI or workspace admin console (owned by MIU)
- A staging cluster (solo-dev scale with fast rollback)
- A managed cloud service (cloud-agnostic K8s by design)
- A dedicated BigQuery sink (GCS + external tables, pipeline owned by deployer)

---

## Target Users

1. **AI service teams** — Build agents without understanding channel-specific quirks
2. **Channel-ops engineers** — Deploy new channel adapters, manage inbound/outbound flows
3. **Deployment teams** — Self-host on GKE or run locally for development
4. **Analytics consumers** — Tap the bus for training data, audit logs, cold storage

---

## Functional Requirements

### Inbound Path

- [x] Receive webhook POST from channel with HMAC signature
- [x] Verify signature (HMAC-SHA256)
- [x] Normalize to canonical `Message` proto envelope
- [x] Idempotent dedup via `(account_id, source_message_id)` UNIQUE constraint
- [x] Publish to `MESSAGES_INBOUND` stream within channel deadline (p99 < 500ms)
- [x] Silently 200 OK on duplicate to prevent channel retry loops
- [x] Support attachments with platform-TTL-aware fetching
- [x] Enrich attachment URLs with persistent storage references
- [x] Reconcile provider history gaps outside the webhook hot path

### Outbound Path

- [x] AI service publishes `SendCommand` to `MESSAGES_OUTBOUND` stream
- [x] Sender pool (per-channel consumer) drains work queue
- [x] Respect per-account rate limits (channel-specific)
- [x] Retry transient errors (5xx, network timeout) with exponential backoff
- [x] Terminal-error dead-letter on permanent failures (4xx)
- [x] Support message edits via `edit_of_external_id` reference
- [x] Support channel-agnostic rich outbound content (`SendCommand.rich_content`)
- [x] Render URL-backed outbound attachments in supported adapters

### Multi-tenancy & Fairness

- [x] Per-account rate limits (one workspace must not starve others)
- [x] Per-conversation ordering via `MaxAckPending=1` on AI consumer
- [x] Account-scoped idempotency keys
- [x] Tenant isolation in deployments

### Observability

- [x] OpenTelemetry trace context propagation (channel → gateway → bus → AI → outbound)
- [x] Prometheus metrics (inbound latency, outbound send count, JetStream consumer lag)
- [x] Structured JSON logging (slog in Go, structlog in Python)
- [x] Label discipline (only `channel_type`, `direction`, `outcome` — no cardinality bombs)

---

## Non-Functional Requirements

| Requirement | Target | Rationale |
|---|---|---|
| Gateway inbound p99 latency | < 500ms | Must ack within channel deadline (3–5s) |
| Attachment availability | ≥ 7 days after receipt | Spans LLM processing + retry window |
| AI consumer ordering | Per-conversation FIFO | Correct conversation thread |
| Per-account rate limiting | Enforce by channel API limits | Prevent workspace starvation |
| Data durability | 7-day retention on bus | Replay for iteration, re-training |
| Cloud portability | No vendor lock-in | Run on GKE, EKS, AKS unchanged |
| Wire format stability | WIRE_JSON breaking checks | SDK versioning safety |
| Schema evolution | Proto field numbers never reused | Historical data queryable |

---

## Architectural Principles

1. **Gateway is dumb.** Only signature → normalize → publish → ack. Intelligence lives in consumers.
2. **Consumers own their logic.** No shared business logic in the gateway; pull from the bus.
3. **Idempotency at the edge.** `(account_id, source_message_id)` is the idempotency key, not request UUID.
4. **Bus is the contract.** All consumers see the same protobuf `Message` and `SendCommand` envelopes.
5. **Per-workspace fairness.** Rate limits are per-account, not global. Slow consumers don't block others.
6. **Two-step UX for latency.** LLM run > 1s? Send "thinking…" first, then edit-in-place with the answer.
7. **Storage tiers are separate.** Operational (Postgres) ≠ Bus (NATS JetStream) ≠ Lake (GCS).

---

## Success Metrics

### POC Milestone (P8–P9)

- [x] End-to-end Cliq message → echo reply within 3-second budget
- [x] Attachment fetchable ≥ 7 days after receipt
- [x] Gateway p99 inbound latency < 500ms (measured)
- [x] Per-account rate limiting enforced (tested via `TestFairness` bench)
- [x] Zero data loss on channel signature verify → publish → ack

### Production Readiness (future)

- SLO: p99 inbound < 500ms, p95 outbound < 2s per channel API
- RTO: < 5 minutes on gateway crash (new pod + rejoin NATS cluster)
- RPO: < 1 minute (stream retention 7d for inbound, 24h for outbound)
- Scalability: 1k msg/sec inbound per account (burst), 10k msg/sec cluster-wide

---

## Stakeholders & Boundaries

### MIO owns (this repo)

- Message envelope schema (protobuf `Message`, `SendCommand`)
- Gateway (inbound signature verify, normalize, publish; outbound send)
- SDKs (sdk-go, sdk-py) for consuming/publishing
- Bus provisioning (NATS JetStream streams, retention, consumer defs)
- Archive sink (sink-gcs) to cold storage
- Attachment sidecar (media-vault) for TTL-aware persistence
- Control plane (admin server) for tenant/channel/message introspection

### MIU owns (separate repo)

- AI agent logic (LangGraph, Hatchet integration)
- Intelligence routing (which agent processes which message)
- Conversation state (Postgres operational storage)
- UI/UX (workspace admin console, agent management)

### Deployer owns (infra repo, not mio)

- Kubernetes cluster (GKE, EKS, etc.)
- Secret management (age keys, webhook secrets, credentials)
- CNPG Postgres instance + backups
- GCS bucket provisioning
- BigQuery loader pipeline (external to mio)
- FluxCD reconciliation (Helm releases)

### Contract between MIO and MIU

```
MESSAGES_INBOUND stream (mio.v1.Message)
        ↓ [AI consumes via sdk-py]
     [Think]
        ↓ [AI publishes via sdk-py]
MESSAGES_OUTBOUND stream (mio.v1.SendCommand)
        ↓ [Gateway sender-pool consumes]
     [Deliver to channel API]
```

SDK contract: `Client.consume()` and `Client.publish()` accept/return canonical proto messages. No channel-specific logic in either library.

---

## Open Questions (Design, not Implementation)

1. **Per-thread sharding:** Stay global `MaxAckPending=1` or shard by `conversation_id` when throughput demands? (Decide at 1k msg/s milestone)
2. **Edit semantics resolver:** Each channel (Slack, Cliq, Telegram, Discord) has different edit models. How does the gateway know which resolver to invoke? (Design at P5+, not now)
3. **Dead-letter strategy:** Separate `MESSAGES_DLQ` stream vs. in-place `terminated` flag + quarantine table? (Defer until first real permanent failure)
4. **Attachment backend swap:** S3, Azure Blob, Backblaze B2 factory pattern ready; do we need day-one multi-backend? (Defer until operational need)

---

## References

- [System Architecture](system-architecture.md) — Full design doc with mermaid diagrams
- [Code Standards](code-standards.md) — Governance rules, adapter pattern, subject grammar
- [Deployment Guide](deployment-guide.md) — GKE reference, secret rotation, HA paths
- [Codebase Summary](codebase-summary.md) — Component layout and API surfaces
