// Package s3 implements storage.Storage backed by any S3-compatible endpoint
// (AWS S3, MinIO, Cloudflare R2, etc.).
package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/lifecycle"

	"github.com/crashchat-ai/mio/services/media-vault/internal/storage"
)

// Backend implements storage.Storage using the MinIO Go SDK (S3-compatible).
type Backend struct {
	client *minio.Client
	bucket string
}

var _ storage.Storage = (*Backend)(nil)

// Config holds S3 connection settings derived from environment.
type Config struct {
	Endpoint  string // empty → AWS S3
	AccessKey string
	SecretKey string
	UseSSL    bool   // default true
	Region    string // optional; empty lets SDK auto-detect
	Bucket    string
}

// ConfigFromEnv reads the S3 env surface:
//
//	MIO_STORAGE_S3_ENDPOINT   (default: "" → AWS S3)
//	MIO_STORAGE_S3_ACCESS_KEY
//	MIO_STORAGE_S3_SECRET_KEY
//	MIO_STORAGE_S3_USE_SSL    (default: "true")
//	MIO_STORAGE_S3_REGION     (optional)
func ConfigFromEnv(bucket string) (*Config, error) {
	if bucket == "" {
		return nil, errors.New("s3: bucket name is required")
	}
	useSSL := true
	if v := os.Getenv("MIO_STORAGE_S3_USE_SSL"); v == "false" || v == "0" {
		useSSL = false
	}
	return &Config{
		Endpoint:  os.Getenv("MIO_STORAGE_S3_ENDPOINT"),
		AccessKey: os.Getenv("MIO_STORAGE_S3_ACCESS_KEY"),
		SecretKey: os.Getenv("MIO_STORAGE_S3_SECRET_KEY"),
		UseSSL:    useSSL,
		Region:    os.Getenv("MIO_STORAGE_S3_REGION"),
		Bucket:    bucket,
	}, nil
}

// New constructs an S3-backed Storage from the given config.
func New(_ context.Context, cfg *Config) (*Backend, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("s3: bucket name is required")
	}

	endpoint := cfg.Endpoint
	useSSL := cfg.UseSSL

	if endpoint == "" {
		endpoint = "s3.amazonaws.com"
		useSSL = true
	} else {
		endpoint = strings.TrimPrefix(endpoint, "https://")
		if strings.HasPrefix(endpoint, "http://") {
			endpoint = strings.TrimPrefix(endpoint, "http://")
			useSSL = false
		}
	}

	opts := &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: useSSL,
	}
	if cfg.Region != "" {
		opts.Region = cfg.Region
	}

	client, err := minio.New(endpoint, opts)
	if err != nil {
		return nil, fmt.Errorf("s3: new client: %w", err)
	}
	return &Backend{client: client, bucket: cfg.Bucket}, nil
}

// Backend returns the implementation name.
func (b *Backend) Backend() string { return "s3" }

// Put streams body to key. Honours opts.IfNotExists via If-None-Match: *.
func (b *Backend) Put(ctx context.Context, key string, body io.Reader, size int64, opts storage.PutOptions) error {
	putOpts := minio.PutObjectOptions{
		ContentType:  opts.ContentType,
		UserMetadata: buildUserMetadata(opts),
	}
	if opts.IfNotExists {
		putOpts.SetMatchETagExcept("*")
	}

	_, err := b.client.PutObject(ctx, b.bucket, key, body, size, putOpts)
	if err != nil {
		return mapErr(err)
	}
	return nil
}

// Get streams the bytes at key.
func (b *Backend) Get(ctx context.Context, key string) (io.ReadCloser, *storage.Object, error) {
	obj, err := b.client.GetObject(ctx, b.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, nil, mapErr(err)
	}
	info, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		return nil, nil, mapErr(err)
	}
	return obj, infoToObject(key, info), nil
}

// Stat returns metadata without fetching bytes.
func (b *Backend) Stat(ctx context.Context, key string) (*storage.Object, error) {
	info, err := b.client.StatObject(ctx, b.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return nil, mapErr(err)
	}
	return infoToObject(key, info), nil
}

// Delete removes a single object. Idempotent (404 → nil).
func (b *Backend) Delete(ctx context.Context, key string) error {
	err := b.client.RemoveObject(ctx, b.bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		er := minio.ToErrorResponse(err)
		if er.Code == "NoSuchKey" {
			return nil
		}
		return mapErr(err)
	}
	return nil
}

// List enumerates objects under prefix and yields them on the returned channel.
func (b *Backend) List(ctx context.Context, prefix string) (<-chan storage.Object, <-chan error) {
	out := make(chan storage.Object, 32)
	errCh := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errCh)
		for obj := range b.client.ListObjects(ctx, b.bucket, minio.ListObjectsOptions{
			Prefix:    prefix,
			Recursive: true,
		}) {
			if obj.Err != nil {
				errCh <- mapErr(obj.Err)
				return
			}
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			case out <- storage.Object{
				Key:        obj.Key,
				Size:       obj.Size,
				ModifiedAt: obj.LastModified,
			}:
			}
		}
		errCh <- nil
	}()
	return out, errCh
}

