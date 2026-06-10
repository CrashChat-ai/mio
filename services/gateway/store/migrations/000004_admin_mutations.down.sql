DROP TABLE IF EXISTS web_operator_audit;

ALTER TABLE web_operator_sessions
  DROP COLUMN IF EXISTS operator_role;

ALTER TABLE accounts
  DROP COLUMN IF EXISTS rate_limit_scope,
  DROP COLUMN IF EXISTS rate_limit_per_second;
