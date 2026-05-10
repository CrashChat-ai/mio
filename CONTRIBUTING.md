# Contributing to MIO

Two rules that are enforced at code review time for all PRs touching proto definitions or gateway/SDK code.

---

## 1. `attributes` promotion rule

The `attributes map<string,string>` field on `Message` and `SendCommand` is an escape hatch for channel-specific data that does not yet warrant a typed proto field. The rule governing when to promote:

**Promotion threshold:** Any `attributes` key that is read by ≥ 2 consumers **or** written by ≥ 2 channel adapters must be promoted to a named, typed proto field (with a backfill migration).

**Until promotion:** Use named constants, never inline string literals.

```go
// Good — constant defined once, referenced everywhere
const AttrZohoCliqWorkspace = "zoho_cliq_workspace"
const AttrSlackTS           = "slack_ts"

msg.Attributes[AttrZohoCliqWorkspace] = workspaceID
```

```python
# Good
ATTR_ZOHO_CLIQ_WORKSPACE = "zoho_cliq_workspace"
msg.attributes[ATTR_ZOHO_CLIQ_WORKSPACE] = workspace_id
```

```go
// Bad — string literal scattered across files
msg.Attributes["zoho_cliq_workspace"] = workspaceID
```

**Why:** Attributes are stored verbatim in the GCS archive and indexed in BigQuery external tables. A key rename after two consumers exist requires a dual-read migration across both the archive and live consumers (same class of scar as goclaw migration 58). Typed fields + WIRE_JSON breaking checks prevent silent renames.

**Periodic audit:** At each outbound adapter phase (P5+) review the `attributes` map. Any key crossing the threshold gets promoted in the same PR as the feature that crosses it.

---

## 2. `channel_type` registry rule

`channel_type` on the wire is always the `name` field from `proto/channels.yaml`. This file is the single source of truth.

**Adding a new channel:**
1. Add an entry to `proto/channels.yaml` with `status: planned` (or `active` if the adapter lands in the same PR).
2. Add the adapter package under `gateway/internal/channels/<slug>/`.
3. No proto regeneration, no SDK redeploy — the field is a string, not an enum.

**Renaming a channel slug:**
1. Add the old slug to `deprecated_aliases` in `proto/channels.yaml`, mapping it to the new slug.
2. The SDK will accept both names during the migration window.
3. **Never update a `name` in-place.** In-place renames break the GCS archive partition path and any BigQuery external table that filters on `channel_type` — the same class of data loss as goclaw migration 58.

**CI gate:** PRs that introduce a `channel_type` string in gateway or SDK code that is not listed in `proto/channels.yaml` (or its `deprecated_aliases`) will be rejected by the channel-type lint check (added at P3).

---

## Proto field number policy

- Fields 1–15 use single-byte tags (hot path; use for frequently-set fields).
- Never reuse a field number. Removing a field means adding `reserved <N>;` to the message.
- Current reserved fields:
  - `Message` field 17: reserved for `MessageRelation` (P5 outbound edit/reaction linkage).
  - `Message` field 18: reserved for `is_summary` (compaction flag, future).
  - `SendCommand` field 15: reserved for `MessageRelation`.
