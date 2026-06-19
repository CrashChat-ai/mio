---
title: Consumer Contract v1
description: The authoritative, frozen specification for downstream consumers of the MIO gateway.
---

# MIO Consumer Contract v1

**Status:** frozen | **Effective:** 2026-06-12 | **Schema version:** 1

This document is the authoritative specification for downstream consumers of the MIO gateway.
Changes to any element marked **frozen** require a 1-quarter deprecation notice and a
`schema_version` bump with a dual-write migration plan (see [Change Policy](#change-policy)).

---

## Streams

| Name | Subject filter | Retention | Purpose |
|---|---|---|---|
| `MESSAGES_INBOUND` | `mio.inbound.>` | ≥7 days | Raw inbound from gateway; source of truth for media-vault and sink |
| `MESSAGES_INBOUND_ENRICHED` | `mio.inbound_enriched.>` | ≥7 days | Attachment-enriched envelope; **primary consumer stream** |
| `MESSAGES_OUTBOUND` | `mio.outbound.>` | 24 hours (work-queue) | Outbound send commands consumed by the sender pool |

All three stream names and subject filters are **frozen**.

---

## Subject Grammar

```
mio.inbound_enriched.<channel_type>.<account_id>.<conversation_id>
```

- Exactly **4 tokens** after the `mio.inbound_enriched` prefix.
- `<channel_type>` — lowercase underscore string from `proto/channels.yaml` (e.g. `zoho_cliq`).
  Never an integer enum; new channels are added to the YAML registry, not the proto.
- `<account_id>` — mio UUID v7 of the channel install (the `accounts` table row).
- `<conversation_id>` — mio UUID v7 of the conversation.

The grammar is **frozen**. A filter of `mio.inbound_enriched.>` matches all accounts and channel types.

---

## Durables (frozen)

Both durables live on `MESSAGES_INBOUND_ENRICHED`. Renaming or deleting either will replay messages.

| Durable name | Consumer | Pull batch |
|---|---|---|
| `channel-pulse` | channel-pulse FastAPI app | 100 |
| `ai-consumer-enriched` | echo-consumer / AI lab | not specified |

---

## `mio.v1.Message` — Wire Fields

Source: `proto/mio/v1/message.proto`.

All field numbers and names below are **frozen** (WIRE_JSON breaking-change guard via `buf breaking`).

| # | Name | Type | Notes |
|---|---|---|---|
| 1 | `id` | string | mio UUID v7 |
| 2 | `schema_version` | int32 | Always `1`; consumers MUST assert and skip/log on mismatch |
| 3 | `tenant_id` | string | Top-tier isolation scope |
| 4 | `account_id` | string | mio UUID of the channel install |
| 5 | `channel_type` | string | Value from `proto/channels.yaml`; never an integer |
| 6 | `conversation_id` | string | mio UUID |
| 7 | `conversation_external_id` | string | Platform-side opaque id |
| 8 | `conversation_kind` | ConversationKind | Enum; see order below |
| 9 | `parent_conversation_id` | string | Empty unless thread / forum-post |
| 10 | `source_message_id` | string | Platform message id |
| 11 | `thread_root_message_id` | string | Empty if not in a thread |
| 12 | `sender` | Sender | `external_id`, `display_name`, `is_bot` |
| 13 | `text` | string | Plain-text body |
| 14 | `attachments` | repeated Attachment | `kind`, `mime`, `filename`, `storage_key`, `error_code` |
| 15 | `received_at` | Timestamp | UTC wall-clock of gateway receipt |
| 16 | `attributes` | map<string,string> | Channel-specific escape hatch; see promotion rule |
| 17 | `relation` | MessageRelation | `kind`, `target_message_id`, `reaction_emoji`; empty for plain messages |
| 18 | *(reserved)* | — | Reserved for `is_summary`; do not reclaim without a migration plan |

---

## `ConversationKind` Enum — Order Frozen

Source: `proto/mio/v1/enums.proto`.

```
CONVERSATION_KIND_UNSPECIFIED  = 0
CONVERSATION_KIND_DM           = 1
CONVERSATION_KIND_GROUP_DM     = 2
CONVERSATION_KIND_CHANNEL_PUBLIC  = 3
CONVERSATION_KIND_CHANNEL_PRIVATE = 4
CONVERSATION_KIND_THREAD       = 5
CONVERSATION_KIND_FORUM_POST   = 6
CONVERSATION_KIND_BROADCAST    = 7
```

On the wire the field carries the integer; consumers that map integers to strings MUST preserve
this order. Adding a new variant at the end is additive and safe.

---

## Idempotency Key

**`(account_id, source_message_id)`** is the unique key for deduplication.

`(channel_type, source_message_id)` is **not** the key — it breaks when a tenant runs two
workspaces of the same platform under different accounts.

---

## `schema_version` Contract

- The gateway publishes `schema_version = 1` on every `Message`.
- Consumers MUST assert `schema_version == 1` at decode time.
- On mismatch: log an error and skip the message — do **not** crash the consumer process.
- A future `schema_version = 2` requires a dual-write migration plan before the gateway
  hard-rejects the old version.

---

## `attributes` Escape Hatch

`attributes` is a `map<string,string>` for channel-specific data that has not yet been
promoted to a typed field.

**Promotion rule:** any key read by ≥2 consumers OR written by ≥2 channels gets promoted
to a named typed field in the proto (additive, not a replacement).

**Active attributes (as of v1):**

| Key | Set by | Read by | Notes |
|---|---|---|---|
| `cliq_channel_name` | zoho_cliq adapter | channel-pulse | Zoho Cliq API slug for the channel (e.g. `ducdev`); used by outbound sender to address the channel |
| `conversation_display_name` | adapters (generic) | any consumer | Human-readable conversation name; preferred over channel-specific keys when the value is channel-agnostic; constant `channels.AttrConversationDisplayName` |
| `cliq_org_id` | zoho_cliq adapter | gateway (multi-account routing) | Cliq workspace / organisation ID; used by the inbound router to resolve the correct account when multiple Cliq installs share one webhook endpoint |

Consumers MUST treat unknown attribute keys as advisory and ignore them — new keys are additive
and will not be announced.

---

## Retention

- `MESSAGES_INBOUND` and `MESSAGES_INBOUND_ENRICHED`: **≥7 days** (media-vault SLA).
- `MESSAGES_OUTBOUND`: 24 hours (work-queue; messages are acked on delivery).

Consumers that replay from the start of the stream MUST tolerate 7 days of history.

---

## Change Policy

1. **Additive-only** — new fields, new enum variants at the end, new `attributes` keys are
   permitted without a deprecation window.
2. **Deprecation notice** — any removal or semantic change to a frozen element requires a
   **1-quarter notice** communicated in the MIO changelog and this document.
3. **`schema_version` bump path** — incrementing `schema_version` (e.g. 1→2) requires:
   a. A dual-write period where the gateway publishes both versions in parallel.
   b. All known consumers updated and confirmed before the old version is retired.
   c. A new section in this document documenting the migration window.
4. **`proto/channels.yaml` additions** — adding a new `channel_type` string is additive;
   consumers that see an unknown `channel_type` MUST NOT fail — treat it as opaque.
