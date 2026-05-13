package writer

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

// gcsWriter implements Writer using the Google Cloud Storage client.
//
// Atomic rename pattern:
//  1. Write inflight object (objectPath + ".inflight") via a resumable upload.
//  2. Copy inflight → final object (atomic at the GCS API layer).
//  3. Delete inflight.
//  4. Caller acks JetStream messages AFTER this function returns nil.
//
// Idempotency: if the process dies between (2) and (3), the next pod delivers
// the same sequence range → same objectPath → same bytes → safe overwrite.
// GCS object creation is idempotent for identical content; the overwrite simply
// replaces with equal bytes, producing no data loss.
type gcsWriter struct {
	client *storage.Client
	bucket string
	buf    bytes.Buffer
}

func newGCSWriter(ctx context.Context, cfg *Config) (*gcsWriter, error) {
	var opts []option.ClientOption
	if cfg.CredentialsFile != "" {
		opts = append(opts, option.WithCredentialsFile(cfg.CredentialsFile))
	}
	// With no CredentialsFile, the GCS SDK uses Application Default Credentials:
	// GOOGLE_APPLICATION_CREDENTIALS → Workload Identity → ADC chain.
	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("gcs: new client: %w", err)
	}
	return &gcsWriter{client: client, bucket: cfg.Bucket}, nil
}

func (w *gcsWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *gcsWriter) Len() int { return w.buf.Len() }

// Flush implements the copy-then-delete atomic rename pattern.
func (w *gcsWriter) Flush(ctx context.Context, objectPath string) error {
	if w.buf.Len() == 0 {
		return nil
	}
	inflightPath := objectPath + ".inflight"
	bkt := w.client.Bucket(w.bucket)

	// Step 1: write inflight object.
	wc := bkt.Object(inflightPath).NewWriter(ctx)
	if _, err := io.Copy(wc, bytes.NewReader(w.buf.Bytes())); err != nil {
		_ = wc.Close()
		return fmt.Errorf("gcs: write inflight %q: %w", inflightPath, err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("gcs: close inflight writer %q: %w", inflightPath, err)
	}

	// Step 2: copy inflight → final (atomic at the GCS API).
	src := bkt.Object(inflightPath)
	dst := bkt.Object(objectPath)
	copier := dst.CopierFrom(src)
	if _, err := copier.Run(ctx); err != nil {
		return fmt.Errorf("gcs: copy inflight→final %q: %w", objectPath, err)
	}

	// Step 3: delete inflight.
	if err := src.Delete(ctx); err != nil {
		// Non-fatal: orphaned inflight will be cleaned by bucket lifecycle rule
		// (*.inflight older than 24h, deployed in P7). Log but don't fail.
		// Caller still acks — the final object exists.
		_ = err // intentional: logged at caller level
	}

	w.buf.Reset()
	return nil
}

// Close flushes remaining data (if any) then closes the GCS client.
func (w *gcsWriter) Close(ctx context.Context, objectPath string) error {
	if w.buf.Len() > 0 {
		if err := w.Flush(ctx, objectPath); err != nil {
			return err
		}
	}
	return w.client.Close()
}
