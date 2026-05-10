#!/usr/bin/env bash
# deploy/gke/setup.sh — Idempotent GKE + GCS + IAM bootstrap for MIO POC.
# Run once per environment. Safe to re-run.
#
# Prerequisites:
#   gcloud CLI authenticated with project owner permissions
#   kubectl configured (or this script configures it via get-credentials)
#   GHCR_PAT env var set (fine-grained GitHub PAT, read:packages scope only)
#   helm 3.x installed
#
# DO NOT commit secrets. GHCR_PAT must be set in the calling environment.
#
# Usage:
#   PROJECT=my-gcp-project REGION=us-central1 GHCR_PAT=<pat> ./deploy/gke/setup.sh

set -euo pipefail

# ── Configuration ─────────────────────────────────────────────────────────────
PROJECT="${PROJECT:-}"
REGION="${REGION:-us-central1}"
CLUSTER_NAME="${CLUSTER_NAME:-mio-poc}"
NAMESPACE="${NAMESPACE:-mio}"
GCS_BUCKET="${GCS_BUCKET:-mio-messages-poc}"
GHCR_USER="${GHCR_USER:-}"
GHCR_EMAIL="${GHCR_EMAIL:-}"
NODE_MACHINE_TYPE="${NODE_MACHINE_TYPE:-e2-small}"
MIN_NODES="${MIN_NODES:-1}"
MAX_NODES="${MAX_NODES:-3}"

if [ -z "$PROJECT" ]; then
  echo "ERROR: PROJECT env var is required (GCP project ID)"
  exit 1
fi

if [ -z "${GHCR_PAT:-}" ]; then
  echo "ERROR: GHCR_PAT env var is required (GitHub PAT with read:packages)"
  exit 1
fi

if [ -z "$GHCR_USER" ]; then
  echo "ERROR: GHCR_USER env var is required (GitHub username for ghcr.io pull)"
  exit 1
fi

if [ -z "$GHCR_EMAIL" ]; then
  echo "ERROR: GHCR_EMAIL env var is required (email for docker-registry pull secret)"
  exit 1
fi

SINK_GSA="mio-sink-gcs@${PROJECT}.iam.gserviceaccount.com"
WORKLOAD_POOL="${PROJECT}.svc.id.goog"

echo "==> Project:  $PROJECT"
echo "==> Region:   $REGION"
echo "==> Cluster:  $CLUSTER_NAME"
echo "==> Namespace: $NAMESPACE"

# ── 1. GKE cluster ────────────────────────────────────────────────────────────
echo ""
echo "==> [1/9] Creating GKE cluster (3-zone regional, Workload Identity enabled)..."
gcloud container clusters create "$CLUSTER_NAME" \
  --project="$PROJECT" \
  --region="$REGION" \
  --machine-type="$NODE_MACHINE_TYPE" \
  --num-nodes=1 \
  --min-nodes="$MIN_NODES" \
  --max-nodes="$MAX_NODES" \
  --enable-autoscaling \
  --workload-pool="$WORKLOAD_POOL" \
  --release-channel=regular \
  --no-enable-basic-auth \
  --enable-ip-alias \
  2>/dev/null || echo "  (cluster already exists — skipping)"

gcloud container clusters get-credentials "$CLUSTER_NAME" \
  --project="$PROJECT" \
  --region="$REGION"

# ── 2. Namespace ──────────────────────────────────────────────────────────────
echo ""
echo "==> [2/9] Creating namespace $NAMESPACE..."
kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

# ── 3. Cloud SQL Postgres 16 (private IP) ─────────────────────────────────────
echo ""
echo "==> [3/9] Provisioning Cloud SQL Postgres 16 (private IP via PSA)..."
# NOTE: Private Service Access + VPC peering must be pre-configured for the
# default VPC. Auth Proxy sidecar deferred to P8.
# This step is a reminder — full SQL provisioning is manual for POC.
echo "  TODO (manual): gcloud sql instances create mio-postgres --database-version=POSTGRES_16 ..."
echo "  TODO (manual): configure private IP + VPC peering + create mio_app role + mio DB"

# ── 4. GCS bucket ─────────────────────────────────────────────────────────────
echo ""
echo "==> [4/9] Creating GCS bucket $GCS_BUCKET..."
gsutil mb -p "$PROJECT" -l "$REGION" "gs://${GCS_BUCKET}" 2>/dev/null || echo "  (bucket exists — skipping)"

# Lifecycle: Standard → Nearline @30d → Coldline @90d
gsutil lifecycle set - "gs://${GCS_BUCKET}" <<'EOF'
{
  "rule": [
    {
      "action": {"type": "SetStorageClass", "storageClass": "NEARLINE"},
      "condition": {"age": 30, "matchesStorageClass": ["STANDARD"]}
    },
    {
      "action": {"type": "SetStorageClass", "storageClass": "COLDLINE"},
      "condition": {"age": 90, "matchesStorageClass": ["NEARLINE"]}
    }
  ]
}
EOF
echo "  Lifecycle rules applied."

