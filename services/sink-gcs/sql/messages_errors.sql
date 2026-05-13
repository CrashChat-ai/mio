-- Quarantine table for rows the bq-loader couldn't validate.
--
-- Loader writes here when validate.sql predicates fail (invalid timestamp,
-- missing dedup key, malformed attributes JSON, unknown conversation_kind).
-- Rows here are NOT in raw_mio.messages — investigate, then either fix the
-- source NDJSON shape or accept the loss.
--
-- 30-day partition expiration on this table specifically (set via
-- partition_expiration_days option). Bad rows are debugging signal, not
-- long-term audit.
--
-- Apply:
--   envsubst < messages_errors.sql | bq query --use_legacy_sql=false

CREATE TABLE IF NOT EXISTS `${PROJECT_ID}.${DATASET}.messages_errors` (
  _source_object STRING,
  _ingest_at     TIMESTAMP,
  error_message  STRING,
  raw_payload    STRING
)
PARTITION BY DATE(_ingest_at)
OPTIONS (
  partition_expiration_days = 30,
  description = 'Quarantine for rows that failed bq-loader validation. 30-day retention.'
);
