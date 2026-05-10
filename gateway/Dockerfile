# syntax=docker/dockerfile:1.7
# Build context: repo root (not gateway/) so the single-module layout
# and proto/gen/go are reachable without go.work.

# ── Stage 1: build ──────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

# Install ca-certificates for TLS in tests; git for VCS stamping.
RUN apk add --no-cache ca-certificates git

WORKDIR /src

# Copy go.mod files first for better layer cache reuse.
COPY go.mod go.sum ./
COPY sdk-go/go.mod sdk-go/go.sum ./sdk-go/
COPY gateway/go.mod gateway/go.sum ./gateway/

# Download deps with build cache mounts (warm build: ~5s vs ~60s cold).
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    cd gateway && go mod download

# Copy full source tree.
COPY . .

ARG BUILD_VERSION=dev

# Build static binary. Must cd into the gateway sub-module because the
# root go.mod does not contain gateway/cmd/gateway (separate module).
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    cd gateway && \
    CGO_ENABLED=0 GOOS=linux \
    go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${BUILD_VERSION}" \
      -o /gateway \
      ./cmd/gateway

# ── Stage 2: runtime ────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot

# UID 65532 = nonroot in distroless.
USER 65532

COPY --from=builder /gateway /gateway

EXPOSE 8080

ENTRYPOINT ["/gateway"]
