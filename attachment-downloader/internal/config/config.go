// Package config holds attachment-downloader runtime config loaded from env.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config is the validated runtime config.
type Config struct {
	NATSURLs         string
	TenantID         string
	AccountID        string
	StorageBackend   string
	StorageBucket    string
	StoragePrefix    string
	DownloadTimeout  time.Duration
	DownloadMaxBytes int64
	SignedURLTTL     time.Duration
	DurableName      string
	MetricsPort      int
	LogLevel         string

	EnrichedStream string

	// Cliq token (read once on startup, env-injected by chart from existing
	// gateway secret).
	CliqBotToken string
}

// FromEnv loads + validates the config. Fail-fast: required fields missing
// → error, never silent default.
func FromEnv() (*Config, error) {
	c := &Config{
		NATSURLs:         envOr("MIO_NATS_URLS", "nats://localhost:4222"),
		TenantID:         os.Getenv("MIO_TENANT_ID"),
		AccountID:        os.Getenv("MIO_ACCOUNT_ID"),
		StorageBackend:   envOr("MIO_STORAGE_BACKEND", "gcs"),
		StorageBucket:    os.Getenv("MIO_STORAGE_BUCKET"),
		StoragePrefix:    envOr("MIO_STORAGE_PREFIX", "mio/attachments/"),
		DurableName:      envOr("MIO_DURABLE_NAME", "attachment-downloader"),
		LogLevel:         envOr("MIO_LOG_LEVEL", "info"),
		EnrichedStream:   envOr("MIO_ENRICHED_STREAM", "MESSAGES_INBOUND_ENRICHED"),
		CliqBotToken:     os.Getenv("MIO_CLIQ_BOT_TOKEN"),
	}

	dlSec, err := envIntOr("MIO_DOWNLOAD_TIMEOUT_SECONDS", 60)
	if err != nil {
		return nil, err
	}
	c.DownloadTimeout = time.Duration(dlSec) * time.Second

	maxBytes, err := envInt64Or("MIO_DOWNLOAD_MAX_BYTES", 26214400) // 25 MiB
	if err != nil {
		return nil, err
	}
	c.DownloadMaxBytes = maxBytes

	signedSec, err := envIntOr("MIO_SIGNED_URL_TTL_SECONDS", 3600)
	if err != nil {
		return nil, err
	}
	c.SignedURLTTL = time.Duration(signedSec) * time.Second

	port, err := envIntOr("MIO_METRICS_PORT", 9090)
	if err != nil {
		return nil, err
	}
	c.MetricsPort = port

	if c.TenantID == "" {
		return nil, fmt.Errorf("config: MIO_TENANT_ID is required")
	}
	if c.AccountID == "" {
		return nil, fmt.Errorf("config: MIO_ACCOUNT_ID is required")
	}
	if c.StorageBucket == "" {
		return nil, fmt.Errorf("config: MIO_STORAGE_BUCKET is required")
	}
	return c, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envIntOr(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be int: %w", key, err)
	}
	return n, nil
}

func envInt64Or(key string, def int64) (int64, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be int64: %w", key, err)
	}
	return n, nil
}
