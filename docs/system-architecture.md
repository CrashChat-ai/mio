# MIO — System Architecture

> Status: design doc, locked-in for the POC. Last updated 2026-05-10 (P9 attachment-persistence shipped).

MIO is the messaging I/O platform for [MIU](https://github.com/vanducng/miu).
Channels are messy; agents shouldn't care. MIO normalizes every chat
surface (Zoho Cliq first, then Slack / Telegram / Discord / …) into one
canonical envelope so MIU's AI service receives a `Message` and returns
a `SendCommand` — without ever importing a channel SDK.

This document is the source of truth for **what MIO is**. The phased
build plan and morning journal live in `plans/plan.md` (local-only).

---

## 1. Why decoupled

Every channel webhook has a hard ack deadline (Slack: 3s, Discord: 3s,
Cliq: ~5s). LLM calls take 2–30s. Coupling them drops messages on the
first slow agent run.

MIO sits in between: gateway acks fast, durably persists to a bus, AI
service consumes on its own schedule. Side benefits:

- **Replay** for prompt iteration and training-data harvest
- **Failure isolation** between transport and intelligence
- **Independent scaling** — gateway is bursty/CPU-light, AI is steady/CPU-heavy
- **New consumers for free** — analytics, archive, audit can subscribe without touching the receiver

---

## 2. Component map

```mermaid
flowchart LR
  subgraph Channels
    cliq[Zoho Cliq]
    slack[Slack]
    tg[Telegram]
    disc[Discord]
  end

  subgraph "MIO (this repo)"
    gw["mio-gateway<br/>(Go, stateless)"]
    bus[("NATS JetStream<br/>3-replica cluster")]
    sink["mio-sink-gcs<br/>(Go consumer)"]
    dl["mio-attachment-downloader<br/>(Go sidecar)"]
    sdkgo["sdk-go"]
    sdkpy["sdk-py"]
  end

  subgraph "MIU (separate repo)"
    ai["AI service<br/>(Python, LangGraph<br/>+ Hatchet)"]
    pg[(Postgres<br/>+ pgvector)]
  end

  gcs[(GCS<br/>raw archive +<br/>attachments)]
  bq[(BigQuery<br/>external tables)]

  cliq -- webhook --> gw
  slack -- webhook --> gw
  tg -- webhook --> gw
  disc -- webhook --> gw

  gw -- "publish<br/>MESSAGES_INBOUND" --> bus
  bus -- "consume<br/>(gcs-archiver)" --> sink
  sink --> gcs
  bus -- "consume<br/>(attachment-downloader)" --> dl
  dl -- "fetch bytes,<br/>persist,<br/>enrich" --> gcs
  dl -- "publish<br/>MESSAGES_INBOUND_ENRICHED" --> bus
  bus -- "consume<br/>(ai-consumer-enriched)" --> ai
  ai --> pg
  ai -- "publish<br/>MESSAGES_OUTBOUND" --> bus
  bus -- "consume<br/>(sender-pool)" --> gw
  gw -- "REST/API call" --> cliq
  gw -.-> slack
  gw -.-> tg
  gw -.-> disc

  gcs -. external tables .-> bq

  sdkgo -. used by .-> gw
  sdkgo -. used by .-> sink
  sdkgo -. used by .-> dl
  sdkpy -. used by .-> ai
```

Crucial: **the AI service is not in this repo.** MIO ships the SDKs and
guarantees the envelope; MIU imports `sdk-py` and lives elsewhere. This
is the boundary that keeps "intelligence" and "transport" separable.

---

## 3. Inbound data flow

The hot path on receive. Every step has a clear owner.

```mermaid
sequenceDiagram
  autonumber
  participant Ch as Channel<br/>(e.g. Zoho Cliq)
  participant GW as mio-gateway
  participant DB as Postgres<br/>(idempotency)
  participant JS as JetStream<br/>MESSAGES_INBOUND
  participant DL as mio-attachment-<br/>downloader
  participant GCS as GCS<br/>(attachments)
  participant JSE as JetStream<br/>MESSAGES_INBOUND_ENRICHED
  participant AI as MIU AI service<br/>(ai-consumer-enriched)

  Ch->>GW: POST /webhooks/{channel} (signed)
  GW->>GW: verify HMAC signature
  GW->>GW: normalize → mio.v1.Message
  GW->>DB: INSERT (account_id, source_message_id) ON CONFLICT DO NOTHING
  alt duplicate
    DB-->>GW: 0 rows
    GW-->>Ch: 200 OK (silently dedup)
  else fresh
    DB-->>GW: 1 row
    GW->>JS: Publish(subject, payload, Nats-Msg-Id)
    JS-->>GW: PubAck (seq#)
    GW-->>Ch: 200 OK
    Note over GW,Ch: ack inside channel deadline (≤3s)
  end

  JS->>DL: Pull (attachment-downloader, MaxAckPending=N)
  alt has attachments
    DL->>DL: fetch bytes from platform URL<br/>(within platform TTL)
    DL->>GCS: Put (content-addressed, deduplicated)
    DL->>DL: enrich: add storage_key,<br/>content_sha256
  else no attachments
    DL->>DL: pass through unchanged
  end
  DL->>JSE: Publish enriched Message<br/>to MESSAGES_INBOUND_ENRICHED
  DL->>JS: Ack
  Note over DL: idempotent; republish<br/>safe on error redo

  JSE->>AI: Pull (ai-consumer-enriched, MaxAckPending=1)
  AI->>AI: LangGraph run (2–30s)
  AI->>AI: fetch attachment bytes from<br/>storage_key (no platform TTL risk)
  AI->>JSE: Ack
  Note over AI: AI may publish "thinking..."<br/>SendCommand immediately,<br/>then edit when done
```

Latency budget on the gateway path: **target p99 < 500ms**, hard ceiling
the channel deadline. Anything that doesn't fit (signature verify,
Postgres upsert, NATS publish) is moved off-path or pre-warmed.

