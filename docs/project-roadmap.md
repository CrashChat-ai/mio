# MIO — Project Roadmap

**Last updated:** 2026-05-13  
**Current phase:** P9 ✅ (attachment persistence shipped)  
**Next focus:** P10–P11 (BigQuery sink, channel registry control plane)

---

## Phase Tracker

| Phase | Status | Title | Description | Plan Ref |
|---|---|---|---|---|
| **P0** | ✅ | Repo scaffold | Initialize monorepo, Makefile, docker-compose | `plans/260507-0904-mio-bootstrap/` |
| **P1** | ✅ | Proto v1 envelope | Define Message, SendCommand, Attachment, Capabilities | — |
| **P2** | ✅ | SDKs (Go + Python) | Implement sdk-go and sdk-py with OTel + Prometheus | — |
| **P3** | ✅ | Gateway + Cliq inbound | Webhook handler, HMAC verify, normalize to Message | — |
| **P4** | ✅ | Echo consumer | Example Python consumer (POC proof-of-concept) | — |
| **P5** | ✅ | Outbound path → Cliq | SendCommand dispatch, rate limiting, edit support | — |
| **P6** | ✅ | Sink-GCS archiver | Consumer that writes raw payloads to GCS (cold storage) | — |
| **P7** | ✅ | Helm charts + NATS | 6 Helm charts, NATS StatefulSet, JetStream bootstrap | — |
| **P8** | ✅ | POC deploy on GKE | Reference Kubernetes topology, CNPG Postgres, Flux reconciliation | `plans/260509-2125-p8-poc-deploy-gke/` |
| **P9** | ✅ | Attachment persistence | Media-vault sidecar, content-addressed storage, 7-day TTL | `plans/260509-2328-attachment-persistence/` |
| **P10** | 🚧 | BigQuery sink / lakehouse | External tables + native warehouse table, loader pipeline | `plans/260510-1102-bq-sink-lakehouse/` |
| **P11** | 🚧 | Channel registry control plane | Admin server RPC for channel installs, credentials, tenants | `plans/260513-0351-channel-management-control-plane/` |
| **—** | — | Second channel adapter (Slack) | Webhook inbound, API outbound, per-channel rate limits | open |
| **—** | — | ELT pipeline (Airflow DAG) | Scheduled Cloud Run Job for BigQuery loader | `plans/260510-2333-elt-mio-airflow-dag/` |
| **—** | — | Cliq OAuth refresh hardening | Token refresh retry/backoff, credential rotation | `plans/260510-0152-cliq-oauth-token-refresh/` |
| **—** | — | TUI write operations | Admin TUI: create tenants, manage channels, set credentials | open |
| **—** | — | NATS HA upgrade | 3-replica cluster + PVC storage, stream replication | see deployment-guide |

---

## Shipped Highlights

### P9: Attachment Persistence (Latest)

**Date shipped:** 2026-05-10  
**Key features:**
- Media-vault sidecar pulls from MESSAGES_INBOUND, enriches, publishes to MESSAGES_INBOUND_ENRICHED
- Attachments fetched within platform TTL (Cliq ~12 min) and persisted to GCS
- Content-addressed deduplication (same image = single object)
- Lifecycle rule: 7-day expiry on attachments (matches JetStream MaxAge)
- AI consumers always retrieve from persistent storage (no platform TTL race)

**Success metrics:**
- Attachment retrievable ≥7 days after receipt ✅
- Gateway p99 inbound latency < 500ms (unaffected) ✅
- Per-account rate limiting (unaffected) ✅

**Code:** `services/media-vault/`, `deploy/charts/mio-media-vault`  
**Report:** `plans/reports/Cook-260509-2328-attachment-persistence.md`

### P8: POC Deploy on GKE

**Date shipped:** 2026-05-09  
**Key features:**
- Reference Kubernetes topology (ingress → gateway → NATS → consumers)
- CNPG Postgres integration (1 instance POC, 10Gi PVC)
- Secret rotation (SOPS-encrypted in infra repo)
- FluxCD reconciliation (external infra repo)
- NATS 1-replica emptyDir (accepts data loss risk, HA path documented)

**Deployment:**
- GKE regional cluster, single namespace `mio`
- 2x gateway replicas, 1x media-vault, 1x sink-gcs, 1x echo-consumer
- Workload Identity for GCS access
- ingress-nginx + cert-manager (HTTP-01 via Cloud DNS)

**Code:** `deploy/charts/`, `deploy/fluxcd/`  
**Report:** `plans/reports/Cook-260509-2125-p8-poc-deploy-gke.md`

---

## In Progress / Planned

### P10: BigQuery Sink / Lakehouse

**Status:** Planning phase  
**Goal:** Materialize GCS archive into queryable BigQuery dataset

**Components:**
- `raw_mio.messages_external` — EXTERNAL TABLE on GCS NDJSON (ad-hoc queries, duplicates by design)
- `raw_mio.messages` — Native partitioned table (authoritative, deduped on `(account_id, source_message_id)`)
- `raw_mio.messages_dedup` — View (analyst-facing canonical query)
- `raw_mio.messages_errors` — Quarantine for validation failures (30-day expiry)
- **Loader pipeline** — External: Cloud Run Job (hourly) or Airflow DAG, runs `validate.sql` + `merge.sql`

**SLA:** Rows visible in `messages` within ~10–60 min of NDJSON write (hourly job + 5-min budget)

**Schema contract:** Proto changes must update `services/sink-gcs/sql/messages_schema.json` (CI guard: `check-proto-drift.sh`)

**Code:** `services/sink-gcs/sql/{messages_schema.json, check-proto-drift.sh}` (producer-side); loader lives in deployer's infra repo

**Plan:** `plans/260510-1102-bq-sink-lakehouse/`

