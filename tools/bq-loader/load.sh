#!/usr/bin/env bash
# Hourly bq-loader entrypoint.
#
# Reads NDJSON from gs://${BUCKET}/${PREFIX}channel_type=*/date=*/*.ndjson
# within [WINDOW_START, WINDOW_END), validates rows, quarantines bad ones into
# ${DATASET}.messages_errors, and MERGEs good rows into ${DATASET}.messages.
#
# Required env vars:
#   PROJECT_ID  GCP project that owns the dataset (e.g. dp-prod-7e26)
#   DATASET     BQ dataset (e.g. raw_mio)
#   BUCKET      GCS bucket holding NDJSON (e.g. ab-spectrum-sensitive-prod)
#   PREFIX      GCS path prefix below bucket root, including trailing slash (e.g. mio/)
#
# Optional env vars:
#   WINDOW_START  RFC3339 UTC; default = previous_full_hour - 15 min overlap
#   WINDOW_END    RFC3339 UTC; default = previous_full_hour
#   MODE          incremental | backfill (informational only — affects log tag)

set -euo pipefail

: "${PROJECT_ID:?PROJECT_ID is required}"
: "${DATASET:?DATASET is required}"
: "${BUCKET:?BUCKET is required}"
: "${PREFIX:=}"
: "${MODE:=incremental}"

# Default window: previous full hour with 15-min overlap into the prior hour.
# Hour-aligned so backfill chunks are deterministic.
if [ -z "${WINDOW_END:-}" ]; then
  WINDOW_END="$(date -u -d "$(date -u +%Y-%m-%dT%H:00:00) -1 hour" +%Y-%m-%dT%H:%M:%SZ)"
fi
if [ -z "${WINDOW_START:-}" ]; then
  WINDOW_START="$(date -u -d "${WINDOW_END} -1 hour -15 minutes" +%Y-%m-%dT%H:%M:%SZ)"
fi

WINDOW_START_DATE="${WINDOW_START%%T*}"
WINDOW_END_DATE="${WINDOW_END%%T*}"

# Unique staging table name (timestamp + random suffix avoids collisions across
# concurrent backfill runs targeting different windows).
STAMP="$(date -u +%Y%m%d%H%M%S)_$$"
STAGING="${DATASET}._staging_messages_${STAMP}_ext"

log_struct() {
  # Cloud Logging picks up structured JSON on stdout when run on Cloud Run.
  printf '{"severity":"%s","loader_run_status":"%s","mode":"%s","window_start":"%s","window_end":"%s","staging":"%s","message":"%s"}\n' \
    "${1}" "${2}" "${MODE}" "${WINDOW_START}" "${WINDOW_END}" "${STAGING}" "${3:-}"
}

# Always drop the staging table on exit, even on partial failure.
cleanup() {
  bq --project_id="${PROJECT_ID}" rm -t -f "${PROJECT_ID}:${STAGING}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

log_struct INFO start "begin"

# ---------------------------------------------------------------------------
# 1. Create EXTERNAL staging table over the windowed NDJSON URIs.
# ---------------------------------------------------------------------------
DEF_FILE="$(mktemp)"
cat > "${DEF_FILE}" <<JSON
{
  "sourceFormat": "NEWLINE_DELIMITED_JSON",
  "sourceUris": ["gs://${BUCKET}/${PREFIX}channel_type=*/date=*/*.ndjson"],
  "hivePartitioningOptions": {
    "mode": "AUTO",
    "sourceUriPrefix": "gs://${BUCKET}/${PREFIX}",
    "requirePartitionFilter": false
  },
  "schema": { "fields": $(cat /loader/sql/messages_schema.json) }
}
JSON

bq --project_id="${PROJECT_ID}" mk \
  --external_table_definition="${DEF_FILE}" \
  "${PROJECT_ID}:${STAGING}"
rm -f "${DEF_FILE}"

# ---------------------------------------------------------------------------
# 2. Quarantine invalid rows → messages_errors.
# 3. MERGE valid rows → messages.
# ---------------------------------------------------------------------------
bq --project_id="${PROJECT_ID}" query \
  --use_legacy_sql=false \
  --parameter="window_start:TIMESTAMP:${WINDOW_START}" \
  --parameter="window_end:TIMESTAMP:${WINDOW_END}" \
  --parameter="window_start_date:DATE:${WINDOW_START_DATE}" \
  --parameter="window_end_date:DATE:${WINDOW_END_DATE}" \
  --parameter="staging:STRING:${PROJECT_ID}.${STAGING}" \
  --parameter="dataset:STRING:${PROJECT_ID}.${DATASET}" \
  "$(envsubst < /loader/sql/validate.sql)"

bq --project_id="${PROJECT_ID}" query \
  --use_legacy_sql=false \
  --parameter="window_start:TIMESTAMP:${WINDOW_START}" \
  --parameter="window_end:TIMESTAMP:${WINDOW_END}" \
  --parameter="window_start_date:DATE:${WINDOW_START_DATE}" \
  --parameter="window_end_date:DATE:${WINDOW_END_DATE}" \
  "$(PROJECT_ID="${PROJECT_ID}" DATASET="${DATASET}" STAGING="${STAGING}" \
       envsubst < /loader/sql/merge.sql)"

log_struct INFO success "complete"