---

## 4. Outbound data flow

The reply path. AI publishes a `SendCommand`; gateway delivers to the
channel and reports back.

```mermaid
sequenceDiagram
  autonumber
  participant AI as MIU AI service
  participant JS as JetStream<br/>MESSAGES_OUTBOUND
  participant GW as mio-gateway<br/>(sender-pool)
  participant RL as Per-workspace<br/>rate limiter
  participant Ch as Channel API

  AI->>JS: Publish SendCommand (workqueue)
  JS->>GW: Pull (sender-pool, MaxAckPending=N)
  GW->>RL: token check (workspace_id)
  alt limited
    RL-->>GW: deny
    GW->>JS: Nak with delay
  else allowed
    GW->>Ch: REST/API call
    alt success
      Ch-->>GW: 200 OK + message_id
      GW->>JS: Ack
    else 5xx / network
      GW->>JS: Nak (retry up to max_deliver)
    else 4xx (permanent)
      GW->>JS: TermAck (move to dead-letter)
    end
  end
```

Two-step UX rule: for any LLM run > 1s, the AI service emits a "thinking…"
`SendCommand` first, then an **edit** `SendCommand` referencing the same
`channel_message_id` once the real answer is ready. The user never sees
a blank thread.

---

## 5. Streams and subjects

Three streams, all file-backed, all `mio.v1` envelope.

| Stream | Subject pattern | Retention | Max age | Purpose |
|---|---|---|---|---|
| `MESSAGES_INBOUND` | `mio.inbound.>` | `limits` | 7d | Raw inbound. Gateway publisher. Attachment-downloader + sink-gcs consumers. (Old AI consumer deprecated.) |
| `MESSAGES_INBOUND_ENRICHED` | `mio.inbound_enriched.>` | `limits` | 7d | Enriched with persistent attachment URLs. Attachment-downloader publisher. AI consumer + future analytics subscribers. |
| `MESSAGES_OUTBOUND` | `mio.outbound.>` | `workqueue` | 23h | Drain semantics. Sender-pool is the only consumer. |

### Subject grammar

```
mio.<direction>.<channel_type>.<account_id>.<conversation_id>[.<message_id>]
        ▲              ▲             ▲              ▲                ▲
        │              │             │              │                └─ optional, for edit/delete commands
        │              │             │              └─ enables per-conversation ordering filters
        │              │             └─ per-account rate-limit / multi-tenant scoping (one tenant may run multiple accounts)
        │              └─ registry slug from proto/channels.yaml (zoho_cliq, slack, telegram, discord) — underscore for multi-word
        └─ inbound | inbound_enriched | outbound
```

Examples:

```
mio.inbound.zoho_cliq.<account-uuid>.<conv-uuid>
mio.inbound_enriched.zoho_cliq.<account-uuid>.<conv-uuid>
mio.outbound.slack.<account-uuid>.<conv-uuid>.<msg-uuid>
mio.outbound.zoho_cliq.<account-uuid>.<conv-uuid>.<msg-uuid>
```

Why these dimensions live in the subject:

