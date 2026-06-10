-- Sessions for the mio-web operator console. The browser holds only the raw
-- random session token; Postgres stores a SHA-256 hash.
CREATE TABLE IF NOT EXISTS web_operator_sessions (
  id_hash        TEXT PRIMARY KEY,
  operator_email TEXT NOT NULL,
  operator_name  TEXT NOT NULL DEFAULT '',
  operator_avatar_url TEXT NOT NULL DEFAULT '',
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_seen_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at     TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS web_operator_sessions_expires_at_idx
  ON web_operator_sessions (expires_at);
