#!/usr/bin/env bash
# Integration test for bq-loader.
#
# Provisions an ephemeral sandbox dataset, uploads good + bad NDJSON fixtures
# to a CI-scoped GCS path, runs load.sh, and asserts:
#   1. Good rows land in <dataset>.messages.
#   2. Bad row lands in <dataset>.messages_errors with error_message='missing_account_id'.
#   3. _source_object is non-NULL on every native row.
#
# Required env vars:
#   TEST_PROJECT_ID   GCP project for the sandbox (use a non-prod project).
#   TEST_BUCKET       GCS bucket the test SA can write to.
#
# Optional:
#   TEST_DATASET      defaults to: bq_loader_citest_${RANDOM}
#   TEST_PREFIX       defaults to: ci/bq-loader/${RUN_ID}/
#
# Auth: caller must have ADC for an SA with bigquery.dataEditor on the
# sandbox dataset + storage.objectAdmin on the bucket prefix.
#
# Cleanup happens unconditionally on exit (even on test failure).

set -euo pipefail

: "${TEST_PROJECT_ID:?TEST_PROJECT_ID is required}"
: "${TEST_BUCKET:?TEST_BUCKET is required}"
: "${TEST_DATASET:=bq_loader_citest_${RANDOM}}"
: "${TEST_PREFIX:=ci/bq-loader/${GITHUB_RUN_ID:-local-$$}/}"

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
FIXTURES="${REPO_ROOT}/tools/bq-loader/test/fixtures"

# Window matches the timestamps in fixtures (2026-05-10T11:42:*).
WINDOW_START="2026-05-10T11:00:00Z"
WINDOW_END="2026-05-10T12:00:00Z"

cleanup() {
  echo "--- cleanup ---"
  bq --project_id="${TEST_PROJECT_ID}" rm -r -f -d "${TEST_PROJECT_ID}:${TEST_DATASET}" || true
  gsutil -m rm -r "gs://${TEST_BUCKET}/${TEST_PREFIX}" || true
}
trap cleanup EXIT

echo "--- provision sandbox dataset ${TEST_DATASET} ---"
bq --project_id="${TEST_PROJECT_ID}" mk --dataset --location=US "${TEST_PROJECT_ID}:${TEST_DATASET}"

# Apply DDL: native + errors only (loader writes to both).
envsubst < "${REPO_ROOT}/sink-gcs/sql/messages_errors.sql" \
  | PROJECT_ID="${TEST_PROJECT_ID}" DATASET="${TEST_DATASET}" envsubst \
  | bq --project_id="${TEST_PROJECT_ID}" query --use_legacy_sql=false

bq --project_id="${TEST_PROJECT_ID}" mk --table \
  --schema="${REPO_ROOT}/sink-gcs/sql/messages_schema.json" \
  --time_partitioning_field=received_at \
  --clustering_fields=channel_type,account_id,conversation_id \
  "${TEST_PROJECT_ID}:${TEST_DATASET}.messages"

echo "--- upload fixtures to gs://${TEST_BUCKET}/${TEST_PREFIX} ---"
PARTITION_PATH="channel_type=zoho_cliq/date=2026-05-10"
gsutil cp "${FIXTURES}/good.ndjson" "gs://${TEST_BUCKET}/${TEST_PREFIX}${PARTITION_PATH}/good.ndjson"
gsutil cp "${FIXTURES}/bad.ndjson"  "gs://${TEST_BUCKET}/${TEST_PREFIX}${PARTITION_PATH}/bad.ndjson"

echo "--- run loader ---"
PROJECT_ID="${TEST_PROJECT_ID}" \
DATASET="${TEST_DATASET}" \
BUCKET="${TEST_BUCKET}" \
PREFIX="${TEST_PREFIX}" \
WINDOW_START="${WINDOW_START}" \
WINDOW_END="${WINDOW_END}" \
MODE="ci-test" \
"${REPO_ROOT}/tools/bq-loader/load.sh"

echo "--- assert results ---"

NATIVE_COUNT="$(bq --project_id="${TEST_PROJECT_ID}" --format=csv --quiet \
  query --use_legacy_sql=false --nouse_cache \
  "SELECT COUNT(*) FROM \`${TEST_PROJECT_ID}.${TEST_DATASET}.messages\` WHERE DATE(received_at) = DATE '2026-05-10'" \
  | tail -n1)"
[ "${NATIVE_COUNT}" = "1" ] || { echo "FAIL: expected 1 native row, got ${NATIVE_COUNT}"; exit 1; }

ERROR_COUNT="$(bq --project_id="${TEST_PROJECT_ID}" --format=csv --quiet \
  query --use_legacy_sql=false --nouse_cache \
  "SELECT COUNT(*) FROM \`${TEST_PROJECT_ID}.${TEST_DATASET}.messages_errors\` WHERE error_message = 'missing_account_id'" \
  | tail -n1)"
[ "${ERROR_COUNT}" = "1" ] || { echo "FAIL: expected 1 quarantined row, got ${ERROR_COUNT}"; exit 1; }

NULL_SOURCE_COUNT="$(bq --project_id="${TEST_PROJECT_ID}" --format=csv --quiet \
  query --use_legacy_sql=false --nouse_cache \
  "SELECT COUNT(*) FROM \`${TEST_PROJECT_ID}.${TEST_DATASET}.messages\` WHERE _source_object IS NULL AND DATE(received_at) = DATE '2026-05-10'" \
  | tail -n1)"
[ "${NULL_SOURCE_COUNT}" = "0" ] || { echo "FAIL: ${NULL_SOURCE_COUNT} rows missing _source_object"; exit 1; }

echo "--- PASS ---"
