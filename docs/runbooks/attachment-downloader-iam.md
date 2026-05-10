# Attachment-downloader IAM (terragrunt-managed)

The `mio-attachment-downloader` sidecar's GCP service account, IAM
bindings, and bucket lifecycle rules are all managed by terragrunt in
the `infra` repo. There is no manual gcloud step.

## Source of truth (`infra` repo)

| Resource | File |
|---|---|
| GSA `prod-mio-attachments-sa@...` + WI binding + self-impersonation | `terraform/gcp/prod/service-accounts/service-accounts.yaml` |
| Bucket prefix-scoped IAM binding (`storage.objectAdmin` on `mio/attachments/`) | `terraform/gcp/prod/gcs/buckets.yaml` |
| Bucket lifecycle rule (7d expiry on `mio/attachments/`) | `terraform/gcp/prod/gcs/buckets.yaml` |
| Workload-Identity GSA reference in HelmRelease | `fluxcd/apps/prod/mio/release-attachment-downloader.yaml` |

## Apply changes

```bash
cd infra/terraform/gcp/prod/service-accounts && terragrunt apply
cd infra/terraform/gcp/prod/gcs              && terragrunt apply
```

The HelmRelease in `fluxcd/apps/prod/mio/` is reconciled by Flux on a
30m interval, or force a re-reconcile:

```bash
flux reconcile source git flux-system
flux reconcile kustomization apps -n flux-system
flux reconcile helmrelease mio-attachment-downloader -n mio
```

## Verify

```bash
# KSA carries the right WI annotation
kubectl -n mio get sa mio-attachment-downloader -o yaml | grep iam.gke.io
# expected: iam.gke.io/gcp-service-account: prod-mio-attachments-sa@dp-prod-7e26.iam.gserviceaccount.com

# GSA exists with self-impersonation + WI binding
gcloud iam service-accounts get-iam-policy \
  prod-mio-attachments-sa@dp-prod-7e26.iam.gserviceaccount.com

# Bucket binding (look for the prefix-scoped condition)
gcloud storage buckets get-iam-policy gs://ab-spectrum-backups-prod \
  --format=json | jq '.bindings[] | select(.members[] | contains("mio-attachments"))'
```

## What if the operator has no terragrunt access?

Run the runbook against a separate GCP project / separate prefix on a
dev bucket. Do not bypass terragrunt for prod — drift between manual
gcloud changes and terragrunt-managed state will be reverted on the
next apply.
