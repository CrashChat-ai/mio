// Package gcs implements storage.Storage backed by Google Cloud Storage.
package gcs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	gcs "cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"

	"github.com/crashchat-ai/mio/services/media-vault/internal/storage"
)

// Backend implements storage.Storage on top of GCS.
type Backend struct {
	client *gcs.Client
	bucket string
}

var _ storage.Storage = (*Backend)(nil)

// New constructs a GCS-backed Storage. Uses ADC (Workload Identity in cluster).
func New(ctx context.Context, bucket string) (*Backend, error) {
	if bucket == "" {
		return nil, errors.New("gcs: bucket name is required")
	}
	c, err := gcs.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("gcs: new client: %w", err)
	}
	return &Backend{client: c, bucket: bucket}, nil
}

// Close releases the underlying GCS client.
func (b *Backend) Close() error { return b.client.Close() }

// Backend returns the implementation name (for metric labels).
func (b *Backend) Backend() string { return "gcs" }

func (b *Backend) bkt() *gcs.BucketHandle { return b.client.Bucket(b.bucket) }

// Put streams body to key. Honours opts.IfNotExists for race-free dedup.
func (b *Backend) Put(ctx context.Context, key string, body io.Reader, size int64, opts storage.PutOptions) error {
	obj := b.bkt().Object(key)
	if opts.IfNotExists {
		obj = obj.If(gcs.Conditions{DoesNotExist: true})
	}
	w := obj.NewWriter(ctx)
	if opts.ContentType != "" {
		w.ContentType = opts.ContentType
	}
	meta := map[string]string{}
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
	if len(meta) > 0 {
		w.Metadata = meta
	}

	if _, err := io.Copy(w, body); err != nil {
		_ = w.Close()
		return mapErr(err)
	}
	if err := w.Close(); err != nil {
		return mapErr(err)
	}
	return nil
}

// Get streams the bytes at key.
func (b *Backend) Get(ctx context.Context, key string) (io.ReadCloser, *storage.Object, error) {
	obj := b.bkt().Object(key)
	attrs, err := obj.Attrs(ctx)
	if err != nil {
		return nil, nil, mapErr(err)
	}
	r, err := obj.NewReader(ctx)
	if err != nil {
		return nil, nil, mapErr(err)
	}
	return r, attrsToObject(key, attrs), nil
}

// Stat returns metadata without fetching bytes.
func (b *Backend) Stat(ctx context.Context, key string) (*storage.Object, error) {
	attrs, err := b.bkt().Object(key).Attrs(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	return attrsToObject(key, attrs), nil
}

// Delete removes a single object. Idempotent (ErrNotFound is not an error).
func (b *Backend) Delete(ctx context.Context, key string) error {
	if err := b.bkt().Object(key).Delete(ctx); err != nil {
		if errors.Is(err, gcs.ErrObjectNotExist) {
			return nil
		}
		return mapErr(err)
	}
	return nil
}

// List enumerates objects under prefix and yields them on the returned channel.
// errCh receives a single error or nil at the end and is then closed.
func (b *Backend) List(ctx context.Context, prefix string) (<-chan storage.Object, <-chan error) {
	out := make(chan storage.Object, 32)
	errCh := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errCh)
		it := b.bkt().Objects(ctx, &gcs.Query{Prefix: prefix})
		for {
			attrs, err := it.Next()
			if errors.Is(err, iterator.Done) {
				errCh <- nil
				return
			}
			if err != nil {
				errCh <- mapErr(err)
				return
			}
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			case out <- *attrsToObject(attrs.Name, attrs):
			}
		}
	}()
	return out, errCh
}

// SignedURL issues a V4 signed GET URL for direct external access.
func (b *Backend) SignedURL(ctx context.Context, key string, opts storage.SignOptions) (string, error) {
	method := opts.Method
	if method == "" {
		method = http.MethodGet
	}
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = time.Hour
	}
	url, err := b.bkt().SignedURL(key, &gcs.SignedURLOptions{
		Scheme:          gcs.SigningSchemeV4,
		Method:          method,
		Expires:         time.Now().Add(ttl),
		QueryParameters: queryParams(opts.ResponseContentDisposition),
	})
	if err != nil {
		return "", fmt.Errorf("gcs: signed url: %w", err)
	}
	return url, nil
}

