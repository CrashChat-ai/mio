package gcs

import (
	"context"

	"github.com/crashchat-ai/mio/attachment-downloader/internal/storage"
)

func init() {
	storage.Register("gcs", func(ctx context.Context, bucket string) (storage.Storage, error) {
		return New(ctx, bucket)
	})
}
