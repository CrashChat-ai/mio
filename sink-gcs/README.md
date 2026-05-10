# mio-sink-gcs

Archival consumer: pulls from `MESSAGES_INBOUND` JetStream stream and writes
partitioned NDJSON to GCS (production) or MinIO (local dev).

## Partition path

```
gs://mio-messages/channel_type=<slug>/date=YYYY-MM-DD/<consumer-id>-<seq-start>-<seq-end>.ndjson
```

- `channel_type=<slug>` — Hive-style key for BigQuery external table partition discovery.  
  Slug value is the `proto/channels.yaml` registry value (`zoho_cliq`, `slack`, …).  
  **Never** the URL-slug form (`zoho-cliq` — that's for webhook routes only).
- `date=YYYY-MM-DD` — UTC date from `msg.received_at`, not wall-clock at write time.
- Extension `.ndjson` — newline-delimited JSON; BQ understands it natively.

## Filename scheme (offset-based)

```
<consumer-id>-<seq-start>-<seq-end>.ndjson
```

Example: `gcs-archiver-1000-1063.ndjson` (records 1000–1063 from the stream).

**Why offset-based, not timestamp-based:**

- Two pods consuming the same durable receive non-overlapping JetStream sequence
  ranges by construction — collision is impossible.
- Pod restart safe: sequences come from JetStream state, not per-pod counters.
- Replay-friendly: locate any record by stream sequence with one `ls`.

## Slug-drift rule

The partition path uses `channel_type` exactly as it arrives on the wire.
The sink **never** rewrites or normalises slugs.

If a channel slug is deprecated (e.g. `zalo_oa` → `zalo_oauth`), old partitions
stay forever under `channel_type=zalo_oa/`. Queries spanning a rename must use:

```sql
WHERE channel_type IN ('zalo_oa', 'zalo_oauth')
```

## Multi-pod safety

Offset-based naming is mandatory before P7 multi-replica deployment. The
concurrent two-pod integration test (`TestMinIO_TwoPods_NonOverlappingRanges`)
asserts pairwise non-overlapping `<seq-start..seq-end>` ranges across pods.

## Local dev

```bash
# Start MinIO (part of docker compose infra):
make up

# Run sink-gcs locally against MinIO:
SINK_BACKEND=minio SINK_BUCKET=mio-messages \
SINK_ENDPOINT=http://localhost:9000 \
SINK_ACCESS_KEY=minioadmin SINK_SECRET_KEY=minioadmin \
NATS_URL=nats://localhost:4222 \
go run ./cmd/sink
```

## BigQuery dedup

JetStream pull is at-least-once; the sink does no runtime dedup. Standard view:

```sql
SELECT * FROM `<project>.<dataset>.messages`
QUALIFY ROW_NUMBER() OVER (PARTITION BY id ORDER BY received_at DESC) = 1;
```

## Upgrade trigger

Switch from NDJSON to Parquet when archive cost exceeds **$1 500/mo or ≥ 50 GB/day**.