| Dimension | Rationale |
|---|---|
| `direction` | One stream per direction; subject prefix lets a single filter scope a consumer cleanly. |
| `channel_type` | Per-channel sender pools, per-channel rate-limit buckets, per-channel sinks. Registry slug, not enum. |
| `account_id` | Per-account rate limits — one chatty workspace must not starve others; idempotency key with `source_message_id`. |
| `conversation_id` | Future-proofs partition-per-conversation when global `MaxAckPending=1` graduates (subject-shard). |

---

## 6. Consumer model

| Consumer | Stream | Type | `MaxAckPending` | Notes |
|---|---|---|---|---|
| `attachment-downloader` | `MESSAGES_INBOUND` | Pull, durable | **N** | Fetches attachment bytes within platform TTL, persists to storage, publishes to enriched stream. Stateless; can scale horizontally. |
| `gcs-archiver` | `MESSAGES_INBOUND` | Pull, durable | 64 | Long-tail consumer; falls behind without affecting attachment or AI path. Archives raw inbound. |
| `ai-consumer-enriched` | `MESSAGES_INBOUND_ENRICHED` | Pull, durable | **1** | Single-flight. Per-thread ordering enforced globally for now; partition by subject when throughput demands. |
| `sender-pool` | `MESSAGES_OUTBOUND` | Pull, durable | **32** | Workqueue drain. One pool per channel adapter eventually. |

*Deprecated:* `ai-consumer` on `MESSAGES_INBOUND` — remove after successful
enriched-stream cutover via `nats consumer rm MESSAGES_INBOUND ai-consumer`.

Adding a new consumer (analytics, training-data tap, audit) is a config
change, not an engineering task. That's the *whole point* of the decoupled bus.

---

## 7. Idempotency, ordering, rate limits

### Idempotency

Two layers, defense in depth:

1. **NATS publish dedup** via `Nats-Msg-Id` header inside the stream's
   `duplicate_window` (2 min). Catches retries from the gateway itself.
2. **Postgres unique constraint** on `(account_id, source_message_id)`.
   Authoritative. Catches channel-level redeliveries past the dedup window.
   `account_id` (not `channel_type`) so one tenant running two installs of
   the same platform — e.g. two Slack workspaces — gets disjoint dedup keys.

The gateway's loop is: signature verify → upsert → publish → ack. If
the upsert returns "already exists," we silently 200 the channel and
skip the publish.

### Ordering

The bus does not order across subjects. We enforce ordering by:

- **Per-stream**: NATS gives FIFO within a stream
- **Per-conversation**: `MaxAckPending=1` on `ai-consumer-enriched` makes the
  consumer effectively single-flight. Slow but correct. (Attachment-downloader
  has `MaxAckPending > 1` since it batches fetches and has no AI latency.)
- **Graduation path**: once we need throughput, partition by subject —
  one consumer per `mio.inbound_enriched.<channel_type>.<account_id>.<conversation_id>`
  shard. Documented but not built

### Rate limits

Per-`account_id` token buckets (one bucket per channel install), sized
per channel API. Lives in the gateway sender-pool, not the bus. Adapters
may override the bucket key (e.g. Slack tier-4 uses
`account_id:conversation_external_id` for per-channel fairness). Examples:

| Channel | Limit | Source |
|---|---|---|
| Zoho Cliq | 10 msg/sec/bot | Cliq REST docs |
| Slack | 1 msg/sec/channel (chat.postMessage tier 4) | Slack rate-limit docs |
| Telegram | 30 msg/sec/bot global, 1/sec/chat | Telegram Bot API |
| Discord | 5 msg/5s/channel | Discord HTTP rate limits |

Burst is fine. The bucket refills; the workqueue retries on Nak.

---

## 8. Storage tiers

Two lifetimes, two access patterns, never shared.

| Tier | Tech | Lifetime | Access pattern | Owner |
|---|---|---|---|---|
| Operational | Postgres + pgvector | hot | per-thread, low-latency, transactional | MIU |
| Bus | NATS JetStream | 7d (in) / 23h (out) | streaming, replayable | MIO |
| Archive | GCS + BigQuery external tables | indefinite (lifecycle to Coldline) | analytical, batch | MIO |

GCS partitioning: `gs://mio-messages/channel_type=<channel_type>/date=YYYY-MM-DD/`
(Hive-style for BQ external table partition discovery; `channel_type` value
is the `proto/channels.yaml` registry slug — e.g. `zoho_cliq`, `slack`).
Lifecycle: Standard → Nearline @ 30d → Coldline @ 90d. BigQuery external
tables read directly from GCS — no separate BQ sink, no double-write.

