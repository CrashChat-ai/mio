package writer

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// minioWriter implements Writer using the MinIO Go SDK (S3-compatible).
//
// Used for local development against the docker-compose MinIO service.
// Shares the same atomic rename contract as gcsWriter:
//  1. PutObject inflight
//  2. CopyObject inflight → final
//  3. RemoveObject inflight
//
// This mirrors the GCS copy-then-delete pattern exactly so that the
// local integration test exercises the same code path as production.
type minioWriter struct {
	client *minio.Client
	bucket string
	buf    bytes.Buffer
}

func newMinIOWriter(_ context.Context, cfg *Config) (*minioWriter, error) {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = "localhost:9000"
	}
	// Strip scheme — minio-go expects host:port, not http://host:port.
	useSSL := cfg.UseSSL
	endpoint = strings.TrimPrefix(endpoint, "https://")
	if strings.HasPrefix(endpoint, "http://") {
		endpoint = strings.TrimPrefix(endpoint, "http://")
		useSSL = false
	}

	accessKey := cfg.AccessKey
	if accessKey == "" {
		accessKey = "minioadmin"
	}
	secretKey := cfg.SecretKey
	if secretKey == "" {
		secretKey = "minioadmin"
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("minio: new client: %w", err)
	}
	return &minioWriter{client: client, bucket: cfg.Bucket}, nil
}

func (w *minioWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *minioWriter) Len() int { return w.buf.Len() }

// Flush implements the copy-then-delete atomic rename pattern via MinIO S3 API.
func (w *minioWriter) Flush(ctx context.Context, objectPath string) error {
	if w.buf.Len() == 0 {
		return nil
	}
	inflightPath := objectPath + ".inflight"
	data := w.buf.Bytes()
	size := int64(len(data))

	// Step 1: upload inflight object.
	_, err := w.client.PutObject(ctx, w.bucket, inflightPath,
		bytes.NewReader(data), size,
		minio.PutObjectOptions{ContentType: "application/x-ndjson"},
	)
	if err != nil {
		return fmt.Errorf("minio: put inflight %q: %w", inflightPath, err)
	}

	// Step 2: copy inflight → final (server-side copy, atomic at the API).
	dst := minio.CopyDestOptions{Bucket: w.bucket, Object: objectPath}
	src := minio.CopySrcOptions{Bucket: w.bucket, Object: inflightPath}
	if _, err := w.client.CopyObject(ctx, dst, src); err != nil {
		return fmt.Errorf("minio: copy inflight→final %q: %w", objectPath, err)
	}

	// Step 3: delete inflight (non-fatal; lifecycle rule handles orphans).
	if err := w.client.RemoveObject(ctx, w.bucket, inflightPath, minio.RemoveObjectOptions{}); err != nil {
		// Non-fatal — orphaned .inflight cleaned by lifecycle rule in P7.
		_ = err
	}

	w.buf.Reset()
	return nil
}

// Close flushes remaining data then releases resources (no persistent conn to close in minio-go).
func (w *minioWriter) Close(ctx context.Context, objectPath string) error {
	if w.buf.Len() > 0 {
		return w.Flush(ctx, objectPath)
	}
	return nil
}

// EnsureBucket creates the bucket if it does not already exist.
// Called once at startup; idempotent.
func EnsureBucket(ctx context.Context, cfg *Config) error {
	w, err := newMinIOWriter(ctx, cfg)
	if err != nil {
		return err
	}
	exists, err := w.client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return fmt.Errorf("minio: bucket exists check: %w", err)
	}
	if !exists {
		if err := w.client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("minio: make bucket %q: %w", cfg.Bucket, err)
		}
	}
	return nil
}
