package sdk

import (
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace"
)

// Client wraps a NATS connection and a JetStream v2 handle.
//
// SDK does NOT create streams or consumers — gateway startup (P3) is the
// authoritative provisioner. The SDK only publishes to existing streams and
// attaches to existing durable consumers.
type Client struct {
	nc      *nats.Conn
	js      jetstream.JetStream
	metrics *Metrics
	tp      trace.TracerProvider

	// defaults for consume options
	maxAckPending int
	ackWait       time.Duration
}

// Option is a functional option for Client construction.
type Option func(*clientConfig)

type clientConfig struct {
	name              string
	credFile          string
	tracerProvider    trace.TracerProvider
	metricsRegisterer prometheus.Registerer
	maxAckPending     int
	ackWait           time.Duration
}

func defaultConfig() *clientConfig {
	return &clientConfig{
		maxAckPending: 1,
		ackWait:       30 * time.Second,
	}
}

// WithName sets the NATS client name (visible in server monitoring).
func WithName(name string) Option {
	return func(c *clientConfig) { c.name = name }
}

// WithCreds sets the path to a NATS credentials file (.creds).
func WithCreds(path string) Option {
	return func(c *clientConfig) { c.credFile = path }
}

// WithTracerProvider sets the OTel tracer provider. Defaults to the global provider.
func WithTracerProvider(tp trace.TracerProvider) Option {
	return func(c *clientConfig) { c.tracerProvider = tp }
}

// WithMetricsRegistry sets the Prometheus registerer. Defaults to the default registry.
func WithMetricsRegistry(reg prometheus.Registerer) Option {
	return func(c *clientConfig) { c.metricsRegisterer = reg }
}

// WithMaxAckPending sets the max-ack-pending for pull consumers (default: 1).
// MaxAckPending=1 enforces per-conversation ordering at POC scale.
// Graduation path: shard by subject prefix when load-test forces it.
func WithMaxAckPending(n int) Option {
	return func(c *clientConfig) { c.maxAckPending = n }
}

// WithAckWait sets the ack-wait timeout for pull consumers (default: 30s).
func WithAckWait(d time.Duration) Option {
	return func(c *clientConfig) { c.ackWait = d }
}

// New connects to NATS and returns a ready Client.
//
// Uses the v2 jetstream package (NOT the deprecated nc.JetStream() / JetStreamContext).
// Callers must call Close() when done to drain the connection.
func New(url string, opts ...Option) (*Client, error) {
	cfg := defaultConfig()
	for _, o := range opts {
		o(cfg)
	}

	// Build nats.Options slice.
	natsOpts := []nats.Option{}
	if cfg.name != "" {
		natsOpts = append(natsOpts, nats.Name(cfg.name))
	}
	if cfg.credFile != "" {
		natsOpts = append(natsOpts, nats.UserCredentials(cfg.credFile))
	}

	nc, err := nats.Connect(url, natsOpts...)
	if err != nil {
		return nil, fmt.Errorf("sdk: nats connect: %w", err)
	}

	// v2 JetStream handle — do NOT use nc.JetStream() (legacy JetStreamContext).
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("sdk: jetstream.New: %w", err)
	}

	// Metrics registerer fallback.
	reg := cfg.metricsRegisterer
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	m, err := newMetrics(reg)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("sdk: register metrics: %w", err)
	}

	// Tracer provider fallback.
	tp := cfg.tracerProvider
	if tp == nil {
		tp = noopTracerProvider()
	}

	return &Client{
		nc:            nc,
		js:            js,
		metrics:       m,
		tp:            tp,
		maxAckPending: cfg.maxAckPending,
		ackWait:       cfg.ackWait,
	}, nil
}

// Close drains and closes the underlying NATS connection.
// After Close, any in-flight publish may or may not have been acknowledged.
func (c *Client) Close() {
	_ = c.nc.Drain()
}