---

## 9. Deployment topology

POC target: GKE.

```mermaid
flowchart TB
  subgraph "GKE cluster (regional)"
    direction TB
    subgraph "ns: mio"
      gwd["mio-gateway<br/>Deployment, 2 replicas"]
      dld["mio-attachment-downloader<br/>Deployment, 1 replica (POC)"]
      sinkd["mio-sink-gcs<br/>Deployment, 1 replica"]
      subgraph "StatefulSet: mio-nats (3 replicas)"
        n0["nats-0<br/>zone-a · pd-balanced"]
        n1["nats-1<br/>zone-b · pd-balanced"]
        n2["nats-2<br/>zone-c · pd-balanced"]
      end
      promex[Prometheus exporter]
    end
    subgraph "ns: miu"
      aid["AI service<br/>Deployment + Hatchet workers"]
      pgd[(Postgres + pgvector<br/>StatefulSet)]
    end
  end

  ing["Cloud LB / Ingress"] --> gwd
  gwd <--> n0 & n1 & n2
  dld <--> n0 & n1 & n2
  aid <--> n0 & n1 & n2
  sinkd <--> n0 & n1 & n2
  dld --> gcs
  sinkd --> gcs
  gcs["GCS bucket<br/>(raw + attachments)<br/>via Workload Identity"]
  promex --> mon["Prometheus / Grafana"]
```

Stack rules carried over:

- Helm charts in-repo under `deploy/charts/{mio-nats,mio-gateway,mio-sink-gcs}`
- Only K8s primitives — no Cloud Pub/Sub, no Cloud Run; cloud-agnostic by construction
- Workload Identity for GCS auth; no service-account JSON files
- Single regional cluster for POC; multi-region is future work

---

## 10. Observability

Everything emits OpenTelemetry traces and Prometheus metrics. Logs are
structured JSON via `slog` (Go) and `structlog` (Python).

### Trace correlation

Trace context propagates: channel webhook → gateway → bus header
(`mio-trace-id`) → AI consumer → outbound publish → sender pool →
channel API. A single user message produces one root trace covering
the whole loop.

### Key metrics

Label discipline (cross-phase invariant): the only allowed application-metric
labels are `channel_type`, `direction`, `outcome`. Adding `account_id`,
`tenant_id`, `conversation_id`, `message_id`, or any free-form string is
forbidden — they are cardinality bombs. Phase-specific bounded extras
(`http_status` bucketed `2xx/4xx/429/5xx/network`, `reason` bounded enum)
are acceptable; see P5.

| Metric | Owner | Why |
|---|---|---|
| `mio_gateway_inbound_latency_seconds{channel_type,direction,outcome}` | gateway | p99 < 500ms SLO |
| `mio_gateway_outbound_send_total{channel_type,direction,outcome}` | gateway | rate-limit hits, channel errors |
| `mio_jetstream_consumer_lag{stream,consumer}` | NATS exporter | AI consumer falling behind |
| `mio_sink_gcs_bytes_written_total{channel_type}` | sink-gcs | archive throughput |
| `mio_idempotency_dedup_total{channel_type}` | gateway | redelivery rate sanity |

---

## 11. Non-goals (explicit)

- **No UI in MIO.** Workspace OAuth onboarding lives in MIU's admin console.
- **No staging cluster.** Solo dev scale; feature flags + fast rollback.
- **No multiple channel adapters on day one.** Cliq POC first, generalize after.
- **No AI agent code in this repo.** Agents live in MIU.
- **No dedicated BigQuery sink.** GCS + external tables.
- **No managed cloud bus.** NATS JetStream — cloud-agnostic by construction.

---

## 12. Open questions

- Per-thread ordering on enriched stream: stay global `MaxAckPending=1` or shard-by-subject? Decide when first throughput regression appears. (Attachment-downloader's `MaxAckPending > 1` doesn't need ordering guarantees.)
- Edit semantics across channels: Slack and Cliq both support edits with the original `channel_message_id`; Telegram supports `edit_message_text`; Discord requires the original message be from the same bot. The `SendCommand.edit_of` field needs a per-channel resolver — design at P5, not now.
- Dead-letter strategy: separate `MESSAGES_DLQ` stream vs in-place `terminated` flag? Defer until we hit a real channel-permanent failure in the wild.
- Attachment backend portability: S3, Azure Blob, Backblaze B2? Factory pattern ready; plug in a new Storage impl. Defer multi-backend support until operational need arises.
