---
title: "Local Dev: MIO + Zoho Cliq end-to-end"
description: Run a full inbound→process→outbound Zoho Cliq loop on your laptop with no real Zoho org and no cluster access.
---

# Local Dev: MIO + Zoho Cliq end-to-end

This is the turnkey path for developing against MIO — and for channel-pulse
developers who need a live MIO message bus. One command brings up a self-contained
MIO + Zoho Cliq loop with **no Zoho org, no cluster access, and no PHI**.

```bash
make cliq-up        # bring the stack up
make cliq-replay    # drive a synthetic Cliq inbound message (channel-text)
make cliq-smoke     # assert the full round-trip closed (204 at cliq-mock)
```

## What comes up

`make cliq-up` (compose `--profile media`) starts, on one network:

| Service | Role | Host port |
|---|---|---|
| `nats` | JetStream bus (`MESSAGES_INBOUND`, `MESSAGES_OUTBOUND`, `…_ENRICHED`) | 4222 / 8222 |
| `postgres` | gateway store (conversations, messages) | 5432 |
| `minio` (+init) | S3 for attachments + sink | 9000 / 9001 |
| `gateway` | Cliq webhook in + sender pool out | 8080 |
| `cliq-mock` | fake Zoho REST + OAuth (so outbound works credential-free) | 8090 |
| `media-vault` | republishes inbound → `mio.inbound_enriched.>` | — |
| `echo-consumer` | echoes each enriched message back as an outbound SendCommand | — |

## The round-trip

```
make cliq-replay
   │  POST /webhooks/zoho-cliq  (HMAC-signed, dev secret)
   ▼
gateway ── verify sig → normalize → persist → publish ──► MESSAGES_INBOUND (mio.inbound.>)
   ▼
media-vault ── republish ──► MESSAGES_INBOUND_ENRICHED (mio.inbound_enriched.>)
   ▼
echo-consumer ── echo as SendCommand ──► MESSAGES_OUTBOUND (mio.outbound.>)
   ▼
gateway sender pool ── POST /api/v2/channelsbyname/{ch}/message ──► cliq-mock (204)
```

`make cliq-smoke` replays a fixture and asserts the outbound leg reached `cliq-mock`
(a `204` in its logs) within 20s — the whole loop, closed, with no Zoho.

## How the replay works (and the #1 footgun)

`scripts/cliq-replay.sh` reads a fixture under `channels/zohocliq/testdata/`, then:

1. **Unwraps `body_json`** — fixtures store the real Cliq payload under a `body_json`
   key. The webhook signature is computed over the **inner** bytes, not the file as
   stored. Posting the raw file fails signature verification.
2. Reads the dev secret from `deploy/local/secrets/cliq-webhook-secret` (`dev-webhook-secret`).
3. Signs: `X-Webhook-Signature: sha256=<hmac-sha256(secret, inner-body)>` (hex).
4. `POST`s the inner body to `/webhooks/zoho-cliq`.

A correct request returns `200 {"ok":true}`. A bad/missing signature returns `401`.
Replaying the same fixture publishes once (idempotency on `(account, message id)`).
`make cliq-smoke` sets `UNIQUE=1`, which rewrites the message id to a nonce so it's
repeatable; plain `make cliq-replay` is faithful (re-runs dedupe).

```bash
make cliq-replay FIXTURE=channel-text       # any testdata fixture, by name fragment
UNIQUE=1 ./scripts/cliq-replay.sh           # fresh id each run, never deduped
```

> **Channel fixtures vs DM fixtures.** Only **channel** fixtures (`channel-text`,
> `channel-bot-mention`, …) close the *full* round-trip — Cliq's bot send addresses a
> channel by name (`channelsbyname`), so the outbound leg needs a `cliq_channel_name`.
> **DM** fixtures (`dm-to-bot`) exercise inbound only; the Cliq adapter has no bot-send-to-DM
> endpoint. The default fixture is `channel-text` for this reason.

