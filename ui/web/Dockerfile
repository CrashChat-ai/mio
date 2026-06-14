# syntax=docker/dockerfile:1.7

FROM bufbuild/buf:1.70.0 AS proto
WORKDIR /src
COPY buf.yaml buf.gen.yaml ./
COPY proto/ ./proto/
RUN buf generate proto

FROM golang:1.26-bookworm AS go-builder
WORKDIR /src

COPY go.mod go.sum ./
COPY sdks/go/go.mod sdks/go/go.sum ./sdks/go/
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
COPY --from=proto /src/proto/gen/ ./proto/gen/
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go build -trimpath -ldflags="-s -w" -o /out/mio-web ./ui/web/cmd/mio-web

FROM gcr.io/distroless/base-debian12:nonroot
WORKDIR /
COPY --from=go-builder /out/mio-web /mio-web
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/mio-web"]
