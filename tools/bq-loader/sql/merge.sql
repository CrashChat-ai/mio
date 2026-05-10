-- Idempotent, partition-bounded MERGE: staging → messages.
--
-- Parameters (set by load.sh via --parameter):
--   window_start       TIMESTAMP  inclusive lower bound on received_at
--   window_end         TIMESTAMP  exclusive upper bound on received_at
--   window_start_date  DATE       partition pruner — keeps T-side reads narrow
--   window_end_date    DATE       partition pruner — keeps T-side reads narrow
--
-- Substitution (done in load.sh via envsubst before send):
--   ${PROJECT_ID}.${DATASET}        target dataset
--   ${PROJECT_ID}.${STAGING}        staging external table (just-created)
--
-- Source filtering mirrors validate.sql — rows that fail any predicate were
-- already routed to messages_errors and MUST NOT be re-inserted here.
--
-- Partition pruning: the ON-clause `DATE(T.received_at) BETWEEN ...` lets BQ
-- skip partitions outside the window. Required by the table's
-- require_partition_filter = TRUE option.

MERGE INTO `${PROJECT_ID}.${DATASET}.messages` T
USING (
  SELECT
    id,
    schema_version,
    tenant_id,
    account_id,
    channel_type,
    conversation_id,
    conversation_external_id,
    conversation_kind,
    parent_conversation_id,
    source_message_id,
    thread_root_message_id,
    sender,
    text,
    attachments,
    received_at,
    attributes,
    CURRENT_TIMESTAMP() AS _ingest_at,
    _FILE_NAME          AS _source_object
  FROM `${PROJECT_ID}.${STAGING}`
  WHERE date BETWEEN @window_start_date AND @window_end_date
    AND received_at >= @window_start
    AND received_at <  @window_end
    -- validation invariants — keep in lock-step with validate.sql
    AND account_id IS NOT NULL AND LENGTH(account_id) > 0
    AND source_message_id IS NOT NULL AND LENGTH(source_message_id) > 0
    AND (
      conversation_kind IS NULL OR conversation_kind IN UNNEST([
        'CONVERSATION_KIND_UNSPECIFIED',
        'CONVERSATION_KIND_DM',
        'CONVERSATION_KIND_GROUP_DM',
        'CONVERSATION_KIND_CHANNEL_PUBLIC',
        'CONVERSATION_KIND_CHANNEL_PRIVATE',
        'CONVERSATION_KIND_THREAD',
        'CONVERSATION_KIND_FORUM_POST',
        'CONVERSATION_KIND_BROADCAST'
      ])
    )
) S
ON T.account_id = S.account_id
   AND T.source_message_id = S.source_message_id
   AND DATE(T.received_at) BETWEEN @window_start_date AND @window_end_date
WHEN NOT MATCHED THEN
  INSERT ROW;
