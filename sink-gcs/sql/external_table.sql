-- BigQuery External Table DDL for MIO archive (GCS NDJSON).
--
-- Hive-style partitioning: BQ auto-discovers channel_type (STRING) and
-- date (DATE) from the GCS path structure:
--   gs://<bucket>/channel_type=<slug>/date=YYYY-MM-DD/*.ndjson
--
-- Usage:
--   Replace ${PROJECT_ID}, ${DATASET}, ${BUCKET} with real values, then:
--   bq query --use_legacy_sql=false < external_table.sql
--
-- Dedup view (at-least-once delivery; post-hoc dedup in BQ):
--   SELECT * FROM `${PROJECT_ID}.${DATASET}.messages`
--   QUALIFY ROW_NUMBER() OVER (PARTITION BY id ORDER BY received_at DESC) = 1;

CREATE OR REPLACE EXTERNAL TABLE `${PROJECT_ID}.${DATASET}.messages`
OPTIONS (
  format = 'NEWLINE_DELIMITED_JSON',
  uris = ['gs://${BUCKET}/channel_type=*/date=*/*.ndjson'],
  hive_partition_uri_prefix = 'gs://${BUCKET}/',
  require_hive_partition_filter = false,
  autodetect = true
);
