# bq-loader

Hourly Cloud Run Job that materialises `raw_mio.messages` from the GCS NDJSON
sink. Triggered by Cloud Scheduler (`5 * * * *`); runs in <5 min per hour-window.

See `plans/260510-1102-bq-sink-lakehouse/` for the full plan and decisions.

## What it does

1. Creates a temporary EXTERNAL staging table over the windowed NDJSON URIs.
2. Runs `sql/validate.sql` — quarantines rows that fail invariants (missing
   timestamp, empty dedup-key fields, unknown `conversation_kind`) into
   `${DATASET}.messages_errors`.
3. Runs `sql/merge.sql` — partition-bounded `MERGE` into `${DATASET}.messages`
   using `(account_id, source_message_id)` as the dedup key.
4. Drops the staging table (also unconditionally on EXIT).
5. Emits structured Cloud Logging entry with `loader_run_status`.

## Files

| File | Purpose |
|---|---|
| `Dockerfile` | Image based on `cloudsdktool/google-cloud-cli:slim` + `gettext-base` for `envsubst`. |
| `load.sh` | Entry point — orchestrates staging → validate → merge → cleanup. |
| `sql/validate.sql` | Inserts bad rows into `messages_errors`. |
| `sql/merge.sql` | Inserts good rows into `messages` (NOT MATCHED only). |
| `test/fixtures/{good,bad}.ndjson` | Sample rows for the integration test. |
| `test/run-integration.sh` | Smoke test against a sandbox dataset (auth required). |

## Build + push

Build context is the **repo root** (so the Dockerfile can copy
`sink-gcs/sql/messages_schema.json` into the image — single source of truth).
Registry is **GHCR** — same as every other mio service.

```bash
cd <repo-root>
SHA=$(git rev-parse HEAD)
docker build -f tools/bq-loader/Dockerfile \
  -t ghcr.io/crashchat-ai/mio/bq-loader:${SHA} \
  -t ghcr.io/crashchat-ai/mio/bq-loader:main .
docker push ghcr.io/crashchat-ai/mio/bq-loader:${SHA}
docker push ghcr.io/crashchat-ai/mio/bq-loader:main
```

CI does this automatically on `main` push (`build-bq-loader` job in
`.github/workflows/ci.yaml`). Terragrunt pins `:<sha>` for explicit rollouts.

**Cross-cloud note:** the image lives in GHCR (GitHub) and runs on GCP Cloud
Run. Either pass GHCR pull credentials in the Cloud Run Job spec, or mirror
through Artifact Registry (preferred — avoids embedding a PAT in GCP secrets).
Mirror setup is a one-time infra task — see `plans/260510-1102-bq-sink-lakehouse/infra-handoff.md`.

## Run

The image is invoked by Cloud Run Job — env vars come from the terragrunt
module (`infra/terraform/gcp/{dev,prod}/bigquery-loader/`):

| Env var | Meaning | Default |
|---|---|---|
| `PROJECT_ID` | GCP project that owns the dataset | required |
| `DATASET` | BQ dataset name | required (`raw_mio`) |
| `BUCKET` | GCS bucket holding NDJSON | required (`ab-spectrum-sensitive-prod`) |
| `PREFIX` | Path prefix below bucket root, with trailing `/` | required (`mio/`) |
| `WINDOW_START` | RFC3339 UTC, inclusive | `previous_full_hour - 15m` |
| `WINDOW_END` | RFC3339 UTC, exclusive | `previous_full_hour` |
| `MODE` | `incremental` \| `backfill` \| free-form (log tag only) | `incremental` |

### Manual invocation (op runbook)

```bash
gcloud run jobs execute bq-loader \
  --region=us-central1 \
  --update-env-vars=WINDOW_START=2026-05-10T11:00:00Z,WINDOW_END=2026-05-10T12:00:00Z
```

### Backfill (chunk-by-chunk)

Phase 0 Q14 deferred backfill — the loader supports it via
`MODE=backfill` + custom `WINDOW_START`/`WINDOW_END`, but no driver script ships
in this PR. If backfill becomes necessary later, run hour-by-hour or day-by-day
sequentially (concurrent runs collide on the MERGE target).

## Concurrency model

The Cloud Run Job is configured `max_instances=1`, `parallelism=1`. A manual
backfill run blocks the scheduled hourly run instead of racing it. Failures
let Cloud Scheduler retry on the next cron tick — the partition-bounded MERGE
is idempotent.

## Observability

Each run emits one structured log line on stdout:

```json
{"severity":"INFO","loader_run_status":"success","mode":"incremental","window_start":"...","window_end":"...","staging":"...","message":"complete"}
```

Cloud Monitoring alerts (Phase 6):
- Cloud Run Job execution failed in last 75 min.
- No `loader_run_status=success` log entry in last 90 min (catches Scheduler not firing).

Notification channel: existing `GCHAT_WEBHOOK_URL` (Google Chat).

## Schema evolution

When `proto/mio/v1/*.proto` adds a field:

1. Update the proto.
2. Update `sink-gcs/sql/messages_schema.json`.
3. Update column lists in `sink-gcs/sql/messages_native.sql` and
   `sink-gcs/sql/external_table.sql`.
4. The loader inherits the change automatically (`merge.sql` selects fields
   by name from the schema-driven staging table).
5. Apply on existing partitions: pass
   `--schema_update_option=ALLOW_FIELD_ADDITION` to `bq load` if needed.
6. CI gate: `tools/bq-loader/ci/check-schema-drift.sh` (Phase 6) fails the PR
   if proto fields outpace `messages_schema.json`.

## Local testing

```bash
export TEST_PROJECT_ID=dp-dev   # any non-prod project
export TEST_BUCKET=...           # bucket your ADC SA can write to
./test/run-integration.sh
```

The script provisions an ephemeral dataset, uploads fixtures, runs the loader,
asserts row counts, and tears everything down on EXIT.

CI wiring: deferred — needs a CI service account + dedicated test bucket. Once
provisioned, add a `bq-loader-integration` job to `.github/workflows/ci.yaml`
that runs this script on PRs touching `tools/bq-loader/**` or `sink-gcs/sql/**`.
