// Package storage defines the backend-agnostic interface for persisting
// attachment bytes. Implementations live under sub-packages (gcs, s3, ...).
package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

// Object describes a stored object's metadata (returned by Stat / List).
//
// TenantID / ConversationID / SourceMessageID are forward-only — they are
// populated only on objects written after the metadata-enrichment rollout,
// so an empty value means "unknown", not "non-match", to GDPR filters.
// Dedup hazard: media-vault writes with IfNotExists=true on a content-hash
// key, so if two messages with different owner identifiers carry the same
// bytes, only the first writer's identifiers are stamped — see
// docs/runbooks/attachment-gdpr-delete.md for operator guidance.
type Object struct {
	Key             string
	Size            int64
	ContentType     string
	SHA256Hex       string
	TenantID        string
	AccountID       string
	ConversationID  string
	SourceMessageID string
	ModifiedAt      time.Time
}

// PutOptions controls write behaviour.
type PutOptions struct {
	ContentType string
	SHA256Hex   string
	// IfNotExists: GCS DoesNotExist precondition / S3 If-None-Match.
	IfNotExists     bool
	TenantID        string
	AccountID       string
	ConversationID  string
	SourceMessageID string
}

// SignOptions controls signed-URL issuance.
type SignOptions struct {
	TTL    time.Duration
	Method string // "GET" only for POC
	// ResponseContentDisposition lets us force download filenames cross-backend.
	ResponseContentDisposition string
}

// LifecycleRule is the minimal cross-backend abstraction:
// "expire objects older than N days under prefix P".
type LifecycleRule struct {
	Prefix  string
	AgeDays int
}

// Storage is the contract every backend implements.
type Storage interface {
	Put(ctx context.Context, key string, body io.Reader, size int64, opts PutOptions) error
	Get(ctx context.Context, key string) (io.ReadCloser, *Object, error)
	Stat(ctx context.Context, key string) (*Object, error)
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) (<-chan Object, <-chan error)
	SignedURL(ctx context.Context, key string, opts SignOptions) (string, error)
	SetLifecycle(ctx context.Context, rules []LifecycleRule) error
	Backend() string
}

// Sentinel errors. Backend impls wrap concrete errors with these.
var (
	ErrNotFound      = errors.New("storage: not found")
	ErrAlreadyExists = errors.New("storage: already exists")
	ErrUnsupported   = errors.New("storage: unsupported by backend")
)
