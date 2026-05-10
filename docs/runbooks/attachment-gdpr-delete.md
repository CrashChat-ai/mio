# Attachment GDPR delete

Right-to-erasure runbook for purging all persisted attachments belonging to
a single account from object storage.

## When to run

- Customer offboard requested via legal channel
- GDPR Article 17 right-to-erasure request
- Internal data-cleanup exercise

## Pre-flight

1. Identify the account UUID. Confirm via the operator portal it matches
   the requesting tenant.
2. Freeze the account at the gateway level (separate runbook) so no new
   attachments arrive during the sweep — otherwise dry-run vs execute can
   miss in-flight writes.

## Dry-run (always first)

```bash
kubectl -n mio run cli --rm -it --restart=Never \
  --image=ghcr.io/crashchat-ai/mio/media-vault:<sha> \
  --serviceaccount=mio-media-vault \
  --command -- /mio-media-cli delete \
    --account_id=<UUID> \
    --prefix=mio/attachments/ \
    --dry-run
```

Output: `listed=N matched=K deleted=0 dry_run=true`. Sanity-check K against
expected counts.

## Execute

Drop `--dry-run`:

```bash
kubectl -n mio run cli --rm -it --restart=Never \
  --image=ghcr.io/crashchat-ai/mio/media-vault:<sha> \
  --serviceaccount=mio-media-vault \
  --command -- /mio-media-cli delete \
    --account_id=<UUID> \
    --prefix=mio/attachments/ \
    --concurrency=16
```

Output: `listed=N matched=K deleted=K dry_run=false`. `deleted` must equal
`matched`; if not, the CLI returns non-zero and a `gdpr: delete <key>` error
is logged.

## Audit

Cloud Logging captures every Delete call under the `mio-attachments` GSA
identity:

```
resource.type="gcs_bucket"
resource.labels.bucket_name="ab-spectrum-backups-prod"
protoPayload.methodName="storage.objects.delete"
protoPayload.authenticationInfo.principalEmail="mio-attachments@dp-prod-7e26.iam.gserviceaccount.com"
```

Cluster log retention is ≥30d.

## Local execution (for developers)

```bash
gcloud auth application-default login
export MIO_STORAGE_BACKEND=gcs
export MIO_STORAGE_BUCKET=ab-spectrum-backups-prod
go run ./cmd/mio-media-cli delete --account_id=<UUID> --dry-run
```

## Notes

- Sweep cost is O(N Stat). For 100k objects under the prefix, expect ≤5min.
- For volumes >1M, build a side-table mapping account_id → keys and read
  from there instead of scanning. Tracked as a P10 enhancement.
- Content-hash dedup means a single image may be referenced by multiple
  accounts. The current CLI deletes the underlying blob even if other
  accounts also reference it; this is acceptable for POC because dedup
  across tenants is rare. If/when relevant, add reference counting.
