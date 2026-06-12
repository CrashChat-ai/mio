# Self-Host Quickstart

Run MIO locally without any GCP account. Two options: **docker compose** (full stack) or **all-in-one binary** (embedded NATS).

---

## Option 1: Docker Compose (Recommended)

**Requirements:** Docker + Docker Compose + `make`

### Bring Up the Stack

```bash
cd mio
make up
```

This starts:
- **NATS JetStream** (4222) — message bus
- **Postgres** (5432) — operational state
- **MinIO** (9000 API, 9001 console) — S3-compatible object storage
- **gateway** (8080) — webhook ingress + sender pool

Verify health:

```bash
docker compose -f deploy/local/docker-compose.yml ps
```

All should show `healthy` or `running`.

### Access MinIO Console

- URL: `http://localhost:9001`
- User: `minioadmin`
- Password: `minioadmin`

Browse `mio-messages` (message archive) and `mio-attachments` (media).

### Create Your First Tenant & Account

Use the admin CLI (runs in-process with the gateway):

```bash
# Terminal 1: Start admin server (loopback:9090)
make admin-run &

# Terminal 2: Create tenant
ADMIN_URL=http://localhost:9090 go run ./services/gateway/cmd/admin --create-tenant \
  --tenant-id 00000000-0000-0000-0000-000000000001 \
  --tenant-slug my-tenant \
  --display-name "My Tenant"

# Create account
ADMIN_URL=http://localhost:9090 go run ./services/gateway/cmd/admin --create-account \
  --tenant-id 00000000-0000-0000-0000-000000000001 \
  --account-id 00000000-0000-0000-0000-000000000002 \
  --channel-type zoho_cliq
```

(The compose file uses hardcoded tenant/account IDs; queries use these defaults.)

### Expose Your Webhook

The gateway listens on `http://localhost:8080/webhooks/zoho_cliq`. To accept real Zoho Cliq webhooks, expose this via a tunnel:

```bash
# Option A: ngrok
ngrok http 8080

# Option B: cloudflare tunnel
cloudflared tunnel run --url http://localhost:8080 my-tunnel

# Option C: local DNS (if you own the domain)
# Add A record pointing to your IP, configure MIO_PUBLIC_BASE_URL
```

Then in Zoho Cliq admin console:
- Webhook URL: `https://<your-tunnel>.ngrok.io/webhooks/zoho_cliq`
- Webhook Secret: value from `deploy/local/secrets/zoho-cliq-webhook-secret`

### Configure Storage Backend (MinIO)

By default, the compose file uses MinIO for `sink-gcs` (archive writer). To use AWS S3 or Cloudflare R2 instead:

**Environment variables in `.env.local`:**

```bash
# AWS S3
SINK_BACKEND=s3
SINK_ENDPOINT=https://s3.amazonaws.com
SINK_ACCESS_KEY=AKIAIOSFODNN7EXAMPLE
SINK_SECRET_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
SINK_BUCKET=my-mio-archive

# Cloudflare R2
SINK_BACKEND=s3
SINK_ENDPOINT=https://<account-id>.r2.cloudflarestorage.com
SINK_ACCESS_KEY=<r2-token-id>
SINK_SECRET_KEY=<r2-token-secret>
SINK_BUCKET=mio-archive

# MinIO (local)
SINK_BACKEND=minio
SINK_ENDPOINT=http://minio:9000
SINK_ACCESS_KEY=minioadmin
SINK_SECRET_KEY=minioadmin
SINK_BUCKET=mio-messages
```

Then:

```bash
export $(grep -v '^#' .env.local | xargs)
make down && make up
```

### Consume with echo-consumer

The example Python consumer echoes inbound messages back to Cliq:

```bash
make echo-up
```

This starts the echo-consumer connected to your local NATS. Monitor logs:

```bash
make echo-logs -f
```

Send a message to Cliq → it appears in MIO → echo-consumer echoes it back → message appears in Cliq.

---

## Option 2: All-in-One Binary (Laptop Demo)

**Requirements:** Go 1.25+ (from `mise install`)

### Run the Binary

```bash
make run-laptop
```

This starts a single process with:
- Embedded NATS JetStream (memory-backed, `localhost:4222`)
- Gateway (`:8080`)
- Postgres (`:5432`) — **still external, not embedded**

**Guard rail:** If `MIO_ENV=prod` is set, the binary refuses to run with memory storage (panics on startup). Always use external NATS for production.

### Persistent Storage (Optional)

Use file-backed JetStream instead of memory:

```bash
make run-laptop-persist
```

This creates a `./var/jetstream/` directory and persists streams across restarts.

### Webhook Exposure

Same as compose: use ngrok or a tunnel to expose `http://localhost:8080/webhooks/zoho_cliq` to your Zoho Cliq workspace.

---

## Zero-GCP Checklist

