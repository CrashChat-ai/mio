# MIO — Deployment Guide

**Last updated:** 2026-05-13  
**Scope:** GKE POC reference, local dev, all-in-one binary, admin server bootstrap

---

## Local Development

### Quick Start

```bash
cd /path/to/mio
make up              # Start docker-compose (NATS, Postgres, MinIO)
make proto           # Generate proto stubs (proto/gen/)
make gateway-test    # Run gateway unit tests
```

### Services

Docker Compose (`deploy/local/docker-compose.yml`) brings up 5 services:

| Service | Port | Purpose | Health Check |
|---|---|---|---|
| `nats` | 4222 (client), 8222 (monitoring) | NATS JetStream message bus | TCP 4222 |
| `postgres` | 5432 | Operational database (mio_app user, mio DB) | SQL query |
| `minio` | 9000 (API), 9001 (console) | S3-compatible object storage | TCP 9000 |
| `gateway` | 8080 | mio-gateway service (HTTP) | GET /health |
| `sink-gcs` | async | Archive consumer (writes to MinIO) | NATS consumer lag |

**Port collisions:** Copy `.env.example` to `.env.local`, override ports, then `export $(grep -v '^#' .env.local | xargs) && make up`.

### Testing Integration

```bash
# Unit tests (no live deps)
make gateway-test        # gateway internal tests
make sdk-go-test         # sdk-go tests
make sdk-py-test         # sdk-py pytest (non-integration)
make sink-gcs-test       # sink-gcs tests

# Integration tests (need MIO_TEST_DSN)
MIO_TEST_DSN="postgres://mio_app:password@localhost/mio" \
  make gateway-bench-outbound  # Fairness benchmark
```

---

## All-in-One Binary (Laptop Demo)

### Purpose

For rapid prototyping without external NATS/Postgres. Embeds:
- NATS JetStream server (memory or file-backed)
- Schema migrations (bootstrapped on startup)
- Gateway service (inbound + outbound)

### Storage Modes

```bash
# Memory storage (volatile, loses on restart)
make run-laptop

# File storage (persistent, ./var/jetstream/)
make run-laptop-persist

# Or manually:
cd gateway
go run ./cmd/all-in-one --storage [memory|file] --store-dir ./var/jetstream
```

### Guards

**MIO_ENV=prod enforcement:** Panics if `--storage memory` on production deployments (see `services/gateway/internal/nats/guardrail.go`).

```bash
# This fails in prod (safety guard):
MIO_ENV=prod go run ./cmd/all-in-one --storage memory
# panic: refusing to run with memory storage in production

# This works (file storage safe):
MIO_ENV=prod go run ./cmd/all-in-one --storage file --store-dir /data/jetstream
```

### Admin Server

Even with all-in-one, you can run the admin server on loopback:

```bash
# Terminal 1: gateway with embedded NATS
make run-laptop

# Terminal 2: admin server
make admin-run  # Listens on http://127.0.0.1:9090

# Terminal 3: TUI client
make tui-run    # Connects to admin server
```

---

## Kubernetes / GKE Deployment

### Cluster Shape (POC Reference)

```
ingress-nginx (cluster-scoped, TLS via letsencrypt-production)
        │ HTTP-01 via Cloud DNS
        ▼
host: <your-mio-host>  (e.g., mio.example.com)
        │
        ├─ path: /cliq  →  Service mio-gateway:80  →  Pods :8080
        │
        ▼ NATS :4222 (mio namespace)
        │
        ├─► mio-media-vault (1 replica POC)
        │   ├─ Pulls: MESSAGES_INBOUND
        │   └─ Publishes: MESSAGES_INBOUND_ENRICHED
        │
        ├─► mio-sink-gcs (1 replica)
        │   ├─ Pulls: MESSAGES_INBOUND
        │   └─ Writes: gs://bucket/mio/
        │
        └─► echo-consumer (POC, can scale)
            ├─ Pulls: MESSAGES_INBOUND_ENRICHED
            └─ Publishes: MESSAGES_OUTBOUND
```

