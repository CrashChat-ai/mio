# Minimal Cloud SDK image for the hourly bq-loader Cloud Run Job.
# Uses bq + gsutil + gcloud only — no application code beyond shell + SQL.
#
# Build context: REPO ROOT (so we can COPY both tools/bq-loader/* and
# sink-gcs/sql/messages_schema.json into the image — keeping a single source
# of truth for the schema).
#
#   docker build -f tools/bq-loader/Dockerfile -t bq-loader:dev .
FROM gcr.io/google.com/cloudsdktool/google-cloud-cli:slim

WORKDIR /loader

COPY tools/bq-loader/load.sh /loader/load.sh
COPY tools/bq-loader/sql/ /loader/sql/
COPY sink-gcs/sql/messages_schema.json /loader/sql/messages_schema.json

RUN chmod +x /loader/load.sh && apt-get update && apt-get install -y --no-install-recommends gettext-base && rm -rf /var/lib/apt/lists/*

# Cloud Run Job sets PROJECT_ID, DATASET, BUCKET, PREFIX as env vars.
# WINDOW_START / WINDOW_END default to "previous full hour with 15-min overlap"
# inside load.sh.
ENTRYPOINT ["/loader/load.sh"]
