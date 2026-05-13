# syntax=docker/dockerfile:1.7
# Build context: repo root. Root go.mod is the single consolidated module
# after Option B layout migration; gateway is now a sub-package, not its
# own module. sdks/go remains a separate module (resolved via replace).

# ── Stage 1: build ──────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

# Install ca-certificates for TLS in tests; git for VCS stamping.
RUN apk add --no-cache ca-certificates git

WORKDIR /src

# Copy go.mod files first for better layer cache reuse.
COPY go.mod go.sum ./
COPY sdks/go/go.mod sdks/go/go.sum ./sdks/go/

# Download deps with build cache mounts (warm build: ~5s vs ~60s cold).
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy full source tree.
COPY . .

ARG BUILD_VERSION=dev

# Build static binary from root module (gateway is now services/gateway/).
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux \
    go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${BUILD_VERSION}" \
      -o /gateway \
      ./services/gateway/cmd/gateway

# ── Stage 2: runtime ────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot

# UID 65532 = nonroot in distroless.
USER 65532

COPY --from=builder /gateway /gateway

EXPOSE 8080

ENTRYPOINT ["/gateway"]
