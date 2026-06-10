package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"

	"github.com/crashchat-ai/mio/services/media-vault/internal/dedup"
	"github.com/crashchat-ai/mio/services/media-vault/internal/fetcher"
	"github.com/crashchat-ai/mio/services/media-vault/internal/keygen"
	"github.com/crashchat-ai/mio/services/media-vault/internal/metrics"
	"github.com/crashchat-ai/mio/services/media-vault/internal/storage"
)

// EnrichingProcessor implements the real per-message flow: fetch each
// attachment, persist to storage with dedup, rewrite Attachment fields, and
// publish the enriched Message via the supplied publisher.
type EnrichingProcessor struct {
	Storage       storage.Storage
	Publisher     Publisher
	StorageBucket string
	StoragePrefix string
	Log           *slog.Logger
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

	for idx, att := range msg.GetAttachments() {
		if att.GetStorageKey() != "" {
			continue
		}
		p.processAttachment(ctx, attachmentSource{
			MessageID:              msg.GetId(),
			TenantID:               msg.GetTenantId(),
			AccountID:              msg.GetAccountId(),
			ChannelType:            channelType,
			ConversationID:         msg.GetConversationId(),
			ConversationExternalID: msg.GetConversationExternalId(),
			SourceMessageID:        msg.GetSourceMessageId(),
			ThreadRootMessageID:    msg.GetThreadRootMessageId(),
			Relation:               msg.GetRelation(),
			AttachmentIndex:        idx,
			ReceivedAt:             receivedAt,
		}, att)
	}

	if err := p.Publisher.Publish(ctx, msg); err != nil {
		// Publish failure is retryable — Nak so we re-enrich (storage writes
		// are idempotent under content-hash key + IfNotExists).
		return fmt.Errorf("processor: publish enriched: %w", err)
	}
	return nil
}

type attachmentSource struct {
	MessageID              string
	TenantID               string
	AccountID              string
	ChannelType            string
	ConversationID         string
	ConversationExternalID string
	SourceMessageID        string
	ThreadRootMessageID    string
	Relation               *miov1.MessageRelation
	AttachmentIndex        int
	ReceivedAt             time.Time
}

func (p *EnrichingProcessor) processAttachment(
	ctx context.Context,
	source attachmentSource,
	att *miov1.Attachment,
) {
	start := time.Now()
	outcome := "ok"
	defer func() {
		metrics.DownloadDuration.WithLabelValues(source.ChannelType, outcome).Observe(time.Since(start).Seconds())
		metrics.DownloadedTotal.WithLabelValues(source.ChannelType, outcome).Inc()
	}()

	f := fetcher.Lookup(source.ChannelType)
	if f == nil {
		p.Log.Warn("processor: no fetcher for channel", "channel_type", source.ChannelType)
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
		p.Log.Error("processor: fetch transient error", "err", err, "channel_type", source.ChannelType)
		att.ErrorCode = miov1.Attachment_ERROR_CODE_TIMEOUT
		return
	}

	key := keygen.Build(
		p.StoragePrefix,
		source.ChannelType,
		res.SHA256Hex,
		res.ContentType,
		att.GetFilename(),
		source.ReceivedAt,
	)
	storageKey := p.objectStorageKey(key)

	storeStart := time.Now()
	dr, err := dedup.PersistIfAbsent(ctx, p.Storage, key, func() error {
		return p.Storage.Put(ctx, key, bytes.NewReader(buf.Bytes()), res.Bytes, storage.PutOptions{
			ContentType:     res.ContentType,
			SHA256Hex:       res.SHA256Hex,
			TenantID:        source.TenantID,
			AccountID:       source.AccountID,
			ConversationID:  source.ConversationID,
			SourceMessageID: source.SourceMessageID,
			Metadata:        p.sourceMetadata(source, att, key, storageKey, res),
			IfNotExists:     true,
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
		metrics.DedupHits.WithLabelValues(source.ChannelType).Inc()
	} else {
		metrics.BytesTotal.WithLabelValues(source.ChannelType).Add(float64(res.Bytes))
	}

	// storage_key is the full gs:// URI so downstream consumers have a
	// self-contained reference without needing the bucket name from elsewhere.
	// att.Url is left untouched to preserve the original platform URL (Cliq,
	// etc.). Signed GCS URLs expire in ~1 h and are useless in long-lived
	// storage like BigQuery — callers can sign on demand from storage_key.
	att.StorageKey = storageKey
	att.ContentSha256 = res.SHA256Hex
	att.Bytes = res.Bytes
	if res.ContentType != "" {
		att.Mime = res.ContentType
	}
	att.ErrorCode = miov1.Attachment_ERROR_CODE_OK
}

func (p *EnrichingProcessor) objectStorageKey(key string) string {
	if p.StorageBucket == "" {
		return key
	}
	return "gs://" + p.StorageBucket + "/" + key
}

func (p *EnrichingProcessor) sourceMetadata(
	source attachmentSource,
	att *miov1.Attachment,
	objectKey string,
	storageKey string,
	res fetcher.Result,
) map[string]string {
	metadata := map[string]string{
		storage.MetadataSchemaVersion:   "1",
		storage.MetadataAttachmentIndex: strconv.Itoa(source.AttachmentIndex),
		storage.MetadataStorageKey:      storageKey,
		storage.MetadataObjectKey:       objectKey,
		storage.MetadataBytes:           strconv.FormatInt(res.Bytes, 10),
		storage.MetadataContentSHA256:   res.SHA256Hex,
		storage.MetadataErrorCode:       miov1.Attachment_ERROR_CODE_OK.String(),
		storage.MetadataReceivedAt:      source.ReceivedAt.Format(time.RFC3339Nano),
	}

	addMetadata(metadata, storage.MetadataMessageID, source.MessageID)
	addMetadata(metadata, storage.MetadataSourceMessageID, source.SourceMessageID)
	addMetadata(metadata, storage.MetadataTenantID, source.TenantID)
	addMetadata(metadata, storage.MetadataAccountID, source.AccountID)
	addMetadata(metadata, storage.MetadataChannelType, source.ChannelType)
	addMetadata(metadata, storage.MetadataConversationID, source.ConversationID)
	addMetadata(metadata, storage.MetadataConversationExternalID, source.ConversationExternalID)
	addMetadata(metadata, storage.MetadataThreadRootMessageID, source.ThreadRootMessageID)
	addMetadata(metadata, storage.MetadataFilename, att.GetFilename())
	mime := res.ContentType
	if mime == "" {
		mime = att.GetMime()
	}
	addMetadata(metadata, storage.MetadataMIME, mime)

	if source.Relation != nil {
		addMetadata(metadata, storage.MetadataRelationKind, source.Relation.GetKind().String())
		addMetadata(metadata, storage.MetadataRelationTargetMessageID, source.Relation.GetTargetMessageId())
		addMetadata(metadata, storage.MetadataRelationTargetExternalID, source.Relation.GetTargetExternalId())
	}

	return metadata
}

func addMetadata(metadata map[string]string, key string, value string) {
	if value == "" {
		return
	}
	metadata[key] = value
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
