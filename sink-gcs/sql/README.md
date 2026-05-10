# sink-gcs/sql — BigQuery schema contract + reference DDL

This directory is the **schema authority** for the mio lakehouse. mio is the
producer; consumers (e.g. `ab-spectrum/infra/services/bq-mio`) read the canonical
schema and DDL from here.

## Files

| File | Role | Notes |
|---|---|---|
| `messages_schema.json` | **Canonical schema** (typed, snake_case) | Source of truth. Consumers vendor a copy. The `check-proto-drift.sh` CI guard ensures this matches `proto/mio/v1/*.proto`. |
| `messages_native.sql` | Reference DDL — partitioned + clustered native table | Apply once per environment by the operator. |
| `messages_dedup_view.sql` | Reference DDL — analyst-facing dedup view | Use **this**, not `messages` directly. |
| `messages_errors.sql` | Reference DDL — quarantine table (30d retention) | Loader writes here when validation fails. |
| `external_table.sql` | Reference DDL — Hive-partitioned external view | Ops/exploration entry point; duplicates by design. |
| `check-proto-drift.sh` | CI guard | Fails the PR if proto fields outpace `messages_schema.json`. |

All DDL files use `${PROJECT_ID}`, `${DATASET}`, `${BUCKET}`, `${PREFIX}`
placeholders — render with `envsubst` before piping into `bq query`.

## Where the loader lives

The hourly Cloud Run Job that materialises `raw_mio.messages` from GCS NDJSON
is **not in this repo** — see [`ab-spectrum/infra/services/bq-mio/`](https://github.com/AB-Spectrum/infra)
(consumer-side concern). mio publishes the contract; the consumer builds the
pipeline. The loader vendors `messages_schema.json` and verifies it against
this repo's `main` in its own CI.

## Cutover from autodetect stub (run **once**, before Apply step 1)

The previous DDL created an external table at the canonical name
`${DATASET}.messages` (autodetect=true). The new layout reserves that name
for the native table; the external view is renamed to `messages_external`.

If — and **only if** — `bq show ${PROJECT_ID}:${DATASET}.messages` reports
`Type: EXTERNAL`, drop it before running Apply step 1:

```bash
# Verify it's the old EXTERNAL stub, NOT a native table that already holds data.
bq show --format=prettyjson ${PROJECT_ID}:${DATASET}.messages | grep '"type"'
# Expect: "type": "EXTERNAL". If "TABLE", STOP — that's the new native table.

bq rm -t -f ${PROJECT_ID}:${DATASET}.messages
```

Skip this entirely on a fresh dataset.

## Apply order (per environment)

```bash
export PROJECT_ID=dp-prod-7e26   # or dp-dev's project id
export DATASET=raw_mio
export BUCKET=ab-spectrum-sensitive-prod
export PREFIX=mio/

# 1. native table (loader target)
bq mk --table \
  --schema=messages_schema.json \
  --time_partitioning_field=received_at \
  --time_partitioning_type=DAY \
  --clustering_fields=channel_type,account_id,conversation_id \
  --require_partition_filter \
  ${PROJECT_ID}:${DATASET}.messages

# 2. errors table (loader quarantine target)
envsubst < messages_errors.sql | bq query --use_legacy_sql=false

# 3. external table (ad-hoc analyst entry point)
envsubst < external_table.sql | bq query --use_legacy_sql=false

# 4. dedup view (depends on messages existing)
envsubst < messages_dedup_view.sql | bq query --use_legacy_sql=false
```

## Dedup recipe

`messages_dedup` keeps the most-recent row per `(account_id, source_message_id)`,
ordered by `(received_at DESC, _ingest_at DESC)`. This is the at-least-once
delivery contract from sink-gcs — duplicates are expected in the lake-of-record
and resolved at read time.

## Schema-evolution rule

**Proto change → DDL change in the same PR.** Adding a field in
`proto/mio/v1/*.proto` without updating `messages_schema.json` silently
NULLs the new column on every consumer's `bq load`. `check-proto-drift.sh`
fails this repo's CI on drift.

To add a field:

1. Add it to the `.proto` file.
2. Add it to `messages_schema.json` (matching name, type, mode).
3. Add it to `messages_native.sql` and `external_table.sql` columns lists.
4. After mio's PR merges, downstream consumers `sync-schema.sh` against
   the new `main` and bump their loader image. Their CI verifies the
   vendored copy matches mio.
5. For `bq load` to accept the new column on existing partitions, the
   consumer passes `--schema_update_option=ALLOW_FIELD_ADDITION`.

## NDJSON wire format contract

Per Phase 0 Q7, `sink-gcs/internal/encode/ndjson.go` emits **snake_case** field
names (`UseProtoNames: true`) so they match this schema 1:1. Do not flip back
to camelCase without re-authoring the schema.

## Monitoring + alerts

Loader observability and Cloud Monitoring alert policies live with the
loader (`ab-spectrum/infra/services/bq-mio/README.md` + the `bigquery-loader`
terragrunt module). Notification channel: existing `GCHAT_WEBHOOK_URL`.
