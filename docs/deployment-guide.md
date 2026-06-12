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

### Admin Server & TUI

Even with all-in-one, you can run the admin server and TUI on loopback:

```bash
# Terminal 1: gateway with embedded NATS
make run-laptop

# Terminal 2: admin server (connect-rpc)
make admin-run  # Listens on http://127.0.0.1:9090

# Terminal 3: TUI client (read-only v1)
make tui-run    # Connects to admin server (default ADMIN_URL=http://127.0.0.1:9090)
```

**Admin server RPCs** (loopback-only by default):
- `CreateTenant`, `ListTenants`, `GetTenant` — tenant lifecycle and lookup
- `ListChannelTypes` — registered channel adapters and capabilities
- `StartInstall`, `CompleteInstall` — operator-driven OAuth install dance
- `ListAccounts`, `DisableAccount`, `RotateCredential` — account operations
- `TailMessages` — streaming tail of inbound messages (debugging)

**TUI v1 features:**
- Read-only: inspect messages, list channels, view consumer lag
- Write ops (create account, rotate credentials) deferred to P6+

---

## Embedded NATS Option

For development and single-host POC deployments, the `cmd/all-in-one` binary bundles gateway + NATS JetStream.

**Storage modes:**
```bash
# Memory (volatile, loses on restart)
make run-laptop

# File (persistent, ./var/jetstream/)
make run-laptop-persist

# Manual invocation
go run ./services/gateway/cmd/all-in-one \
  --storage [memory|file] \
  --store-dir ./var/jetstream \
  --listen 127.0.0.1:8080
```

**Guard on production:**
- Panics if `MIO_ENV=prod` AND `--storage memory`
- File storage is safe for prod single-host deployments
- Recommended for production: external 3-replica NATS cluster

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
| `mio-jetstream-bootstrap` | Job | — | Post-install verification; never creates or mutates JetStream resources |
| `mio-media-vault` | Deployment | 1 replica (POC) | Can scale; MaxAckPending=N |
| `mio-sink-gcs` | Deployment | 1 replica | Workload Identity to GCP SA |
| `mio-echo-consumer` | Deployment | 1 replica | Example consumer (can remove) |
| `mio-postgres` | CNPG Cluster | 1 instance (POC) | 10Gi PVC, PG 17.2 |

### Helm Charts

Seven charts in `deploy/charts/`:

1. **mio-nats** — NATS JetStream cluster
   ```bash
   helm dependency update deploy/charts/mio-nats
   helm install mio-nats deploy/charts/mio-nats \
     --namespace mio \
     --values deploy/charts/mio-nats/values.yaml
   ```

2. **mio-gateway** — Main API
   ```bash
   helm install mio-gateway deploy/charts/mio-gateway \
     --namespace mio \
     --set image.tag=<sha> \
     --set secrets.existingSecret=mio-gateway-secrets
   ```

3. **mio-media-vault** — Attachment sidecar
   ```bash
   helm install mio-media-vault deploy/charts/mio-media-vault \
     --namespace mio \
     --set serviceAccount.gcpServiceAccount=mio-media-vault@<project>.iam.gserviceaccount.com
   ```

4. **mio-sink-gcs** — Archive consumer
   ```bash
   helm install mio-sink-gcs deploy/charts/mio-sink-gcs \
     --namespace mio \
     --set serviceAccount.gcpServiceAccount=mio-sink-gcs@<project>.iam.gserviceaccount.com
   ```

5. **mio-echo-consumer** — Example consumer

6. **mio-jetstream-bootstrap** — Verify streams and durable consumers after their owning services have started
   ```bash
   helm install mio-jetstream-bootstrap deploy/charts/mio-jetstream-bootstrap \
     --namespace mio
   ```

**Linting:**
```bash
make helm-lint       # Lint all charts
make helm-template   # Render with helm template to stdout
```

### Release And Image Tag Policy

**Main branch CI publishes per-SHA tags and rolling `main` tags to GHCR:**
```
ghcr.io/crashchat-ai/mio/gateway:<sha>
ghcr.io/crashchat-ai/mio/sink-gcs:<sha>
ghcr.io/crashchat-ai/mio/media-vault:<sha>
ghcr.io/crashchat-ai/mio/echo-consumer:<sha>
ghcr.io/crashchat-ai/mio/web:<sha>
```