// SetLifecycle sets the bucket-wide lifecycle rules. Merges by Prefix
// (idempotent: existing rules with matching Prefix are replaced; rules with
// other prefixes are preserved).
func (b *Backend) SetLifecycle(ctx context.Context, rules []storage.LifecycleRule) error {
	bkt := b.bkt()
	attrs, err := bkt.Attrs(ctx)
	if err != nil {
		return mapErr(err)
	}

	wantByPrefix := map[string]storage.LifecycleRule{}
	for _, r := range rules {
		wantByPrefix[r.Prefix] = r
	}

	merged := make([]gcs.LifecycleRule, 0, len(attrs.Lifecycle.Rules)+len(rules))
	for _, existing := range attrs.Lifecycle.Rules {
		// Drop existing rules whose MatchesPrefix overlaps with our wanted set
		// (we will re-emit a fresh rule with the updated AgeDays).
		skip := false
		for _, p := range existing.Condition.MatchesPrefix {
			if _, ok := wantByPrefix[p]; ok {
				skip = true
				break
			}
		}
		if !skip {
			merged = append(merged, existing)
		}
	}
	for _, r := range rules {
		merged = append(merged, gcs.LifecycleRule{
			Action:    gcs.LifecycleAction{Type: gcs.DeleteAction},
			Condition: gcs.LifecycleCondition{AgeInDays: int64(r.AgeDays), MatchesPrefix: []string{r.Prefix}},
		})
	}

	// Idempotency: skip update if the rule set already matches what we want.
	if lifecycleEqual(attrs.Lifecycle.Rules, merged) {
		return nil
	}

	_, err = bkt.Update(ctx, gcs.BucketAttrsToUpdate{
		Lifecycle: &gcs.Lifecycle{Rules: merged},
	})
	if err != nil {
		return mapErr(err)
	}
	return nil
}

func lifecycleEqual(a, b []gcs.LifecycleRule) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Action != b[i].Action {
			return false
		}
		if a[i].Condition.AgeInDays != b[i].Condition.AgeInDays {
			return false
		}
		if !stringSliceEqual(a[i].Condition.MatchesPrefix, b[i].Condition.MatchesPrefix) {
			return false
		}
	}
	return true
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func queryParams(disposition string) map[string][]string {
	if disposition == "" {
		return nil
	}
	return map[string][]string{"response-content-disposition": {disposition}}
}

func attrsToObject(key string, attrs *gcs.ObjectAttrs) *storage.Object {
	if attrs == nil {
		return &storage.Object{Key: key}
	}
	o := &storage.Object{
		Key:         key,
		Size:        attrs.Size,
		ContentType: attrs.ContentType,
		ModifiedAt:  attrs.Updated,
	}
	if m := attrs.Metadata; m != nil {
		o.SHA256Hex = m["sha256"]
		o.TenantID = m["tenant_id"]
		o.AccountID = m["account_id"]
		o.ConversationID = m["conversation_id"]
		o.SourceMessageID = m["source_message_id"]
	}
	return o
}

// mapErr translates concrete GCS errors into storage sentinel errors.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gcs.ErrObjectNotExist) || errors.Is(err, gcs.ErrBucketNotExist) {
		return fmt.Errorf("%w: %v", storage.ErrNotFound, err)
	}
	var ge *googleapi.Error
	if errors.As(err, &ge) {
		switch ge.Code {
		case http.StatusNotFound:
			return fmt.Errorf("%w: %v", storage.ErrNotFound, err)
		case http.StatusPreconditionFailed:
			return fmt.Errorf("%w: %v", storage.ErrAlreadyExists, err)
		}
	}
	return err
}
