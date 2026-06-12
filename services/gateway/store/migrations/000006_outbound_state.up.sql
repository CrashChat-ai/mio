CREATE TABLE outbound_state (
    send_command_id text PRIMARY KEY,
    account_id      text NOT NULL,
    external_id     text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX outbound_state_created_at_idx ON outbound_state (created_at);
