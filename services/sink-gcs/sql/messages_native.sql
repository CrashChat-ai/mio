-- Native partitioned + clustered table for mio chat messages.
--
-- Populated hourly by the bq-loader Cloud Run Job (tools/bq-loader) which
-- runs `bq load` → `MERGE` against this table. Authoritative source for
-- analyst queries via the messages_dedup view.
--
-- Apply:
--   bq mk --table \
--     --schema=sink-gcs/sql/messages_schema.json \
--     --time_partitioning_field=received_at \
--     --time_partitioning_type=DAY \
--     --clustering_fields=channel_type,account_id,conversation_id \
--     --require_partition_filter \
--     --description="See dataset description for SLA + dedup recipe + owner." \
--     ${PROJECT_ID}:${DATASET}.messages
--
-- Or as DDL (envsubst ${PROJECT_ID}, ${DATASET}; the `OPTIONS(description=...)` is
-- intentionally short — full description lives on the dataset, not the table).

CREATE TABLE IF NOT EXISTS `${PROJECT_ID}.${DATASET}.messages` (
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
  relation                 STRUCT<
    kind                STRING,
    target_message_id   STRING,
    target_external_id  STRING,
    reaction_emoji      STRING
  >,
  received_at              TIMESTAMP,
  attributes               JSON,
  _ingest_at               TIMESTAMP,
  _source_object           STRING
)
PARTITION BY DATE(received_at)
CLUSTER BY channel_type, account_id, conversation_id
OPTIONS (
  require_partition_filter = TRUE,
  description = 'mio chat messages — hourly-loaded native table. See dataset description for SLA + dedup recipe.'
);
