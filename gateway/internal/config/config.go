// Package config loads gateway configuration from environment variables
// and file-mounted secrets under /etc/mio/secrets/.
package config

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

const secretsDir = "/etc/mio/secrets"

// Config holds all gateway configuration. Non-secret values come from env
// vars; secrets are read from file mounts (never env vars).
type Config struct {
	// HTTP server
	Port                int    // MIO_PORT, default 8080
	LogLevel            string // MIO_LOG_LEVEL, default "info"
	GracefulShutdownSec int    // MIO_GRACEFUL_SHUTDOWN_SECS, default 15

	// Four-tier identity (required)
	TenantID  string // MIO_TENANT_ID (UUID)
	AccountID string // MIO_ACCOUNT_ID (UUID)

	// Sender pool
	SenderWorkers int // MIO_SENDER_WORKERS, default 8

	// NATS
	NatsURLs []string // MIO_NATS_URLS, comma-separated, default "nats://localhost:4222"

	// Postgres
	PostgresDSN  string // MIO_POSTGRES_DSN, required
	PgxMaxConns  int    // MIO_PGX_MAX_CONNS, default (GOMAXPROCS*2)+1
	MigrateOnStart bool // MIO_MIGRATE_ON_START, default true

	// Secrets (file-mounted)
	CliqWebhookSecret string // /etc/mio/secrets/cliq-webhook-secret
}

// Load reads config from environment and file-mounted secrets.
// Returns an error if any required field is missing.
func Load() (*Config, error) {
	cfg := &Config{
		Port:                envInt("MIO_PORT", 8080),
		LogLevel:            envStr("MIO_LOG_LEVEL", "info"),
		GracefulShutdownSec: envInt("MIO_GRACEFUL_SHUTDOWN_SECS", 15),
		TenantID:            envStr("MIO_TENANT_ID", ""),
		AccountID:           envStr("MIO_ACCOUNT_ID", ""),
		SenderWorkers:       envInt("MIO_SENDER_WORKERS", 8),
		NatsURLs:            envCSV("MIO_NATS_URLS", "nats://localhost:4222"),
		PostgresDSN:         envStr("MIO_POSTGRES_DSN", ""),
		PgxMaxConns:         envInt("MIO_PGX_MAX_CONNS", (runtime.GOMAXPROCS(0)*2)+1),
		MigrateOnStart:      envBool("MIO_MIGRATE_ON_START", true),
	}

	// Load secrets from file mounts.
	secret, err := readSecret("cliq-webhook-secret")
	if err != nil {
		return nil, fmt.Errorf("config: cliq-webhook-secret: %w", err)
	}
	cfg.CliqWebhookSecret = secret

	// Validate required fields.
	if cfg.TenantID == "" {
		return nil, fmt.Errorf("config: MIO_TENANT_ID is required")
	}
	if cfg.AccountID == "" {
		return nil, fmt.Errorf("config: MIO_ACCOUNT_ID is required")
	}
	if cfg.PostgresDSN == "" {
		return nil, fmt.Errorf("config: MIO_POSTGRES_DSN is required")
	}
	if cfg.PgxMaxConns < 1 {
		cfg.PgxMaxConns = 1
	}

	return cfg, nil
}

// readSecret reads a secret file from secretsDir, returning its trimmed content.
// Returns empty string (not error) if the file does not exist — callers decide
// whether the secret is required.
func readSecret(name string) (string, error) {
	path := secretsDir + "/" + name
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envCSV(key, def string) []string {
	v := os.Getenv(key)
	if v == "" {
		v = def
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