### Namespace & RBAC

```bash
kubectl create namespace mio
# RBAC: ServiceAccount per component (Workload Identity for GCS access)
```

### Core Components

| Resource | Type | Count | Notes |
|---|---|---|---|
| `mio-gateway` | Deployment | 2 replicas | Stateless, scales horizontally |
| `mio-nats` | StatefulSet | 1 replica (POC) | File-backed PVC (standard storage class) |
| `mio-jetstream-bootstrap` | Job | — | Post-install, creates streams (one-shot) |
| `mio-media-vault` | Deployment | 1 replica (POC) | Can scale; MaxAckPending=N |
| `mio-sink-gcs` | Deployment | 1 replica | Workload Identity to GCP SA |
| `mio-echo-consumer` | Deployment | 1 replica | Example consumer (can remove) |
| `mio-postgres` | CNPG Cluster | 1 instance (POC) | 10Gi PVC, PG 17.2 |

### Helm Charts

Six charts in `deploy/charts/`:

1. **mio-nats** — NATS JetStream cluster
   ```bash
   helm dependency update deploy/charts/mio-nats
   helm install mio-nats deploy/charts/mio-nats \
     --namespace mio \
     --values deploy/charts/mio-nats/values.yaml
   ```

2. **mio-jetstream-bootstrap** — Initialize streams (run as post-install Job)
   ```bash
   helm install mio-jetstream-bootstrap deploy/charts/mio-jetstream-bootstrap \
     --namespace mio
   ```

3. **mio-gateway** — Main API
   ```bash
   helm install mio-gateway deploy/charts/mio-gateway \
     --namespace mio \
     --set image.tag=<sha> \
     --set secrets.existingSecret=mio-gateway-secrets
   ```

4. **mio-media-vault** — Attachment sidecar
   ```bash
   helm install mio-media-vault deploy/charts/mio-media-vault \
     --namespace mio \
     --set serviceAccount.gcpServiceAccount=mio-media-vault@<project>.iam.gserviceaccount.com
   ```

5. **mio-sink-gcs** — Archive consumer
   ```bash
   helm install mio-sink-gcs deploy/charts/mio-sink-gcs \
     --namespace mio \
     --set serviceAccount.gcpServiceAccount=mio-sink-gcs@<project>.iam.gserviceaccount.com
   ```

6. **mio-echo-consumer** — Example consumer

**Linting:**
```bash
make helm-lint       # Lint all 6 charts
make helm-template   # Render with helm template to stdout
```

### Image Tag Policy

**CI publishes per-SHA tags to GHCR:**
```
ghcr.io/crashchat-ai/mio/gateway:<sha>
ghcr.io/crashchat-ai/mio/sink-gcs:<sha>
ghcr.io/crashchat-ai/mio/media-vault:<sha>
ghcr.io/crashchat-ai/mio/echo-consumer:<sha>
```

**Helm release tagging:**
- HelmRelease values pin `image.tag` to specific SHA
- Manual bump: edit SHA in infra repo (`fluxcd/apps/prod/mio/release-*.yaml`), push, Flux reconciles
- Auto-bump deferred to P10 (image-reflector-controller)

### Database Bootstrap

**Important:** Admin server does NOT run migrations. Follow this order:

1. Deploy CNPG Postgres instance (independent, via infra repo)
2. Deploy `mio-jetstream-bootstrap` (one-shot Job)
3. Deploy `mio-admin` and run schema migrations:
   ```bash
   # Option A: Set gateway to auto-migrate
   kubectl set env deployment/mio-gateway MIO_MIGRATE_ON_START=true
   kubectl rollout restart deployment/mio-gateway

   # Option B: Run migrations manually
   make gateway-migrate   # Locally, or exec in pod
   ```
4. After schema is live, deploy remaining services (gateway, media-vault, sink-gcs, echo-consumer)

