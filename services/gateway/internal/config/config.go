// Package config loads gateway configuration from environment variables
// and file-mounted secrets under /etc/mio/secrets/.
package config

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/crashchat-ai/mio/pkg/channels"
)

// secretsDir is a var so tests can point it at a tempdir.
var secretsDir = "/etc/mio/secrets"

// Config holds all gateway configuration. Non-secret values come from env
// vars; secrets are read from file mounts (never env vars).
type Config struct {
	// Deploy environment
	Env string // MIO_ENV, one of {dev, staging, prod}; default dev

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
	PostgresDSN    string // MIO_POSTGRES_DSN, required
	PgxMaxConns    int    // MIO_PGX_MAX_CONNS, default (GOMAXPROCS*2)+1
	MigrateOnStart bool   // MIO_MIGRATE_ON_START, default true

	// Webhook signing secrets keyed by channel_type, file-mounted from
	// secretsDir. Names come from each adapter's SecretNamer (fallback:
	// channels.DefaultWebhookSecretName). Missing file = dev mode.
	WebhookSecrets map[string]string
}

// validEnvs is the allowlist for MIO_ENV. Unknown values are rejected loud
// and early so a typo in a deploy manifest (`MIO_ENV=production`) doesn't
// silently fall through to dev-mode defaults like NoopCipher acceptance.
var validEnvs = map[string]bool{
	"dev":     true,
	"staging": true,
	"prod":    true,
}

// Load reads config from environment and file-mounted secrets.
// Returns an error if any required field is missing.
func Load() (*Config, error) {
	env := envStr("MIO_ENV", "dev")
	if !validEnvs[env] {
		return nil, fmt.Errorf("config: MIO_ENV=%q invalid; must be one of dev|staging|prod", env)
	}
	cfg := &Config{
		Env:                 env,
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

	secrets, err := loadWebhookSecrets()
	if err != nil {
		return nil, err
	}
	cfg.WebhookSecrets = secrets

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

// loadWebhookSecrets reads each registered inbound adapter's signing secret,
// first matching name wins. Adapters without inbound support are skipped;
// binaries that don't blank-import channel packages load none.
func loadWebhookSecrets() (map[string]string, error) {
	secrets := make(map[string]string)
	for _, adapter := range channels.RegisteredAdapters() {
		inbound := adapter.Inbound()
		if inbound == nil {
			continue
		}
		names := []string{channels.DefaultWebhookSecretName(adapter.ChannelType())}
		if sn, ok := inbound.(channels.SecretNamer); ok {
			names = sn.WebhookSecretNames()
		}
		for _, name := range names {
			secret, err := readSecret(name)
			if err != nil {
				return nil, fmt.Errorf("config: %s: %w", name, err)
			}
			if secret != "" {
				secrets[adapter.ChannelType()] = secret
				break
			}
		}
	}
	return secrets, nil
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