# ── 5. GCP service account for sink-gcs ───────────────────────────────────────
echo ""
echo "==> [5/9] Creating GSA $SINK_GSA..."
gcloud iam service-accounts create mio-sink-gcs \
  --project="$PROJECT" \
  --display-name="MIO sink-gcs GCS writer" \
  2>/dev/null || echo "  (GSA exists — skipping)"

gsutil iam ch "serviceAccount:${SINK_GSA}:roles/storage.objectAdmin" "gs://${GCS_BUCKET}"
echo "  Granted roles/storage.objectAdmin on $GCS_BUCKET."

# ── 6. Workload Identity binding (GSA → KSA) ──────────────────────────────────
echo ""
echo "==> [6/9] Binding GSA to KSA mio-sink-gcs in namespace $NAMESPACE..."
gcloud iam service-accounts add-iam-policy-binding "$SINK_GSA" \
  --project="$PROJECT" \
  --role="roles/iam.workloadIdentityUser" \
  --member="serviceAccount:${WORKLOAD_POOL}[${NAMESPACE}/mio-sink-gcs]" \
  2>/dev/null || echo "  (binding already exists — skipping)"

# ── 7. IAM propagation wait + verification ────────────────────────────────────
echo ""
echo "==> [7/9] Waiting 60s for IAM propagation..."
sleep 60

echo "  Running gsutil verification pod via KSA mio-sink-gcs..."
kubectl run wi-verify \
  --image=google/cloud-sdk:alpine \
  --restart=Never \
  --rm \
  --namespace="$NAMESPACE" \
  --serviceaccount=mio-sink-gcs \
  --command -- \
  gsutil ls "gs://${GCS_BUCKET}" && echo "  WI verification: OK" || {
    echo "ERROR: Workload Identity verification failed. Check IAM binding and propagation."
    exit 1
  }

# ── 8. imagePullSecret for GHCR ───────────────────────────────────────────────
echo ""
echo "==> [8/9] Creating ghcr-pull imagePullSecret in namespace $NAMESPACE..."
# PAT must have read:packages scope on crashchat-ai/mio only, 6-month expiry.
# Rotation: see deploy/gke/README.md.
kubectl create secret docker-registry ghcr-pull \
  --namespace="$NAMESPACE" \
  --docker-server=ghcr.io \
  --docker-username="$GHCR_USER" \
  --docker-password="$GHCR_PAT" \
  --docker-email="$GHCR_EMAIL" \
  --dry-run=client -o yaml | kubectl apply -f -

# ── 9. Install Helm charts ────────────────────────────────────────────────────
echo ""
echo "==> [9/9] Installing Helm charts in dependency order..."

helm repo add nats https://nats-io.github.io/k8s/helm/charts/ 2>/dev/null || true
helm repo update

# 9a. mio-nats (JetStream cluster)
echo "  Installing mio-nats..."
helm dependency update deploy/charts/mio-nats
helm upgrade --install mio-nats deploy/charts/mio-nats \
  --namespace="$NAMESPACE" \
  --wait \
  --timeout=5m

# 9b. mio-jetstream-bootstrap (verify-only post-install hook)
# NOTE: Gateway + sink must be running so streams/consumers exist before hook fires.
# Install gateway first, then bootstrap to verify.
echo "  Installing mio-gateway..."
helm upgrade --install mio-gateway deploy/charts/mio-gateway \
  --namespace="$NAMESPACE" \
  --set "serviceAccount.gcpServiceAccount=" \
  --wait \
  --timeout=5m

echo "  Installing mio-sink-gcs..."
helm upgrade --install mio-sink-gcs deploy/charts/mio-sink-gcs \
  --namespace="$NAMESPACE" \
  --set "serviceAccount.gcpServiceAccount=${SINK_GSA}" \
  --wait \
  --timeout=5m

echo "  Installing mio-jetstream-bootstrap (verify-only post-install hook)..."
helm upgrade --install mio-jetstream-bootstrap deploy/charts/mio-jetstream-bootstrap \
  --namespace="$NAMESPACE" \
  --wait \
  --timeout=3m

# ── Final assertion ────────────────────────────────────────────────────────────
echo ""
echo "==> Final assertion: JetStream quorum check..."
kubectl exec -n "$NAMESPACE" mio-nats-0 -- \
  nats --server=nats://localhost:4222 stream cluster info MESSAGES_INBOUND \
  2>&1 | grep -c "current: true" | \
  xargs -I{} sh -c '[ {} -ge 3 ] && echo "QUORUM OK ({})/3 peers current" || (echo "ERROR: quorum not healthy ({}/3)"; exit 1)'

echo ""
echo "==> Setup complete. MIO is running in namespace $NAMESPACE."
echo "    Next: P8 — configure DNS, cert-manager prod issuer, and run live Cliq webhook."
