-- Source reconciliation cursor/status state.
-- A row is one provider conversation under one channel account. Reconciler
-- workers publish fresh history rows to MESSAGES_INBOUND and advance cursor
-- only after the run succeeds.

CREATE TABLE IF NOT EXISTS source_reconcile_cursors (
  account_id               UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  channel_type             TEXT NOT NULL,
  conversation_external_id TEXT NOT NULL,
  cursor                   TEXT NOT NULL DEFAULT '',
  last_success_at          TIMESTAMPTZ,
  last_error               TEXT,
  last_error_at            TIMESTAMPTZ,
  updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (account_id, conversation_external_id)
);

CREATE INDEX IF NOT EXISTS source_reconcile_cursors_channel_idx
  ON source_reconcile_cursors (channel_type, updated_at DESC);
