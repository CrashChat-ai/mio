-- Dev seed: the env-identity tenant + account the gateway falls back to
-- (MIO_TENANT_ID / MIO_ACCOUNT_ID). Without these rows, inbound persist fails
-- the conversations_tenant_id_fkey / account_id_fkey constraints, so the Cliq
-- loop never gets past the gateway. Idempotent — safe to re-run every boot.
INSERT INTO tenants (id, slug, status, display_name)
VALUES ('00000000-0000-0000-0000-000000000001', 'dev', 'active', 'Dev Tenant')
ON CONFLICT (id) DO NOTHING;

INSERT INTO accounts (id, tenant_id, channel_type, external_id, display_name, provider)
VALUES ('00000000-0000-0000-0000-000000000002',
        '00000000-0000-0000-0000-000000000001',
        'zoho_cliq', 'dev-cliq', 'Dev Cliq', 'default')
ON CONFLICT (id) DO NOTHING;
