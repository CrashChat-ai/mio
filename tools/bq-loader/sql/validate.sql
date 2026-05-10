-- Idempotent quarantine pass: bad rows from staging → messages_errors.
--
-- BQ external-NDJSON reads silently null fields that fail typed parse — so a
-- NULL on a NOT-NULL-by-contract field IS the parse failure. We use that
-- signal here.
--
-- Idempotency: MERGE keyed on (T._source_object = S._source_object AND
-- T.raw_payload = S.raw_payload). Cloud Scheduler retries (or operator
-- reruns of the same window) re-execute this and re-find the same bad rows;
-- WHEN NOT MATCHED keeps them out of messages_errors a second time.
-- Partition pruning: T._ingest_at within the last 7 days bounds the scan.
-- A re-quarantine after 7 days is acceptable — `_ingest_at` represents
-- detection-time, not source-event-time.
--
-- Parameters (set by load.sh via --parameter):
--   window_start       TIMESTAMP  inclusive lower bound on received_at
--   window_end         TIMESTAMP  exclusive upper bound on received_at
--   window_start_date  DATE       used to bound Hive `date` partition pruning
--   window_end_date    DATE       same
--
-- Substitution (done in load.sh via envsubst):
--   ${PROJECT_ID}.${DATASET}        target dataset
--   ${PROJECT_ID}.${STAGING}        staging external table
--
-- Failure categories (mutually exclusive — first match wins):
--   invalid_received_at        timestamp parse failed
--   missing_account_id         NULL or empty (breaks dedup key)
--   missing_source_message_id  NULL or empty (breaks dedup key)
--   unknown_conversation_kind  enum value outside the locked set
--
-- Rows quarantined here are NOT carried into the MERGE step (merge.sql
-- mirrors the same WHERE invariants).

MERGE INTO `${PROJECT_ID}.${DATASET}.messages_errors` T
USING (
  SELECT
    _FILE_NAME AS _source_object,
    CURRENT_TIMESTAMP() AS _ingest_at,
    CASE
      WHEN received_at IS NULL                                        THEN 'invalid_received_at'
      WHEN account_id IS NULL OR LENGTH(account_id) = 0               THEN 'missing_account_id'
      WHEN source_message_id IS NULL OR LENGTH(source_message_id) = 0 THEN 'missing_source_message_id'
      WHEN conversation_kind IS NOT NULL
           AND conversation_kind NOT IN UNNEST([
             'CONVERSATION_KIND_UNSPECIFIED',
             'CONVERSATION_KIND_DM',
             'CONVERSATION_KIND_GROUP_DM',
             'CONVERSATION_KIND_CHANNEL_PUBLIC',
             'CONVERSATION_KIND_CHANNEL_PRIVATE',
             'CONVERSATION_KIND_THREAD',
             'CONVERSATION_KIND_FORUM_POST',
             'CONVERSATION_KIND_BROADCAST'
           ])                                                         THEN 'unknown_conversation_kind'
      ELSE 'parse_failure'
    END AS error_message,
    -- raw_payload is the rendered staging row (includes Hive partition cols
    -- channel_type + date), not the literal NDJSON line. Deterministic per
    -- input, so MERGE deduplication is sound.
    TO_JSON_STRING(s) AS raw_payload
  FROM `${PROJECT_ID}.${STAGING}` AS s
  WHERE date BETWEEN @window_start_date AND @window_end_date
    AND (
      received_at IS NULL
      OR account_id IS NULL OR LENGTH(account_id) = 0
      OR source_message_id IS NULL OR LENGTH(source_message_id) = 0
      OR (conversation_kind IS NOT NULL
          AND conversation_kind NOT IN UNNEST([
            'CONVERSATION_KIND_UNSPECIFIED',
            'CONVERSATION_KIND_DM',
            'CONVERSATION_KIND_GROUP_DM',
            'CONVERSATION_KIND_CHANNEL_PUBLIC',
            'CONVERSATION_KIND_CHANNEL_PRIVATE',
            'CONVERSATION_KIND_THREAD',
            'CONVERSATION_KIND_FORUM_POST',
            'CONVERSATION_KIND_BROADCAST'
          ]))
    )
) S
ON  T._source_object = S._source_object
AND T.raw_payload    = S.raw_payload
AND DATE(T._ingest_at) >= DATE_SUB(CURRENT_DATE("UTC"), INTERVAL 7 DAY)
WHEN NOT MATCHED THEN
  INSERT (_source_object, _ingest_at, error_message, raw_payload)
  VALUES (S._source_object, S._ingest_at, S.error_message, S.raw_payload);
