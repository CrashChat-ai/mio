package s3

import (
	"context"

	"github.com/crashchat-ai/mio/services/media-vault/internal/storage"
)

func init() {
	storage.Register("s3", func(ctx context.Context, bucket string) (storage.Storage, error) {
		cfg, err := ConfigFromEnv(bucket)
		if err != nil {
			return nil, err
		}
		return New(ctx, cfg)
	})
}
