-- Rollback 000002_admin_schema — reverse only Phase 3 additions.
-- Order matters: drop FK-dependent tables first, then restore the
-- original 3-column accounts unique constraint, then drop columns.

DROP TABLE IF EXISTS installs;
DROP TABLE IF EXISTS credentials;

ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_tenant_channel_provider_external_key;
ALTER TABLE accounts ADD CONSTRAINT accounts_tenant_id_channel_type_external_id_key
  UNIQUE (tenant_id, channel_type, external_id);

ALTER TABLE accounts DROP COLUMN IF EXISTS disabled_at;
ALTER TABLE accounts DROP COLUMN IF EXISTS provider;
ALTER TABLE tenants DROP COLUMN IF EXISTS disabled_at;
ALTER TABLE tenants DROP COLUMN IF EXISTS display_name;
