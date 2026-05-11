# Mio deployment topology (POC, GKE reference)

This document describes the **reference POC deployment** onto a GKE cluster,
reconciled by FluxCD from a deployer-owned infra repo. The shape is intended as
a template: substitute your own project, host, bucket, and SA names below.

## Cluster shape

```
ingress-nginx (cluster-scoped, pre-existing)
        │  TLS (letsencrypt-production)
        ▼
host:  <your-mio-host>           (e.g. mio.example.com)
path:  /cliq  →  Service mio-gateway:80  →  gateway pods :8080
                                              │
                                              ▼
                       NATS JetStream (mio-nats:4222, namespace mio)
                                              │
                                              ▼
                     mio-echo-consumer  ─────►  outbound dispatcher  ──►  Cliq REST
                                              │
                                              ▼
                                    sink-gcs  →  gs://<your-mio-bucket>/mio/
```

Components in namespace `mio`:

| Resource | Provider | Notes |
|---|---|---|
| `mio-gateway` | HelmRelease (this repo's chart) | 2 replicas, ingress entry, talks to CNPG + NATS |
| `mio-nats` | HelmRelease (upstream nats chart) | **1 replica, emptyDir** (POC; data loss on pod loss — Risk #1) |
| `mio-echo-consumer` | HelmRelease (this repo's chart) | 1 replica, JetStream pull subscription |
| `mio-sink-gcs` | HelmRelease (this repo's chart) | 1 replica, Workload Identity to `mio-sink-gcs@<your-gcp-project>` |
| `mio-postgres` | CNPG `Cluster` (databases/prod/mio) | 1 instance, 10Gi premium-rwo, PG 17.2 |
| Secrets | SOPS-encrypted in your infra repo | `mio-gateway-secrets`, `mio-app-credentials`, `ghcr-pull` |

External infra reused:

- `ingress-nginx` (class `nginx`)
- `cert-manager` ClusterIssuer `letsencrypt-production` (HTTP-01 via NGINX)
- CNPG operator + your backup SA (e.g. `prod-cnpg-backup-sa@<your-gcp-project>`)
- Cloud DNS managed zone for your host's apex domain (manual A record)
- GCS bucket `gs://<your-mio-bucket>` with prefix `mio/`

## Image tag policy

CI publishes per-SHA tags to GHCR for all three components:

- `ghcr.io/crashchat-ai/mio/gateway:<sha>`
- `ghcr.io/crashchat-ai/mio/sink-gcs:<sha>`
- `ghcr.io/crashchat-ai/mio/echo-consumer:<sha>`

HelmRelease values pin `image.tag` to a specific SHA. Bumping is **manual**:
edit the SHA in `infra/fluxcd/apps/prod/mio/release-*.yaml`, push,
Flux reconciles. Auto-bump (image-reflector-controller) deferred to P10.

## Secret rotation

All cluster Secrets are SOPS-encrypted in the infra repo and reconciled
by Flux. Plaintext never lives in Git.

### Gateway secrets (`mio-gateway-secrets`)

Keys: `CLIQ_WEBHOOK_SECRET`, `CLIQ_CLIENT_ID`, `CLIQ_CLIENT_SECRET`,
`CLIQ_REFRESH_TOKEN`, `CLIQ_BOT_NAME`, `CLIQ_BOT_SCOPE`, `DATABASE_URL`.
Gateway and media-vault mint short-lived access tokens on demand
from the refresh token (Zoho access tokens are 1h-TTL). Stored at
`infra/fluxcd/apps/prod/mio/secrets.enc.yaml`.

```bash
cd <your-infra-repo>
SOPS_AGE_KEY_FILE=.secrets/age-key.txt sops fluxcd/apps/prod/mio/secrets.enc.yaml
# edit → save → re-encrypted on write
git commit -am "chore(mio): rotate <key>" && git push
flux reconcile kustomization mio --with-source
kubectl -n mio rollout restart deploy/mio-gateway
```

### `CLIQ_WEBHOOK_SECRET` specifically

Order matters when rotating: push the cluster Secret first (gateway will
accept old + new during the rollout window), **then** update the value
in the Zoho Cliq bot UI. Reverse order ⇒ every Cliq webhook fails with
`bad_signature` for ~5 minutes.

### CNPG credentials (`mio-app-credentials`)

Owned by CNPG via `bootstrap.initdb.secret`. To rotate the password,
edit `infra/fluxcd/databases/prod/mio/secrets.enc.yaml`, push, then
restart the gateway so it re-reads `DATABASE_URL`.

### `ghcr-pull`

GHCR PAT with `read:packages`. Rotate by regenerating the PAT, then:

```bash
kubectl create secret docker-registry ghcr-pull \
  --docker-server=ghcr.io --docker-username="$GHCR_USER" \
  --docker-password=$NEW_GHCR_PAT --docker-email="$GHCR_EMAIL" \
  -n mio --dry-run=client -o yaml > ghcr-pull.yaml
SOPS_AGE_KEY_FILE=.secrets/age-key.txt sops -e -i ghcr-pull.yaml
mv ghcr-pull.yaml fluxcd/apps/prod/mio/ghcr-pull.enc.yaml
```

## Flux reconciliation

Default kustomization interval is 1–5 min. Force a reconcile:

```bash
flux reconcile kustomization mio --with-source
flux get kustomizations -A | grep mio
```

If a HelmRelease fails to upgrade, `kubectl -n mio describe helmrelease
<name>` is the source of truth.

## NATS HA upgrade path (out of scope for P8)

POC runs `mio-nats` with **1 replica + emptyDir**. Per Risk #1 we accept
data loss on pod restart. To take the cluster to HA without a full
re-bootstrap:

1. **Storage first.** Update `release-nats.yaml`:
   - `nats.config.jetstream.fileStore.pvc.enabled: true`
   - `nats.config.jetstream.fileStore.pvc.size: 10Gi`
   - `nats.config.jetstream.fileStore.pvc.storageClassName: premium-rwo`
2. **Add replicas.** `nats.config.cluster.replicas: 3` and
   `nats.podDisruptionBudget.enabled: true` (`maxUnavailable: 1`).
3. **Stream replication.** Update `mio-jetstream-bootstrap` (or gateway's
   `AddOrUpdateStream`) to use `Replicas: 3` for `MESSAGES_INBOUND` /
   `MESSAGES_OUTBOUND`. Existing streams stay R=1 until you re-create them.
4. **Plan for the cutover.** R=1 → R=3 in JetStream is a rolling stream
   re-create. Schedule a maintenance window; replay from sink-gcs
   archive if any window of inbound traffic was lost.

Ordering matters: do **not** add replicas before storage, or new pods
will join with empty JetStream state and fight the existing pod over
stream leadership.

## Attachment persistence flow (P9)

**Goal:** AI consumers always retrieve attachment bytes regardless of
platform URL TTLs (Cliq's are ~12 min).

### Streams

| Stream | Subjects | Retention | Producer | Consumer(s) |
|---|---|---|---|---|
| `MESSAGES_INBOUND` | `mio.inbound.>` | 7d | gateway | sink-gcs (archive), `mio-media-vault` (sidecar) |
| `MESSAGES_INBOUND_ENRICHED` | `mio.inbound_enriched.>` | 7d | `mio-media-vault` | echo / AI consumers |
| `MESSAGES_OUTBOUND` | `mio.outbound.>` | 24h | echo / AI consumers | gateway sender pool |

The sidecar provisions `MESSAGES_INBOUND_ENRICHED` idempotently on boot.

### Object storage layout

```
gs://<your-mio-attachments-bucket>/
└── mio/attachments/
    └── {channel_type}/yyyy=YYYY/mm=MM/dd=DD/{sha256[:2]}/{sha256}{ext}
```

- Content-addressable: same image = single object.
- Partitioned by `received_at` for prefix-delete + chronological cleanup.
- Lifecycle rule: 7d expiry on `mio/attachments/` prefix (matches JetStream MaxAge).
- Object metadata: `sha256`, `account_id` (used for GDPR sweep filtering).

### Signed URLs

Default TTL: 1h. Re-mint from `Attachment.storage_key` via the CLI:

```bash
mio-media-cli signed-url <key> --ttl=1h
```

### GDPR delete

See [`docs/runbooks/attachment-gdpr-delete.md`](runbooks/attachment-gdpr-delete.md).

### IAM

See [`docs/runbooks/media-vault-iam.md`](runbooks/media-vault-iam.md).

### Operator notes

- The 7d round-trip success criterion (image retrievable ≥7d later) cannot
  be verified at deploy time — re-test ≥7d after first deploy.
- Backend swap (GCS→S3) is one new file under
  `media-vault/internal/storage/s3/` plus an env flip
  (`MIO_STORAGE_BACKEND=s3`); zero changes to worker / consumer / sidecar core.
- Old durable `ai-consumer` on `MESSAGES_INBOUND` should be removed after
  successful enriched-stream cutover: `nats consumer rm MESSAGES_INBOUND ai-consumer`.
