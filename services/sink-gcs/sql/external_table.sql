-- BigQuery External Table over GCS NDJSON for ad-hoc / ops queries.
--
-- Hive partitioning auto-discovers (channel_type, date) from the GCS path:
--   gs://${BUCKET}/${PREFIX}channel_type=<slug>/date=YYYY-MM-DD/*.ndjson
--
-- Target name is `messages_external` — the canonical name `messages` is
-- reserved for the native, partitioned, hourly-loaded table (see
-- messages_native.sql). Analysts should query `messages_dedup` (view), not
-- this table — duplicates are present here by design.
--
-- Schema is explicit (was autodetect=true). NDJSON keys are snake_case after
-- sink-gcs ndjson.go was flipped to UseProtoNames:true (Phase 0 Q7 decision).
--
-- Apply:
--   envsubst < external_table.sql | bq query --use_legacy_sql=false
--
-- Cutover guidance (read sink-gcs/sql/README.md "Cutover from autodetect stub"
-- BEFORE running any drop). The destructive command lives in the README, not
-- here, so it can never be accidentally executed by `bq query < this-file`.

CREATE OR REPLACE EXTERNAL TABLE `${PROJECT_ID}.${DATASET}.messages_external` (
  id                       STRING,
  schema_version           INT64,
  tenant_id                STRING,
  account_id               STRING,
  channel_type             STRING,
  conversation_id          STRING,
  conversation_external_id STRING,
  conversation_kind        STRING,
  parent_conversation_id   STRING,
  source_message_id        STRING,
  thread_root_message_id   STRING,
  sender                   STRUCT<
    external_id  STRING,
    display_name STRING,
    peer_kind    STRING,
    is_bot       BOOL
  >,
  text                     STRING,
  attachments              ARRAY<STRUCT<
    kind           STRING,
    url            STRING,
    mime           STRING,
    bytes          INT64,
    filename       STRING,
    storage_key    STRING,
    content_sha256 STRING,
    error_code     STRING
  >>,
  received_at              TIMESTAMP,
  attributes               JSON
)
WITH PARTITION COLUMNS (
  channel_type STRING,
  date         DATE
)
OPTIONS (
  format = 'NEWLINE_DELIMITED_JSON',
  uris = ['gs://${BUCKET}/${PREFIX}channel_type=*/date=*/*.ndjson'],
  hive_partition_uri_prefix = 'gs://${BUCKET}/${PREFIX}',
  require_hive_partition_filter = false,
  description = 'Hive-partitioned external view over mio NDJSON. Duplicates present — prefer messages_dedup view.'
);
