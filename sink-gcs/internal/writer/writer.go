// Package writer provides a backend-agnostic interface for the GCS archive sink.
//
// The Writer interface is implemented by:
//   - gcs.go  — Google Cloud Storage backend (production)
//   - minio.go — MinIO backend (local dev, S3-compatible)
//
// Backend is selected by the SINK_BACKEND env var ("gcs" or "minio").
// The factory function New() handles selection and construction.
//
// Atomic write pattern:
//  1. Write() appends bytes to an in-memory buffer.
//  2. Flush() writes the inflight object, copies it to the final name,
//     then deletes the inflight object. Only after the final object exists
//     does the caller ack the JetStream messages.
//  3. Close() flushes remaining buffered data then releases resources.
package writer

import (
	"context"
	"fmt"
	"os"
)

// Writer is a single-file archival writer.
// Each instance owns one (partition, filename) pair.
// Implementations must be safe for sequential use from a single goroutine.
type Writer interface {
	// Write appends p to the in-memory buffer.
	Write(p []byte) (n int, err error)

	// Flush finalises the current buffer into a named object:
	//  1. Upload buffer as <objectPrefix>.inflight
	//  2. Copy inflight → <objectPrefix> (atomic at the API)
	//  3. Delete inflight
	// On success the final object exists and the buffer is reset.
	Flush(ctx context.Context, objectPath string) error

	// Close flushes any remaining data then releases backend resources.
	Close(ctx context.Context, objectPath string) error

	// Len returns the number of buffered bytes not yet flushed.
	Len() int
}

// Backend identifies the storage backend.
type Backend string

const (
	BackendGCS   Backend = "gcs"
	BackendMinIO Backend = "minio"
)

// Config holds the storage backend configuration derived from environment.
type Config struct {
	Backend         Backend
	Bucket          string
	// Prefix is an optional path prepended to every object key, normalized so
	// that a non-empty value always ends in "/". Empty means write at bucket root.
	// Example: "mio/" → objects land at gs://<bucket>/mio/channel_type=.../...
	Prefix          string
	Endpoint        string // MinIO endpoint override (e.g. "http://minio:9000")
	AccessKey       string // MinIO access key
	SecretKey       string // MinIO secret key
	UseSSL          bool   // MinIO TLS
	CredentialsFile string // GCS: path to service-account JSON (or "" for ADC)
}

// ConfigFromEnv reads SINK_BACKEND, SINK_BUCKET, SINK_PREFIX, and
// backend-specific vars.
func ConfigFromEnv() (*Config, error) {
	backend := Backend(os.Getenv("SINK_BACKEND"))
	if backend == "" {
		backend = BackendMinIO // default for local dev
	}
	if backend != BackendGCS && backend != BackendMinIO {
		return nil, fmt.Errorf("writer: SINK_BACKEND must be 'gcs' or 'minio', got %q", backend)
	}

	bucket := os.Getenv("SINK_BUCKET")
	if bucket == "" {
		return nil, fmt.Errorf("writer: SINK_BUCKET must not be empty")
	}

	return &Config{
		Backend:         backend,
		Bucket:          bucket,
		Prefix:          normalizePrefix(os.Getenv("SINK_PREFIX")),
		Endpoint:        os.Getenv("SINK_ENDPOINT"),
		AccessKey:       os.Getenv("SINK_ACCESS_KEY"),
		SecretKey:       os.Getenv("SINK_SECRET_KEY"),
		CredentialsFile: os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"),
	}, nil
}

// normalizePrefix trims leading slashes (objects don't start with "/") and
// ensures a single trailing slash so the caller can use simple concatenation
// (`cfg.Prefix + objectPath`). An empty prefix stays empty.
func normalizePrefix(p string) string {
	for len(p) > 0 && p[0] == '/' {
		p = p[1:]
	}
	if p == "" {
		return ""
	}
	if p[len(p)-1] != '/' {
		p += "/"
	}
	return p
}

// New constructs a Writer for the given backend configuration.
// The returned writer owns one in-memory buffer; create one per (partition, file).
func New(ctx context.Context, cfg *Config) (Writer, error) {
	switch cfg.Backend {
	case BackendGCS:
		return newGCSWriter(ctx, cfg)
	case BackendMinIO:
		return newMinIOWriter(ctx, cfg)
	default:
		return nil, fmt.Errorf("writer: unknown backend %q", cfg.Backend)
	}
}
