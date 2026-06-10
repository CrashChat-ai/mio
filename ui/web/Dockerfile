# syntax=docker/dockerfile:1.7

FROM node:26-bookworm-slim AS web
WORKDIR /src/ui/web/app

RUN corepack enable && corepack prepare pnpm@10.33.2 --activate
COPY ui/web/app/package.json ui/web/app/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

COPY ui/web/app/ ./
RUN pnpm build

FROM bufbuild/buf:1.69.0 AS proto
WORKDIR /src
COPY buf.yaml buf.gen.yaml ./
COPY proto/ ./proto/
RUN buf generate proto

FROM golang:1.25-bookworm AS go-builder
WORKDIR /src

COPY go.mod go.sum ./
COPY sdks/go/go.mod sdks/go/go.sum ./sdks/go/
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
COPY --from=proto /src/proto/gen/ ./proto/gen/
RUN rm -rf ui/web/internal/embed/dist && mkdir -p ui/web/internal/embed/dist
COPY --from=web /src/ui/web/app/dist/ ./ui/web/internal/embed/dist/
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go build -trimpath -ldflags="-s -w" -o /out/mio-web ./ui/web/cmd/mio-web

FROM gcr.io/distroless/base-debian12:nonroot
WORKDIR /
COPY --from=go-builder /out/mio-web /mio-web
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/mio-web"]
