-- MIO admin schema — version 000002
-- Extends 000001's tenants + accounts; introduces credentials + installs.
-- Additive only: no DROP of base tables; uniqueness on accounts widened
-- to include provider (Chatwoot pattern for WhatsApp Cloud vs 360dialog).
--
-- Idempotent on partial-apply: every statement uses IF [NOT] EXISTS so
-- golang-migrate's dirty-state retry succeeds without manual force.

ALTER TABLE tenants ADD COLUMN IF NOT EXISTS display_name TEXT;
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS disabled_at TIMESTAMPTZ;

ALTER TABLE accounts ADD COLUMN IF NOT EXISTS provider TEXT NOT NULL DEFAULT 'default';
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS disabled_at TIMESTAMPTZ;

-- Widen uniqueness to 4-column. Default 'default' preserves identity of
-- pre-migration rows; new inserts may select a different provider.
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_tenant_id_channel_type_external_id_key;
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conname = 'accounts_tenant_channel_provider_external_key'
  ) THEN
    ALTER TABLE accounts ADD CONSTRAINT accounts_tenant_channel_provider_external_key
      UNIQUE (tenant_id, channel_type, provider, external_id);
  END IF;
END$$;

CREATE INDEX IF NOT EXISTS accounts_tenant_channel_idx ON accounts (tenant_id, channel_type);

-- credentials: opaque KMS-encrypted blobs, one per account.
-- key_version populated from day 1 so future rotation is code-only.
CREATE TABLE IF NOT EXISTS credentials (
  account_id   UUID PRIMARY KEY REFERENCES accounts(id) ON DELETE RESTRICT,
  auth_kind    TEXT NOT NULL,
  ciphertext   BYTEA NOT NULL,
  key_version  INT NOT NULL DEFAULT 1,
  expires_at   TIMESTAMPTZ,
  rotated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS credentials_expires_at_idx ON credentials (expires_at);

-- installs: short-lived state for the OAuth dance.
-- state = pending → active | failed; admin clears completed rows on a TTL.
CREATE TABLE IF NOT EXISTS installs (
  id              UUID PRIMARY KEY,
  account_id      UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
  state           TEXT NOT NULL,
  error_reason    TEXT,
  installed_at    TIMESTAMPTZ,
  last_health_at  TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS installs_state_idx ON installs (state);