// SignedURL issues a presigned GET URL with optional response-content-disposition.
func (b *Backend) SignedURL(ctx context.Context, key string, opts storage.SignOptions) (string, error) {
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = time.Hour
	}
	reqParams := url.Values{}
	if opts.ResponseContentDisposition != "" {
		reqParams.Set("response-content-disposition", opts.ResponseContentDisposition)
	}
	u, err := b.client.PresignedGetObject(ctx, b.bucket, key, ttl, reqParams)
	if err != nil {
		return "", fmt.Errorf("s3: presigned url: %w", err)
	}
	return u.String(), nil
}

// SetLifecycle sets bucket lifecycle rules (best-effort; wraps ErrUnsupported if rejected).
func (b *Backend) SetLifecycle(ctx context.Context, rules []storage.LifecycleRule) error {
	existing, err := b.client.GetBucketLifecycle(ctx, b.bucket)
	if err != nil {
		er := minio.ToErrorResponse(err)
		if er.Code != "NoSuchLifecycleConfiguration" {
			return fmt.Errorf("%w: get lifecycle: %v", storage.ErrUnsupported, err)
		}
		existing = lifecycle.NewConfiguration()
	}

	wantByPrefix := map[string]storage.LifecycleRule{}
	for _, r := range rules {
		wantByPrefix[r.Prefix] = r
	}

	merged := make([]lifecycle.Rule, 0, len(existing.Rules)+len(rules))
	for _, r := range existing.Rules {
		if _, skip := wantByPrefix[r.RuleFilter.Prefix]; !skip {
			merged = append(merged, r)
		}
	}
	for _, r := range rules {
		merged = append(merged, lifecycle.Rule{
			ID:     "mio-" + sanitizeID(r.Prefix),
			Status: "Enabled",
			RuleFilter: lifecycle.Filter{
				Prefix: r.Prefix,
			},
			Expiration: lifecycle.Expiration{
				Days: lifecycle.ExpirationDays(r.AgeDays),
			},
		})
	}

	cfg := &lifecycle.Configuration{Rules: merged}
	if err := b.client.SetBucketLifecycle(ctx, b.bucket, cfg); err != nil {
		return fmt.Errorf("%w: set lifecycle: %v", storage.ErrUnsupported, err)
	}
	return nil
}

func buildUserMetadata(opts storage.PutOptions) map[string]string {
	meta := cloneMetadata(opts.Metadata)
	if opts.SHA256Hex != "" {
		meta["sha256"] = opts.SHA256Hex
	}
	if opts.TenantID != "" {
		meta["tenant_id"] = opts.TenantID
	}
	if opts.AccountID != "" {
		meta["account_id"] = opts.AccountID
	}
	if opts.ConversationID != "" {
		meta["conversation_id"] = opts.ConversationID
	}
	if opts.SourceMessageID != "" {
		meta["source_message_id"] = opts.SourceMessageID
	}
	return meta
}

func infoToObject(key string, info minio.ObjectInfo) *storage.Object {
	o := &storage.Object{
		Key:         key,
		Size:        info.Size,
		ContentType: info.ContentType,
		ModifiedAt:  info.LastModified,
		Metadata:    map[string]string{},
	}
	for k, v := range info.UserMetadata {
		lk := strings.ToLower(k)
		if v != "" {
			o.Metadata[lk] = v
		}
	}
	o.SHA256Hex = o.Metadata["sha256"]
	if o.SHA256Hex == "" {
		o.SHA256Hex = o.Metadata[strings.ToLower(storage.MetadataContentSHA256)]
	}
	o.TenantID = o.Metadata["tenant_id"]
	o.AccountID = o.Metadata["account_id"]
	o.ConversationID = o.Metadata["conversation_id"]
	o.SourceMessageID = o.Metadata["source_message_id"]
	return o
}

func cloneMetadata(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		if k != "" && v != "" {
			out[k] = v
		}
	}
	return out
}

// mapErr translates minio errors to storage sentinel errors.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	er := minio.ToErrorResponse(err)
	switch er.Code {
	case "NoSuchKey", "NoSuchBucket":
		return fmt.Errorf("%w: %v", storage.ErrNotFound, err)
	case "PreconditionFailed":
		return fmt.Errorf("%w: %v", storage.ErrAlreadyExists, err)
	}
	if er.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: %v", storage.ErrNotFound, err)
	}
	if er.StatusCode == http.StatusPreconditionFailed {
		return fmt.Errorf("%w: %v", storage.ErrAlreadyExists, err)
	}
	return err
}

// sanitizeID makes an S3 lifecycle rule ID from a prefix string.
func sanitizeID(prefix string) string {
	r := strings.NewReplacer("/", "-", " ", "-")
	id := r.Replace(prefix)
	id = strings.Trim(id, "-")
	if len(id) > 50 {
		id = id[:50]
	}
	return id
}
