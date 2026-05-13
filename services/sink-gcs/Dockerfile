# syntax=docker/dockerfile:1.7
# Multi-stage distroless build for mio-sink-gcs.
# Build context = repo root. sink-gcs is now services/sink-gcs/ in the
# consolidated root module after Option B layout migration.

# ── Stage 1: builder ────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

ARG BUILD_VERSION=dev

RUN apk add --no-cache ca-certificates git

WORKDIR /src

# Copy go.mod files first for layer-cache reuse on dep changes.
COPY go.mod go.sum ./
COPY sdks/go/go.mod sdks/go/go.sum ./sdks/go/

# Download dependencies with build cache mounts.
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy full source (proto/gen/go/ included via .dockerignore negation).
COPY . .

# Build static binary from root module.
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${BUILD_VERSION}" \
      -o /out/sink-gcs \
      ./services/sink-gcs/cmd/sink

# ── Stage 2: runtime (distroless, nonroot) ───────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot

# USER 65532 is the nonroot user in distroless/static-debian12:nonroot.
USER 65532:65532

COPY --from=builder /out/sink-gcs /sink-gcs

# Health endpoint exposed on 8080 (wired in P7 Helm chart).
EXPOSE 8080

ENTRYPOINT ["/sink-gcs"]
