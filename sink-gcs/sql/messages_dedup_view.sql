-- Analyst-facing dedup view over raw_mio.messages.
--
-- Contract:
--   * Unique on (account_id, source_message_id) — keeps the most-recent row by
--     (received_at DESC, _ingest_at DESC) when duplicates exist.
--   * Inherits require_partition_filter from the underlying table — analysts
--     MUST add WHERE DATE(received_at) BETWEEN x AND y.
--   * Read this, not raw_mio.messages, for canonical analytics.
--
-- Apply:
--   envsubst < messages_dedup_view.sql | bq query --use_legacy_sql=false

CREATE OR REPLACE VIEW `${PROJECT_ID}.${DATASET}.messages_dedup` AS
SELECT * EXCEPT(_rn)
FROM (
  SELECT
    *,
    ROW_NUMBER() OVER (
      PARTITION BY account_id, source_message_id
      ORDER BY received_at DESC, _ingest_at DESC
    ) AS _rn
  FROM `${PROJECT_ID}.${DATASET}.messages`
)
WHERE _rn = 1;