## The cliq-mock

`deploy/local/cliq-mock/` is a ~50-line Go stub standing in for Zoho:

- `POST /oauth/v2/token` → a static `mock-token`, so the gateway's OAuth refresh succeeds.
- `POST /api/v2/channelsbyname/{name}/message` → `204`, like the real bot send endpoint.

The gateway points at it via `CLIQ_API_BASE_URL=http://cliq-mock:8080` and
`CLIQ_OAUTH_URL=http://cliq-mock:8080/oauth/v2/token`. The dev Cliq credentials
(`CLIQ_CLIENT_ID/SECRET/REFRESH_TOKEN/BOT_NAME`) are dummy values — all three OAuth
vars must be set together or the adapter refuses to start.

> **Gotcha — `MIO_TENANT_ID`.** `.mise.toml` exports `MIO_TENANT_ID=tenant-dev` for
> other tooling, but the gateway store needs a UUID tenant. The local compose pins
> literal dev UUIDs for the gateway and media-vault so inbound persist works
> regardless of your shell env. Attachment-bearing fixtures need real Cliq creds
> (media-vault would try to fetch); the text fixtures are fully credential-free.

> **Auto-seeded tenant + account.** The gateway's DB-backed routing needs a `tenants`
> and `accounts` row for the env-identity UUIDs, or inbound persist fails the FK. The
> `db-seed` compose service inserts them idempotently once the gateway has migrated —
> no manual account creation, no admin OAuth dance.

## Hooking up channel-pulse

channel-pulse develops against this local bus instead of the dev cluster. With
`make cliq-up` running (NATS on host `4222`), in `prototypes/channel-pulse-stack`:

```bash
make up    # CP's own backend + frontend + pgvector Postgres
```

CP's backend already targets `nats://host.docker.internal:4222`. By default it consumes
the **enriched** stream (matching prod), which `make cliq-up` provides via media-vault:

| CP env | Default (prod parity) | Raw-stream alternative |
|---|---|---|
| `MIO_STREAM` | `MESSAGES_INBOUND_ENRICHED` | `MESSAGES_INBOUND` |
| `MIO_FILTER` | `mio.inbound_enriched.>` | `mio.inbound.>` |
| `MIO_DURABLE` | `channel-pulse` | `cp-local-<you>` (use a personal name) |

Run `make cliq-replay` and the message flows through to channel-pulse's ingest and dashboard.

> **Caveat — fresh-stream durable.** channel-pulse uses `nats.py` directly, and
> `js.pull_subscribe(filter, durable=...)` resolves the stream by subject — which raises
> `NotFoundError` on a wildcard subject when the durable doesn't exist yet (a fresh local
> stream). Either pre-create the durable (`nats consumer add MESSAGES_INBOUND_ENRICHED …`)
> or pass the stream explicitly: `pull_subscribe(filter, durable=…, stream=settings.mio_stream)`.
> MIO's own Python SDK (`mio.client.consume_inbound`) already passes `stream=`, so
> `echo-consumer` binds cleanly on a brand-new stream.

## Optional: real ABS dev data

To validate against **real** traffic instead of synthetic fixtures, consume the live
ABS dev JetStream. NATS has **no authentication** anywhere — access is pure network
reachability via a `kubectl` port-forward (needs prod GKE RBAC):

```bash
make ingest-prod    # kubectl port-forward svc/mio-nats 4225:4222
```

Then point your consumer at `nats://localhost:4225`. **Always** use a personal durable
(`MIO_DURABLE=mio-local-$USER`) so you don't advance the shared `channel-pulse-dev`
cursor on the prod stream. Note: this pulls regex-de-identified (not audit-grade)
message content onto your laptop — treat it as sensitive and prefer the synthetic
loop for day-to-day work.

## Teardown

```bash
make cliq-down    # stop the stack (volumes preserved)
make clean        # stop + wipe volumes (fresh JetStream/Postgres next time)
```