---

## Secret Management

All secrets SOPS-encrypted in deployer's infra repo. Plaintext never in git.

### Gateway Secrets (`mio-gateway-secrets`)

**Keys:**
- `CLIQ_WEBHOOK_SECRET` — Webhook signature verification (Zoho Cliq)
- `CLIQ_CLIENT_ID` — OAuth client ID
- `CLIQ_CLIENT_SECRET` — OAuth client secret
- `CLIQ_REFRESH_TOKEN` — Long-lived refresh token (bootstrap via auth flow)
- `CLIQ_BOT_NAME` — Bot name for outbound messages
- `CLIQ_BOT_SCOPE` — OAuth scope
- `DATABASE_URL` — postgres://user:pass@host/dbname

**Rotation procedure:**

```bash
cd <your-infra-repo>

# Decrypt, edit, re-encrypt
SOPS_AGE_KEY_FILE=.secrets/age-key.txt sops fluxcd/apps/prod/mio/secrets.enc.yaml

# Commit and push
git commit -am "chore(mio): rotate CLIQ_WEBHOOK_SECRET" && git push

# Trigger Flux reconciliation
flux reconcile kustomization mio --with-source

# Restart gateway to pick up new secret
kubectl -n mio rollout restart deploy/mio-gateway
```

**Critical: CLIQ_WEBHOOK_SECRET rotation order**

1. **Push the new secret to cluster first** (gateway will accept old + new during rollout window)
2. **Update the value in Zoho Cliq bot UI** (Settings → Webhooks → Edit)

Reverse order causes every Cliq webhook to fail with `bad_signature` for ~5 minutes.

### CNPG Credentials (`mio-app-credentials`)

Password managed by CNPG operator via `bootstrap.initdb.secret`. To rotate:

```bash
# Edit password in infra repo
SOPS_AGE_KEY_FILE=.secrets/age-key.txt sops fluxcd/databases/prod/mio/secrets.enc.yaml

# Update DATABASE_URL in mio-gateway-secrets (same password hash)
# Restart gateway
kubectl -n mio rollout restart deploy/mio-gateway
```

### GHCR Registry Pull Secret (`ghcr-pull`)

```bash
# Generate new GHCR PAT (Settings → Developer settings → Personal access tokens)
kubectl create secret docker-registry ghcr-pull \
  --docker-server=ghcr.io \
  --docker-username=<user> \
  --docker-password=<NEW_PAT> \
  --docker-email=<email> \
  -n mio --dry-run=client -o yaml > ghcr-pull.yaml

# Encrypt and add to infra repo
SOPS_AGE_KEY_FILE=.secrets/age-key.txt sops -e -i ghcr-pull.yaml
mv ghcr-pull.yaml fluxcd/apps/prod/mio/ghcr-pull.enc.yaml
git add fluxcd/apps/prod/mio/ghcr-pull.enc.yaml && git commit -am "chore: rotate GHCR PAT" && git push
```

---

## FluxCD Reconciliation

**Default interval:** 1–5 minutes. Force reconcile:

```bash
flux reconcile kustomization mio --with-source
flux get kustomizations -A | grep mio
```

**Troubleshooting:**

```bash
# Check HelmRelease status
kubectl -n mio describe helmrelease mio-gateway

# View release history
helm history mio-gateway -n mio

# Rollback if needed
helm rollback mio-gateway -n mio
```

---

## NATS HA Upgrade Path (Out of Scope for P8)

POC runs `mio-nats` with **1 replica + emptyDir** (accepts data loss risk on pod restart).

**To upgrade to HA without full re-bootstrap:**

### Step 1: Add Persistent Storage

Update `release-nats.yaml` in infra repo:

```yaml
nats:
  config:
    jetstream:
      fileStore:
        pvc:
          enabled: true
          size: 10Gi
          storageClassName: premium-rwo  # GKE: pd-balanced or pd-ssd
```

