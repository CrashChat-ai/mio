# Echo Consumer

A reference Python consumer that demonstrates the MIO consumer contract.

## What It Does

Consumes `MESSAGES_INBOUND` from NATS JetStream, echoes each message back as a `SendCommand` to `MESSAGES_OUTBOUND`, and acks.

**Design invariants** (locked; do not change):
- Consumes via `sdk.consume_inbound()` async iterator only — SDK owns the pull-fetch loop.
- Signal handlers (SIGTERM) registered before iterator opens — 6 second graceful drain.
- No schema verification on consume path (publish-side asymmetry per consumer contract).
- All four-tier IDs (tenant_id, account_id, channel_type, conversation_id) preserved from inbound to outbound unchanged.
- Idempotency key on outbound: set by SDK as `Nats-Msg-Id = "out:<cmd.id>"` — do not set manually.
- Metrics: `{channel_type, direction, outcome}` only — no account_id, tenant_id, or conversation details.

## Run Locally

### Via Docker Compose

Start the full stack (NATS + Postgres + MinIO + gateway):

```bash
cd mio
make up
```

Start the echo consumer:

```bash
make echo-up
```

Monitor logs:

```bash
make echo-logs -f
```

Send a message to Zoho Cliq → appears in echo-consumer logs → echoed back to Cliq.

### Manual (for debugging)

```bash
cd mio
export NATS_URL=nats://localhost:4222
poetry install
python examples/echo-consumer/echo.py
```

## How It Works

1. **Connect to NATS** (`NATS_URL`, default `nats://nats:4222`)
2. **Consume from `MESSAGES_INBOUND`** via `sdk.consume_inbound()` — SDK pulls every 5 seconds
3. **For each message:**
   - Extract `message.id`, `account_id`, `channel_type`, `conversation_external_id`
   - Build a `SendCommand` with text = `"Echo: " + message.text`
   - Publish to `MESSAGES_OUTBOUND` using `sdk.publish_command()`
   - Ack the inbound message
4. **On SIGTERM:** Drain in-flight messages (up to 6 seconds), exit cleanly

## Testing

Run the test suite:

```bash
make echo-consumer-test
```

Tests cover:
- SDK initialization and NATS connection
- Message consume + echo cycle
- Graceful shutdown

## Related Docs

- **Consumer Contract** — `docs/consumer-contract.md` — NATS stream schema, message format, subject grammar
- **Self-Host Guide** — `docs/self-host-quickstart.md` — how to run MIO locally
- **SDK Reference** — `sdks/python/README.md` — async iterator API, idempotency, metrics

## Design Notes

### Why Not Manual Fetch?

The SDK owns the pull-fetch loop (`mio.consumer.Consumer.consume_inbound()`). This ensures:
- Predictable backoff and error handling
- Automatic heartbeat/flow-control
- Metrics aggregation
- Connection pooling

Manually calling `fetch()` in a loop is error-prone and loses these benefits.

### Metrics & Observability

The echo consumer emits metrics to stdout in JSON format:

```json
{
  "level": "info",
  "msg": "message_received",
  "channel_type": "zoho_cliq",
  "direction": "inbound",
  "outcome": "success"
}
```

These are tagged with `{channel_type, direction, outcome}` only — no account, tenant, or conversation details to protect user privacy.

### Idempotency

Outbound messages are idempotent via `Nats-Msg-Id`. If the consumer crashes mid-echo and restarts, NATS deduplicates the resend using the same ID.

The SDK sets this automatically — you don't need to touch it.

## Extending This Consumer

To use this as a template for your own service:

1. Copy `echo.py` → `my_consumer.py`
2. Replace the echo logic with your LLM call or business logic
3. Keep the signal handler, message preservation, and ack patterns
4. Extend metrics with your own labels (but not tenant/account/conversation)
5. Run via `docker build -f examples/echo-consumer/Dockerfile -t my-consumer . && docker run -e NATS_URL=... my-consumer`

Key patterns to preserve:
- Signal handlers before iterator open
- All four-tier IDs preserved
- Metrics labels: `{channel_type, direction, outcome}` only
- Idempotency key handled by SDK (don't override)
- Async/await for concurrency

See `docs/consumer-contract.md` for the full contract and subject grammar.