- [x] **NATS:** Docker Compose or embedded
- [x] **Postgres:** Docker Compose (or bring your own with `MIO_POSTGRES_DSN` env var)
- [x] **Storage:** MinIO (S3-compatible) or AWS S3 / Cloudflare R2
- [x] **TLS:** Bring your own (run behind nginx/Caddy locally, or use a tunnel with auto-TLS)
- [x] **Secrets:** File-mounted (no Kubernetes / GCP Secret Manager needed)
- [x] **Metrics:** Optional (no Prometheus required; logs go to stdout)
- [x] **No Cloud SQL:** Use postgres:16 container
- [x] **No GCS:** Use MinIO or AWS S3
- [x] **No BigQuery:** Raw NDJSON sits in object storage; external loader optional

**Result:** A fully functional MIO deployment on your laptop or a bare VM.

---

## Troubleshooting

### NATS connection refused

Check compose health:

```bash
docker compose -f deploy/local/docker-compose.yml logs nats
```

NATS should output `Server is ready` after startup.

### Postgres connection error

Ensure postgres is healthy and listening:

```bash
docker compose -f deploy/local/docker-compose.yml logs postgres
```

If starting for the first time, migrations run automatically (`MIO_MIGRATE_ON_START=true` in compose).

### MinIO buckets not created

The `minio-init` service runs once on startup. Check:

```bash
docker compose -f deploy/local/docker-compose.yml logs minio-init
```

If failed, manually create buckets via console: `http://localhost:9001` → Create Bucket → `mio-messages`, `mio-attachments`.

### Cannot send message to Cliq

1. Verify webhook URL is publicly accessible: `curl -I https://<your-tunnel>/webhooks/zoho_cliq`
2. Verify webhook secret in Cliq matches `deploy/local/secrets/zoho-cliq-webhook-secret`
3. Verify `MIO_TENANT_ID` and `MIO_ACCOUNT_ID` match the account you created
4. Check gateway logs: `docker compose -f deploy/local/docker-compose.yml logs gateway`

### Consumer not receiving messages

1. Verify NATS stream exists: `docker exec mio-nats nats stream list` should show `MESSAGES_INBOUND`
2. Verify consumer lag: `docker exec mio-nats nats consumer info MESSAGES_INBOUND ai-consumer-enriched`
3. Check echo-consumer logs: `make echo-logs`

---

## Environment Variables Reference

| Variable | Default | Purpose |
|----------|---------|---------|
| `MIO_TENANT_ID` | `00000000-0000-0000-0000-000000000001` | Tenant UUID (compose default) |
| `MIO_ACCOUNT_ID` | `00000000-0000-0000-0000-000000000002` | Account UUID (compose default) |
| `MIO_NATS_URLS` | `nats://nats:4222` | NATS broker URLs |
| `MIO_POSTGRES_DSN` | `postgres://mio_app:dev_password@postgres:5432/mio?sslmode=disable` | Postgres connection |
| `MIO_MIGRATE_ON_START` | `true` | Auto-run database migrations |
| `MIO_PGX_MAX_CONNS` | `5` | Connection pool size |
| `MIO_LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `MIO_STORAGE_BACKEND` | `s3` | `s3` or `minio` or `gcs` |
| `MIO_STORAGE_BUCKET` | `mio-attachments` | Object storage bucket for attachments |
| `MIO_STORAGE_S3_ENDPOINT` | `http://minio:9000` | S3 endpoint (for MinIO / R2) |
| `MIO_STORAGE_S3_ACCESS_KEY` | `minioadmin` | S3 access key |
| `MIO_STORAGE_S3_SECRET_KEY` | `minioadmin` | S3 secret key |
| `MIO_STORAGE_S3_USE_SSL` | `false` | Use HTTPS for S3 |
| `SINK_BACKEND` | `minio` | Archive sink backend (`s3` or `minio`) |
| `SINK_BUCKET` | `mio-messages` | Archive bucket name |
| `SINK_ENDPOINT` | `http://minio:9000` | Archive endpoint |
| `SINK_ACCESS_KEY` | `minioadmin` | Archive access key |
| `SINK_SECRET_KEY` | `minioadmin` | Archive secret key |

---

## Next Steps

1. **Add a second adapter:** Follow [docs/adapter-authoring-guide.md](./adapter-authoring-guide.md) to scaffold Slack or another platform.
2. **Build your consumer:** Use the [consumer-contract docs](./consumer-contract.md) to build your own AI service on top of `MESSAGES_INBOUND_ENRICHED`.
3. **Deploy to a cluster:** Read [deployment-guide.md](./deployment-guide.md) for Kubernetes + multi-replica NATS.

---

## Related Docs

- [Consumer Contract](./consumer-contract.md) — NATS stream schema and message format
- [Adapter Authoring Guide](./adapter-authoring-guide.md) — Build a new channel adapter
- [Deployment Guide](./deployment-guide.md) — GKE + Helm charts
- [System Architecture](./system-architecture.md) — Data flow and component design
