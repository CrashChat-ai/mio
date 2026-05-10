package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"

	"github.com/crashchat-ai/mio/media-vault/internal/dedup"
	"github.com/crashchat-ai/mio/media-vault/internal/fetcher"
	"github.com/crashchat-ai/mio/media-vault/internal/keygen"
	"github.com/crashchat-ai/mio/media-vault/internal/metrics"
	"github.com/crashchat-ai/mio/media-vault/internal/storage"
)

// EnrichingProcessor implements the real per-message flow: fetch each
// attachment, persist to storage with dedup, rewrite Attachment fields, and
// publish the enriched Message via the supplied publisher.
type EnrichingProcessor struct {
	Storage      storage.Storage
	Publisher    Publisher
	StoragePrefix string
	SignedURLTTL time.Duration
	Log          *slog.Logger
}

// Publisher decouples the worker from the publisher package (avoids an import
// cycle if a test wants to inject a stub). The publisher package's *Publisher
// satisfies this trivially.
type Publisher interface {
	Publish(ctx context.Context, msg *miov1.Message) error
}

// Process fetches + persists every unprocessed attachment in msg, rewrites
// fields, and publishes the enriched Message exactly once.
func (p *EnrichingProcessor) Process(ctx context.Context, msg *miov1.Message) error {
	channelType := msg.GetChannelType()
	receivedAt := time.Now().UTC()
	if ts := msg.GetReceivedAt(); ts != nil {
		receivedAt = ts.AsTime().UTC()
	}

	for _, att := range msg.GetAttachments() {
		if att.GetStorageKey() != "" {
			continue
		}
		p.processAttachment(ctx, channelType, receivedAt, msg.GetAccountId(), att)
	}

	if err := p.Publisher.Publish(ctx, msg); err != nil {
		// Publish failure is retryable — Nak so we re-enrich (storage writes
		// are idempotent under content-hash key + IfNotExists).
		return fmt.Errorf("processor: publish enriched: %w", err)
	}
	return nil
}

func (p *EnrichingProcessor) processAttachment(
	ctx context.Context,
	channelType string,
	receivedAt time.Time,
	accountID string,
	att *miov1.Attachment,
) {
	start := time.Now()
	outcome := "ok"
	defer func() {
		metrics.DownloadDuration.WithLabelValues(channelType, outcome).Observe(time.Since(start).Seconds())
		metrics.DownloadedTotal.WithLabelValues(channelType, outcome).Inc()
	}()

	f := fetcher.Lookup(channelType)
	if f == nil {
		p.Log.Warn("processor: no fetcher for channel", "channel_type", channelType)
		att.ErrorCode = miov1.Attachment_ERROR_CODE_FORBIDDEN
		outcome = "no_fetcher"
		return
	}

	// Buffered approach for POC: ≤ MIO_DOWNLOAD_MAX_BYTES means in-memory is
	// fine, and we need the SHA before we can build the content-addressable
	// key. Phase-04 plan acknowledges tempfile-fallback as a P10 enhancement.
	var buf bytes.Buffer
	res, err := f.Fetch(ctx, att, &buf)
	if err != nil {
		var fe *fetcher.Error
		if errors.As(err, &fe) {
			att.ErrorCode = fe.Code
			outcome = errCodeToLabel(fe.Code)
			return
		}
		outcome = "fetch_error"
		// Non-typed (network blip / 5xx / context deadline). The Cliq URL TTL
		// is short and the platform won't return the bytes by next redelivery
		// — so we surface the failure to downstream consumers via
		// ERROR_CODE_TIMEOUT and let the message flow. Downstream AI consumer
		// soft-handles missing bytes (the design contract).
		p.Log.Error("processor: fetch transient error", "err", err, "channel_type", channelType)
		att.ErrorCode = miov1.Attachment_ERROR_CODE_TIMEOUT
		return
	}

	key := keygen.Build(p.StoragePrefix, channelType, res.SHA256Hex, res.ContentType, att.GetFilename(), receivedAt)

	storeStart := time.Now()
	dr, err := dedup.PersistIfAbsent(ctx, p.Storage, key, func() error {
		return p.Storage.Put(ctx, key, bytes.NewReader(buf.Bytes()), res.Bytes, storage.PutOptions{
			ContentType: res.ContentType,
			SHA256Hex:   res.SHA256Hex,
			AccountID:   accountID,
			IfNotExists: true,
		})
	})
	metrics.StorageDuration.WithLabelValues(p.Storage.Backend(), "put").Observe(time.Since(storeStart).Seconds())
	if err != nil {
		p.Log.Error("processor: persist", "err", err, "key", key)
		att.ErrorCode = miov1.Attachment_ERROR_CODE_STORAGE
		outcome = "storage_error"
		return
	}
	if dr.AlreadyExisted || dr.CollisionResolved {
		metrics.DedupHits.WithLabelValues(channelType).Inc()
	} else {
		metrics.BytesTotal.WithLabelValues(channelType).Add(float64(res.Bytes))
	}

	signStart := time.Now()
	signed, err := p.Storage.SignedURL(ctx, key, storage.SignOptions{TTL: p.SignedURLTTL})
	metrics.StorageDuration.WithLabelValues(p.Storage.Backend(), "signed_url").Observe(time.Since(signStart).Seconds())
	if err != nil {
		p.Log.Warn("processor: signed url", "err", err, "key", key)
		// non-fatal — consumers can re-mint via storage_key; leave att.Url alone
	} else if signed != "" {
		att.Url = signed
	}

	att.StorageKey = key
	att.ContentSha256 = res.SHA256Hex
	att.Bytes = res.Bytes
	if res.ContentType != "" {
		att.Mime = res.ContentType
	}
	att.ErrorCode = miov1.Attachment_ERROR_CODE_OK
}

func errCodeToLabel(c miov1.Attachment_ErrorCode) string {
	switch c {
	case miov1.Attachment_ERROR_CODE_OK:
		return "ok"
	case miov1.Attachment_ERROR_CODE_EXPIRED:
		return "expired"
	case miov1.Attachment_ERROR_CODE_FORBIDDEN:
		return "forbidden"
	case miov1.Attachment_ERROR_CODE_NOT_FOUND:
		return "not_found"
	case miov1.Attachment_ERROR_CODE_TOO_LARGE:
		return "too_large"
	case miov1.Attachment_ERROR_CODE_STORAGE:
		return "storage_error"
	case miov1.Attachment_ERROR_CODE_TIMEOUT:
		return "timeout"
	}
	return "unknown"
}
