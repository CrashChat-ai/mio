# syntax=docker/dockerfile:1.7
# Multi-stage distroless build for mio-media-vault.
# Build context = repo root. media-vault is now services/media-vault/ in the
# consolidated root module after Option B layout migration.

FROM golang:1.25-alpine AS builder

ARG BUILD_VERSION=dev

RUN apk add --no-cache ca-certificates git

WORKDIR /src

COPY go.mod go.sum ./
COPY sdks/go/go.mod sdks/go/go.sum ./sdks/go/

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${BUILD_VERSION}" \
      -o /out/media-vault \
      ./services/media-vault/cmd/media-vault && \
    go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/mio-media-cli \
      ./services/media-vault/cmd/mio-media-cli

FROM gcr.io/distroless/static-debian12:nonroot

USER 65532:65532

COPY --from=builder /out/media-vault /media-vault
COPY --from=builder /out/mio-media-cli /mio-media-cli

EXPOSE 9090

ENTRYPOINT ["/media-vault"]
