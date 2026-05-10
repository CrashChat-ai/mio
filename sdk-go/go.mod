module github.com/crashchat-ai/mio/sdk-go

go 1.25.0

require (
	github.com/crashchat-ai/mio v0.0.0
	github.com/nats-io/nats.go v1.52.0
	github.com/prometheus/client_golang v1.23.2
	github.com/prometheus/client_model v0.6.2
	go.opentelemetry.io/otel v1.43.0
	go.opentelemetry.io/otel/trace v1.43.0
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nats-io/nkeys v0.4.15 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	golang.org/x/crypto v0.49.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
)

// Local replace: required so sdk-go can import the unpublished root module
// (proto/gen/go/mio/v1). The require line uses placeholder v0.0.0; without
// this replace, Go tries to fetch a non-existent pseudo-version remotely.
// go.work alone does NOT substitute placeholder versions for workspace members.
// Non-transitive: never leaks to downstream consumers.
replace github.com/crashchat-ai/mio => ..