**SemVer releases:**
- Push a tag like `v0.3.0` to run `.github/workflows/release.yaml`.
- The workflow publishes all service images with the same version tag, e.g. `ghcr.io/crashchat-ai/mio/gateway:0.3.0`.
- The workflow packages all Helm charts as OCI artifacts under `oci://ghcr.io/crashchat-ai/mio/charts`, with chart version and appVersion set to the tag version.
- The workflow creates a GitHub Release with generated notes.

**Helm release tagging during deployment:**
- HelmRelease values pin `image.tag` to specific SHA
- For SemVer deployments, pin `image.tag` to the release version and consume the matching OCI chart version.
- Manual bump: edit SHA in infra repo (`fluxcd/apps/prod/mio/release-*.yaml`), push, Flux reconciles
- Auto-bump remains deferred to the infra repo (image-reflector-controller or release PR automation).

### Database Bootstrap

**Important:** Admin server does NOT run migrations. Follow this order:

1. Deploy CNPG Postgres instance (independent, via infra repo)
2. Run schema migrations before starting normal API traffic:
   ```bash
   # Option A: Set gateway to auto-migrate
   kubectl set env deployment/mio-gateway MIO_MIGRATE_ON_START=true
   kubectl rollout restart deployment/mio-gateway

   # Option B: Run migrations manually
   make gateway-migrate   # Locally, or exec in pod
   ```
3. After schema is live, deploy NATS-backed services (gateway, media-vault, sink-gcs, echo-consumer) so they provision their owned streams and consumers.
4. Run `mio-jetstream-bootstrap` as a verification gate after gateway, media-vault, and sink-gcs are ready.

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

Edit the owning service's stream config, then update the
`mio-jetstream-bootstrap` expected values so drift is caught by the verification
Job:

```go
Replicas: 3  // Was: 1
```

Recreate streams after changing replication. Reinstalling
`mio-jetstream-bootstrap` only verifies the resulting state; it does not mutate
JetStream.

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

### External Durable Consumers

Downstream services that read MIO streams should create their own durable
consumer, then register the expected policy through
`mio-jetstream-bootstrap.externalExpectedConsumers` in that deployment's Helm
values. This keeps the core bus channel-agnostic while still making consumer
cursor policy auditable.

Channel Pulse example:

```yaml
externalExpectedConsumers:
  - stream: MESSAGES_INBOUND_ENRICHED
    durable: channel-pulse
    filterSubject: "mio.inbound_enriched.>"
    ackPolicy: explicit
    deliverPolicy: new
    minAckWaitSeconds: 600
```

The same contract is available as
`deploy/charts/mio-jetstream-bootstrap/values-channel-pulse.example.yaml` for
deployment overlays.

For this contract to be valid, the enriched stream must also keep
`MaxAge>=600s`; the default MIO enriched stream keeps 7 days.

### Source Reconciliation

Run `services/gateway/cmd/source-reconciler` as a separate Job/CronJob/worker
when a provider's webhooks are not a complete source of truth. The process
reads one account/conversation history window, dedupes through Postgres, and
publishes fresh rows to `MESSAGES_INBOUND`; media-vault and downstream
consumers continue unchanged. If `MIO_RECONCILE_CURSOR` is unset, it resumes
from `source_reconcile_cursors.cursor`; successful runs advance that cursor and
failed runs record `last_error`/`last_error_at`.

Required env:

```bash
MIO_POSTGRES_DSN=postgres://...
MIO_NATS_URLS=nats://mio-nats:4222
MIO_ACCOUNT_ID=<account-uuid>
MIO_RECONCILE_CONVERSATION_EXTERNAL_ID=<provider-chat-id>
```

Useful optional env:

```bash
MIO_RECONCILE_CLIQ_CHANNEL_NAME=tobytimedev
MIO_RECONCILE_CURSOR=1781194520799
MIO_RECONCILE_SINCE=2026-06-11T00:00:00Z
MIO_RECONCILE_UNTIL=2026-06-12T00:00:00Z
MIO_RECONCILE_LIMIT=100
```

For Cliq, new OAuth installs request `ZohoCliq.Messages.READ`. Existing
write-only installs fail clearly with `scope_missing` and must be re-consented
before history reconciliation can backfill bot/API-authored messages.

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

## References

- [System Architecture](system-architecture.md) — Design, inbound/outbound flows
- [Code Standards](code-standards.md) — Deployment CI/CD, image tagging
- [Codebase Summary](codebase-summary.md) — Component layout
- `Makefile` — Build and deployment targets
- `.github/workflows/ci.yaml` — CI/CD pipeline definition
