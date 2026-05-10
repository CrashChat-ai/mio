# syntax=docker/dockerfile:1.7
# Multi-stage distroless build for mio-sink-gcs.
# Same pattern as gateway/Dockerfile (per master Revisions 11:53).
# Build context is repo root so proto/gen/go and go.mod replace directives
# (../proto via replace github.com/crashchat-ai/mio => ..) resolve correctly.
#
# proto/gen/go/ is included via !proto/gen/go/ negation in .dockerignore.

# ── Stage 1: builder ────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

ARG BUILD_VERSION=dev

RUN apk add --no-cache ca-certificates git

WORKDIR /src

# Copy go.mod files first for layer-cache reuse on dep changes.
COPY go.mod go.sum ./
COPY sink-gcs/go.mod sink-gcs/go.sum ./sink-gcs/

# Download dependencies with build cache mounts.
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    cd sink-gcs && go mod download

# Copy full source (proto/gen/go/ included via .dockerignore negation).
COPY . .

# Build static binary (cd into sink-gcs module so go.mod resolves correctly).
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    cd sink-gcs && go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${BUILD_VERSION}" \
      -o /out/sink-gcs \
      ./cmd/sink

# ── Stage 2: runtime (distroless, nonroot) ───────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot

# USER 65532 is the nonroot user in distroless/static-debian12:nonroot.
USER 65532:65532

COPY --from=builder /out/sink-gcs /sink-gcs

# Health endpoint exposed on 8080 (wired in P7 Helm chart).
EXPOSE 8080

ENTRYPOINT ["/sink-gcs"]
