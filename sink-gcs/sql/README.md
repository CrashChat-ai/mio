# sink-gcs/sql — BigQuery DDL for the mio lakehouse

DDL for the `raw_mio` BigQuery dataset that materialises mio chat data from
the GCS NDJSON sink. See `plans/260510-1102-bq-sink-lakehouse/` for the full
plan and decisions.

## Files

| File | Type | Applied to | Purpose |
|---|---|---|---|
| `messages_schema.json` | BQ JSON schema | dataset | Canonical schema (typed, snake_case). Used by `bq mk --schema=` and as the source of truth for the schema-drift CI check. |
| `messages_native.sql` | DDL | dataset | Native partitioned + clustered table. Loader writes here. |
| `messages_dedup_view.sql` | DDL | dataset | Analyst-facing dedup view. Use **this**, not `messages` directly. |
| `messages_errors.sql` | DDL | dataset | Quarantine for rows the loader couldn't validate. |
| `external_table.sql` | DDL | dataset | Hive-partitioned external view over GCS — ops/exploration only. |

All DDL files use `${PROJECT_ID}`, `${DATASET}`, `${BUCKET}`, `${PREFIX}`
placeholders — render with `envsubst` before piping into `bq query`.

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
`proto/mio/v1/*.proto` without updating `messages_schema.json` will silently
NULL the new column on `bq load`. The `tools/bq-loader/ci/check-schema-drift.sh`
CI job (Phase 6) fails the PR on drift.

To add a field:

1. Add it to the `.proto` file.
2. Add it to `messages_schema.json` (matching name, type, mode).
3. Add it to `messages_native.sql` and `external_table.sql` columns lists.
4. The loader inherits the change automatically — `merge.sql` uses `INSERT ROW`
   over `*` from the staging select.
5. For `bq load` to accept the new column on existing partitions, pass
   `--schema_update_option=ALLOW_FIELD_ADDITION` (already wired in
   `tools/bq-loader/load.sh`).

## NDJSON wire format contract

Per Phase 0 Q7, `sink-gcs/internal/encode/ndjson.go` emits **snake_case** field
names (`UseProtoNames: true`) so they match this schema 1:1. Do not flip back
to camelCase without re-authoring the schema.

## Monitoring + alerts

See `tools/bq-loader/README.md` for loader observability. Phase 6 wires Cloud
Monitoring alerts on:

- Cloud Run Job execution failure in last 75 min.
- No success log entry in last 90 min (catches scheduler-not-firing).

Notification channel: existing `GCHAT_WEBHOOK_URL` (Google Chat) — same channel
the rest of the platform monitoring uses.
