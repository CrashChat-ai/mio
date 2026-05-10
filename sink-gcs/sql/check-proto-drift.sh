#!/usr/bin/env bash
# Foundation-contract guard: proto ↔ BQ schema must stay in lock-step.
#
# Compares the field set declared in proto/mio/v1/{message,attachment,sender}.proto
# against sink-gcs/sql/messages_schema.json (recursively, including nested
# RECORD fields). Fails the PR (exit 1) when proto fields outpace the BQ
# schema — silent NULL columns on bq load are the failure mode this prevents.
#
# This is a producer-side invariant. Every consumer that reads NDJSON via
# the canonical schema (e.g. ab-spectrum/infra/loaders/bq-mio) depends on
# this contract holding. The check lives next to the schema, not next to
# any specific consumer.
#
# Whitelisted (loader-side conventions, never in proto):
#   _ingest_at, _source_object
#
# Run:
#   sink-gcs/sql/check-proto-drift.sh
#
# Dependencies: bash, jq, awk, sort, comm.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCHEMA="${REPO_ROOT}/sink-gcs/sql/messages_schema.json"
PROTO_DIR="${REPO_ROOT}/proto/mio/v1"

[ -f "${SCHEMA}" ] || { echo "FAIL: ${SCHEMA} not found"; exit 1; }
[ -d "${PROTO_DIR}" ] || { echo "FAIL: ${PROTO_DIR} not found"; exit 1; }

# Loader-added columns — declared in the BQ schema but never in proto.
LOADER_FIELDS_RX='^(_ingest_at|_source_object)$'

# --- Extract proto field names (top-level Message + nested Attachment/Sender,
# regardless of nesting). Field names are lowercase_snake; enum constants are
# UPPERCASE so the [a-z_] anchor in the regex naturally filters them out. ---
proto_fields() {
  for f in message.proto attachment.proto sender.proto; do
    awk '
      /^message[[:space:]]+(Message|Sender|Attachment)[[:space:]]*\{/ { in_msg=1; next }
      in_msg && /^\}/                     { in_msg=0; next }
      in_msg && /^[[:space:]]*reserved/   { next }
      in_msg && match($0, /([a-z_][a-z0-9_]*)[[:space:]]*=[[:space:]]*[0-9]+;/) {
        s = substr($0, RSTART, RLENGTH)
        sub(/[[:space:]]*=.*/, "", s)
        print s
      }
    ' "${PROTO_DIR}/${f}"
  done | sort -u
}

# --- Extract BQ schema field names recursively (top-level + nested RECORD). -
schema_fields() {
  jq -r '.. | objects | select(.name) | .name' "${SCHEMA}" | sort -u
}

PROTO_LIST="$(proto_fields)"
SCHEMA_LIST="$(schema_fields)"

# Strip loader-only fields from the schema list before diffing.
SCHEMA_PROTO_LIST="$(printf '%s\n' "${SCHEMA_LIST}" | grep -Ev "${LOADER_FIELDS_RX}" || true)"

# Fields in proto but missing from schema → drift, fail.
MISSING_IN_SCHEMA="$(comm -23 <(printf '%s\n' "${PROTO_LIST}") <(printf '%s\n' "${SCHEMA_PROTO_LIST}"))"

# Fields in schema but missing from proto → also drift.
EXTRA_IN_SCHEMA="$(comm -13 <(printf '%s\n' "${PROTO_LIST}") <(printf '%s\n' "${SCHEMA_PROTO_LIST}"))"

EXIT=0

if [ -n "${MISSING_IN_SCHEMA}" ]; then
  echo "FAIL: proto fields missing from ${SCHEMA#${REPO_ROOT}/}:"
  echo "${MISSING_IN_SCHEMA}" | sed 's/^/  - /'
  echo
  echo "Add the missing fields to messages_schema.json AND messages_native.sql"
  echo "AND external_table.sql in the same PR. See sink-gcs/sql/README.md."
  EXIT=1
fi

if [ -n "${EXTRA_IN_SCHEMA}" ]; then
  echo "FAIL: BQ schema has fields not present in proto/mio/v1/{message,attachment,sender}.proto:"
  echo "${EXTRA_IN_SCHEMA}" | sed 's/^/  - /'
  echo
  echo "Either add the proto field or remove the column from messages_schema.json."
  echo "Loader-only columns belong to the LOADER_FIELDS_RX whitelist in this script."
  EXIT=1
fi

if [ "${EXIT}" -eq 0 ]; then
  echo "OK: $(printf '%s\n' "${SCHEMA_PROTO_LIST}" | wc -l | tr -d ' ') schema fields aligned with proto."
fi

exit ${EXIT}
