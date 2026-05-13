package storage

import (
	"context"
	"fmt"
	"os"
)

// Backend selection env vars.
const (
	envBackend = "MIO_STORAGE_BACKEND"
	envBucket  = "MIO_STORAGE_BUCKET"
)

// Factory is the constructor a backend package registers itself with.
type Factory func(ctx context.Context, bucket string) (Storage, error)

var registry = map[string]Factory{}

// Register makes a backend available to the env-driven New() dispatcher.
// Backend packages call this from init().
func Register(name string, f Factory) { registry[name] = f }

// New returns a Storage backend chosen by env vars:
//
//	MIO_STORAGE_BACKEND = "gcs" | "s3"   (default "gcs")
//	MIO_STORAGE_BUCKET  = bucket name    (required)
func New(ctx context.Context) (Storage, error) {
	name := os.Getenv(envBackend)
	if name == "" {
		name = "gcs"
	}
	bucket := os.Getenv(envBucket)
	if bucket == "" {
		return nil, fmt.Errorf("storage: %s is required", envBucket)
	}
	f, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("storage: unknown or unregistered backend %q", name)
	}
	return f(ctx, bucket)
}
