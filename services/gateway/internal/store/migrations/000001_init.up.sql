-- MIO bootstrap schema — version 000001
-- Four-tier addressing: tenant_id → account_id → conversation_id → message_id
-- No nullable tenant_id or account_id anywhere: present from row 1.

-- tenants: master tenant seeded for POC via MIO_TENANT_ID env var.
CREATE TABLE IF NOT EXISTS tenants (
  id          UUID PRIMARY KEY,
  slug        TEXT NOT NULL UNIQUE,
  status      TEXT NOT NULL DEFAULT 'active',
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- accounts: one row per (tenant, channel install).
-- channel_type must match proto/channels.yaml registry slug (e.g. "zoho_cliq").
CREATE TABLE IF NOT EXISTS accounts (
  id           UUID PRIMARY KEY,
  tenant_id    UUID NOT NULL REFERENCES tenants(id),
  channel_type TEXT NOT NULL,
  external_id  TEXT NOT NULL,
  display_name TEXT NOT NULL,
  attributes   JSONB NOT NULL DEFAULT '{}',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (tenant_id, channel_type, external_id)
);

-- conversations: polymorphic via kind.
-- One row per DM/group/channel/thread.
-- ON CONFLICT DO NOTHING: first-write-wins; never UPDATE kind or display_name.
CREATE TABLE IF NOT EXISTS conversations (
  id                     UUID PRIMARY KEY,
  tenant_id              UUID NOT NULL REFERENCES tenants(id),
  account_id             UUID NOT NULL REFERENCES accounts(id),
  channel_type           TEXT NOT NULL,
  kind                   TEXT NOT NULL,
  external_id            TEXT NOT NULL,
  parent_conversation_id UUID REFERENCES conversations(id),
  parent_external_id     TEXT,
  display_name           TEXT,
  attributes             JSONB NOT NULL DEFAULT '{}',
  created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (account_id, external_id)
);
CREATE INDEX IF NOT EXISTS conversations_tenant_id_idx ON conversations (tenant_id);
CREATE INDEX IF NOT EXISTS conversations_account_kind_idx ON conversations (account_id, kind);
CREATE INDEX IF NOT EXISTS conversations_parent_conv_idx ON conversations (parent_conversation_id)
  WHERE parent_conversation_id IS NOT NULL;

-- messages: idempotency lives here.
-- (account_id, source_message_id) unique catches replays from same channel install.
CREATE TABLE IF NOT EXISTS messages (
  id                     UUID PRIMARY KEY,
  tenant_id              UUID NOT NULL REFERENCES tenants(id),
  account_id             UUID NOT NULL REFERENCES accounts(id),
  conversation_id        UUID NOT NULL REFERENCES conversations(id),
  thread_root_message_id UUID REFERENCES messages(id),
  source_message_id      TEXT NOT NULL,
  sender_external_id     TEXT NOT NULL,
  text                   TEXT NOT NULL DEFAULT '',
  attributes             JSONB NOT NULL DEFAULT '{}',
  received_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (account_id, source_message_id)
);
CREATE INDEX IF NOT EXISTS messages_conv_time_idx ON messages (tenant_id, conversation_id, received_at DESC);
