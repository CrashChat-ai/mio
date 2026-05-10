# syntax=docker/dockerfile:1.7
# Multi-stage distroless build for mio-attachment-downloader.
# Mirrors sink-gcs/Dockerfile: build context = repo root so proto/gen/go
# (resolved via the `replace github.com/crashchat-ai/mio => ..` directive) is
# accessible. proto/gen/go must be included via .dockerignore negation.

FROM golang:1.25-alpine AS builder

ARG BUILD_VERSION=dev

RUN apk add --no-cache ca-certificates git

WORKDIR /src

COPY go.mod go.sum ./
COPY sdk-go/go.mod sdk-go/go.sum ./sdk-go/
COPY attachment-downloader/go.mod attachment-downloader/go.sum ./attachment-downloader/

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    cd attachment-downloader && go mod download

COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    cd attachment-downloader && go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${BUILD_VERSION}" \
      -o /out/attachment-downloader \
      ./cmd/attachment-downloader && \
    go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/mio-attachment-cli \
      ./cmd/mio-attachment-cli

FROM gcr.io/distroless/static-debian12:nonroot

USER 65532:65532

COPY --from=builder /out/attachment-downloader /attachment-downloader
COPY --from=builder /out/mio-attachment-cli /mio-attachment-cli

EXPOSE 9090

ENTRYPOINT ["/attachment-downloader"]
