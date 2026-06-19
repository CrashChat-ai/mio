# cliq-mock — dev test-double for Zoho Cliq REST + OAuth.
# Build context: this directory (deploy/local/cliq-mock/) — self-contained module,
# no repo-root context needed.
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 go build -trimpath -o /cliq-mock .

FROM alpine:3.20
COPY --from=build /cliq-mock /cliq-mock
EXPOSE 8080
ENTRYPOINT ["/cliq-mock"]