### P11: Channel Registry Control Plane

**Status:** Planning phase  
**Goal:** Admin server RPC for dynamic channel management (tenants, installs, credentials)

**Components:**
- `cmd/admin` control-plane server (connect-go RPC on loopback:9090 + CIDR allowlist)
- `AdminService` RPC:
  - `CreateTenant(name, owner_email)` → tenant_id
  - `ListChannelTypes()` → {name, status, capabilities}
  - `CreateChannelInstall(tenant_id, channel_type, config)` → account_id
  - `ListChannelInstalls(tenant_id)` → {account_id, channel_type, status}
  - `GetCredentials(account_id)` → encrypted credential summary
  - `RefreshCredentials(account_id)` → trigger OAuth refresh
  - `TailMessages(account_id, conversation_id?)` → stream of messages (read-only)
  - `StartInstall(channel_type, redirect_uri)` → oauth_url
  - `CompleteInstall(code, state)` → account_id, refresh_token

**TUI integration:** bubbletea UI (read-only v1 → read-write v2)

**Code:** `services/gateway/internal/admin/`, `services/tui/`

**Plan:** `plans/260513-0351-channel-management-control-plane/`

---

## Open Questions (Design, Not Blocking)

### 1. Per-Thread Ordering Shard

**Current:** Global `MaxAckPending=1` on `ai-consumer-enriched` (single-flight, correct but slow)

**Decision needed:** When throughput hits 1k msg/s, shard by subject?

```
Option A (now): ai-consumer-enriched consumes mio.inbound_enriched.*, MaxAckPending=1
Option B (future): N consumers, each watches mio.inbound_enriched.<channel>.<acct>.<conv>.*, MaxAckPending=1
```

**Trade-off:** B gains throughput (parallel conversations); A is correct. Start with A, graduate when needed.

### 2. Edit Semantics Resolver

**Problem:** Each channel has different edit models.
- Slack: edit_message (overwrites + timestamp immutable)
- Cliq: edit works + timestamp updates
- Telegram: edit_message_text (full replacement)
- Discord: edit requires same bot + message_id

**Current:** SendCommand has `edit_of_external_id` field, but no per-channel resolver.

**Design at P11+:** Adapter-specific `ResolveEditTarget(SendCommand) → (*ExternalMessage, error)` method.

### 3. Dead-Letter Strategy

**Problem:** What happens when sender-pool gets permanent errors (4xx)?

**Options:**
- A: Separate `MESSAGES_DLQ` stream (explicit dead-letter queue)
- B: In-place `terminated` flag in Postgres (soft quarantine)
- C: Separate error table + alert (requires schema change)

**Decision:** Defer until first real channel permanent failure in production. For now, Nak → retry → max_deliver → drop.

### 4. Attachment Backend Portability

**Current:** GCS only (storage interface ready in media-vault).

**Future:** S3, Azure Blob, Backblaze B2?

**Design:** Factory pattern ready (`internal/storage/` interface). Defer multi-backend support until operational need arises.

---

## Recently Shipped Highlights

### Cliq OAuth Token Refresh (In Progress)

**Status:** 🚧 Research phase  
**Goal:** Hardened token refresh with retry/backoff

**Plan:** `plans/260510-0152-cliq-oauth-token-refresh/`

### ELT Pipeline (Airflow DAG)

**Status:** 🚧 Design phase  
**Goal:** Scheduled Cloud Run Job for BigQuery loader

**Plan:** `plans/260510-2333-elt-mio-airflow-dag/`

---

## Dependencies & Blockers

| Phase | Blocker | Status |
|---|---|---|
| P10 | P9 attachment persistence complete | ✅ Shipped |
| P10 | BigQuery dataset + schema defined | ⏳ Pending deployer |
| P11 | Admin service RPC stubs (proto) | ✅ Generated |
| P11 | TUI bubbletea scaffold | ✅ Complete |
| Slack adapter | P11 channel registry control plane | 🚧 In progress |
| Slack adapter | SDK attribute promotion rule enforced | ✅ Done |

---

## Success Metrics (POC → Production)

### Latency SLOs

- **Inbound:** p99 < 500ms (current target, P8–P9 measured ✅)
- **Outbound:** p95 < 2s per channel API (per-channel rate limit fairness)
- **Attachment fetch:** < 100ms (content-addressed, cached in media-vault)

### Reliability

- **Message loss:** RTO < 5 min (pod crash → new pod joins cluster)
- **Attachment availability:** RPO < 1 min (stream retention 7d inbound, media-vault acks only after GCS write)
- **Per-account fairness:** Account A burst 50 msg/s, Account B p99 < 2s (TestFairness bench, currently passing ✅)

### Scalability (Future Milestones)

- **P10 milestone:** 1k msg/sec inbound (burst per account, trigger shard discussion)
- **P11 milestone:** 10k msg/sec cluster-wide, 10 channel adapters registered
- **Production:** 100k msg/sec (multi-region, HA NATS R=3)

---

## Next Immediate Steps

1. **P10 (BQ sink):** Finalize loader pipeline spec (Cloud Run Job vs. Airflow), DDL for native tables
2. **P11 (admin control plane):** Implement remaining AdminService RPCs, TUI write operations
3. **Slack adapter:** Research webhook format, OAuth flow, rate limits; add to proto/channels.yaml
4. **Cliq OAuth hardening:** Implement exponential backoff for token refresh failures

---

## References

- [System Architecture](system-architecture.md) — Design invariants, open questions detail
- [Deployment Guide](deployment-guide.md) — Kubernetes reference, HA paths
- [Code Standards](code-standards.md) — Governance rules, adapter pattern
- `plans/` — Detailed phase plans and research reports
- `README.md` — Status table