Helm upgrade mio-nats and wait for PVC binding.

### Step 2: Add Replicas

```yaml
nats:
  config:
    cluster:
      replicas: 3
  podDisruptionBudget:
    enabled: true
    maxUnavailable: 1
```

Helm upgrade. NATS automatically discovers peers and forms cluster.

### Step 3: Update Stream Replication

Edit `mio-jetstream-bootstrap` ConfigMap (or gateway's `AddOrUpdateStream` call):

```go
Replicas: 3  // Was: 1
```

Recreate streams (or Helm delete + reinstall mio-jetstream-bootstrap).

**Warning:** Existing R=1 streams must be recreated to replicate. Plan downtime or dual-stream migration.

### Step 4: Validate Cluster Health

```bash
kubectl exec -n mio mio-nats-0 -- nats --server=nats://localhost:4222 server check jetstream
```

---

## Attachment Persistence Flow (P9)

### Streams

| Stream | Subject | Retention | Publisher | Consumer(s) |
|---|---|---|---|---|
| `MESSAGES_INBOUND` | `mio.inbound.>` | 7d, limits | gateway | media-vault, sink-gcs |
| `MESSAGES_INBOUND_ENRICHED` | `mio.inbound_enriched.>` | 7d, limits | media-vault | echo/AI consumers |
| `MESSAGES_OUTBOUND` | `mio.outbound.>` | 24h, workqueue | AI consumers | gateway sender-pool |

### Object Storage Layout

```
gs://<your-mio-attachments-bucket>/
└── mio/attachments/
    └── channel_type=<slug>/date=YYYY-MM-DD/{sha256[:2]}/{sha256}{ext}
```

**Features:**
- Content-addressed: duplicate images = single object
- Partitioned by date for prefix-delete + chronological cleanup
- Lifecycle rule: 7-day expiry on `mio/attachments/` prefix (matches JetStream MaxAge)
- Object metadata: `sha256`, `account_id` (used for GDPR sweep filtering)

### Signed URL Generation

Default TTL: 1 hour. Re-mint from storage_key:

```bash
mio-media-cli signed-url gs://bucket/mio/attachments/... --ttl=1h
```

### GDPR Delete

See [docs/runbooks/attachment-gdpr-delete.md](runbooks/attachment-gdpr-delete.md).

### IAM Setup

See [docs/runbooks/media-vault-iam.md](runbooks/media-vault-iam.md).

### Operator Notes

- **7-day round-trip test:** Image must be retrievable ≥7d after receipt (verify after first deploy)
- **Backend swap:** Add new storage backend under `services/media-vault/internal/storage/{s3,azure}/`, flip `MIO_STORAGE_BACKEND=s3`
- **Old consumer removal:** After successful enriched-stream cutover, remove deprecated `ai-consumer` on `MESSAGES_INBOUND`:
  ```bash
  nats consumer rm MESSAGES_INBOUND ai-consumer
  ```

---

## Smoke Testing (kind)

For rapid testing on local kind cluster:

```bash
make kind-up          # Create cluster
make kind-deploy      # Install NATS + gateway + sink-gcs (templates only)
make kind-smoke       # Full: helm lint + template + NATS pod ready
make kind-down        # Destroy cluster
```

---

## Operational Runbooks

- [Attachment GDPR Delete](runbooks/attachment-gdpr-delete.md) — Data deletion procedure
- [Cliq Webhook Down](runbooks/cliq-webhook-down.md) — Incident response
- [Media Vault IAM Setup](runbooks/media-vault-iam.md) — GCP Workload Identity

---

## References

- [System Architecture](system-architecture.md) — Design, inbound/outbound flows
- [Code Standards](code-standards.md) — Deployment CI/CD, image tagging
- [Codebase Summary](codebase-summary.md) — Component layout
- `Makefile` — Build and deployment targets
- `.github/workflows/ci.yaml` — CI/CD pipeline definition
