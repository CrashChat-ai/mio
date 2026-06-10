ALTER TABLE accounts
  ADD COLUMN IF NOT EXISTS rate_limit_per_second INTEGER,
  ADD COLUMN IF NOT EXISTS rate_limit_scope TEXT;

ALTER TABLE web_operator_sessions
  ADD COLUMN IF NOT EXISTS operator_role TEXT NOT NULL DEFAULT 'viewer';

CREATE TABLE IF NOT EXISTS web_operator_audit (
  id             BIGSERIAL PRIMARY KEY,
  operator_email TEXT NOT NULL,
  operator_role  TEXT NOT NULL,
  action         TEXT NOT NULL,
  target_type    TEXT NOT NULL,
  target_id      TEXT NOT NULL,
  result         TEXT NOT NULL,
  error          TEXT NOT NULL DEFAULT '',
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS web_operator_audit_created_at_idx
  ON web_operator_audit (created_at DESC);

CREATE INDEX IF NOT EXISTS web_operator_audit_target_idx
  ON web_operator_audit (target_type, target_id, created_at DESC);
