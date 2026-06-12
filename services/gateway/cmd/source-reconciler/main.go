// Command source-reconciler backfills provider history gaps into
// MESSAGES_INBOUND without touching the webhook hot path.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/crashchat-ai/mio/pkg/channels"
	sdk "github.com/crashchat-ai/mio/sdk-go"
	"github.com/crashchat-ai/mio/services/gateway/internal/crypto"
	"github.com/crashchat-ai/mio/services/gateway/internal/reconciler"
	"github.com/crashchat-ai/mio/services/gateway/store"

	// Register in-tree adapters for history capability discovery.
	_ "github.com/crashchat-ai/mio/channels/all"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	if err := run(context.Background(), logger); err != nil {
		level := slog.LevelError
		if channels.IsScopeMissing(err) {
			level = slog.LevelWarn
		}
		logger.Log(context.Background(), level, "source-reconciler: fatal", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	pg, err := store.NewPool(ctx, cfg.PostgresDSN, int32(cfg.PgxMaxConns))
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	defer pg.Close()
	if cfg.MigrateOnStart {
		if err := store.MigrateUp(cfg.PostgresDSN); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}

	accountID, err := uuid.Parse(cfg.AccountID)
	if err != nil {
		return fmt.Errorf("MIO_ACCOUNT_ID: %w", err)
	}
	account, err := store.GetAccount(ctx, pg, accountID)
	if err != nil {
		return fmt.Errorf("account: %w", err)
	}
	if account.DisabledAt != nil {
		return fmt.Errorf("account %s is disabled", cfg.AccountID)
	}
	cursor := cfg.Cursor
	if cursor == "" {
		storedCursor, found, err := store.GetSourceReconcileCursor(ctx, pg, account.ID, cfg.ConversationExternalID)
		if err != nil {
			return err
		}
		if found {
			cursor = storedCursor.Cursor
		}
	}

	adapter, err := adapterByChannelType(account.ChannelType)
	if err != nil {
		return err
	}
	historyAdapter, ok := adapter.(channels.HistoryAdapter)
	if !ok {
		return fmt.Errorf("channel_type %q does not implement history reconciliation", account.ChannelType)
	}

	cipher, err := buildCipher(cfg)
	if err != nil {
		return err
	}
	credRow, err := store.GetCredential(ctx, pg, cipher, account.ID, logger)
	if err != nil {
		return fmt.Errorf("credential: %w", err)
	}
	credential := channels.Credential{
		AccessToken:  credRow.Plaintext.AccessToken,
		RefreshToken: credRow.Plaintext.RefreshToken,
		ExpiresAt:    credRow.Plaintext.ExpiresAt,
		Extras:       credRow.Plaintext.Extras,
	}
	if shouldRefresh(credential) {
		refreshed, err := adapter.Credentials().RefreshCredential(ctx, credential)
		if err != nil {
			return fmt.Errorf("credential refresh: %w", err)
		}
		if err := store.PutCredential(ctx, pg, cipher, account.ID, credRow.AuthKind, store.CredentialPayload{
			AccessToken:  refreshed.AccessToken,
			RefreshToken: refreshed.RefreshToken,
			ExpiresAt:    refreshed.ExpiresAt,
			Extras:       refreshed.Extras,
		}); err != nil {
			return fmt.Errorf("credential refresh persist: %w", err)
		}
		credential = refreshed
	}

	natsURL := strings.Join(cfg.NatsURLs, ",")
	sdkClient, err := sdk.New(natsURL,
		sdk.WithName("mio-source-reconciler/sdk/"+version),
		sdk.WithMetricsRegistry(prometheus.NewRegistry()),
	)
	if err != nil {
		return fmt.Errorf("sdk: %w", err)
	}
	defer sdkClient.Close()
	if err := store.EnsureStreams(ctx, sdkClient.JetStream(), 1); err != nil {
		return fmt.Errorf("jetstream: ensure streams: %w", err)
	}

	attrs := map[string]string{}
	if cfg.CliqChannelName != "" {
		attrs["cliq_channel_name"] = cfg.CliqChannelName
	}
	r := &reconciler.Runner{
		Store:     store.NewInboundStore(pg),
		Publisher: sdkClient,
		Adapters:  map[string]channels.HistoryAdapter{account.ChannelType: historyAdapter},
		Logger:    logger,
	}
	res, err := r.Reconcile(ctx, reconciler.Request{
		TenantID:    account.TenantID.String(),
		AccountID:   account.ID.String(),
		ChannelType: account.ChannelType,
		Credential:  credential,
		Conversation: channels.HistoryConversation{
			ExternalID:  cfg.ConversationExternalID,
			DisplayName: cfg.ConversationDisplayName,
			Kind:        cfg.ConversationKind,
			Attributes:  attrs,
		},
		Cursor: cursor,
		Since:  cfg.Since,
		Until:  cfg.Until,
		Limit:  cfg.Limit,
	})
	if err != nil {
		if recordErr := store.RecordSourceReconcileError(ctx, pg, account.ID, account.ChannelType, cfg.ConversationExternalID, err); recordErr != nil {
			logger.Warn("source-reconciler: record error status failed", "err", recordErr)
		}
		if errors.Is(err, channels.ErrScopeMissing) {
			return fmt.Errorf("scope_missing: re-consent install with %s: %w", "ZohoCliq.Messages.READ", err)
		}
		return err
	}
	nextCursor := res.NextCursor
	if nextCursor == "" {
		nextCursor = cursor
	}
	if err := store.RecordSourceReconcileSuccess(ctx, pg, account.ID, account.ChannelType, cfg.ConversationExternalID, nextCursor); err != nil {
		return err
	}
	logger.Info("source-reconciler: reconcile complete",
		"account_id", account.ID,
		"channel_type", account.ChannelType,
		"conversation_external_id", cfg.ConversationExternalID,
		"seen", res.Seen,
		"inserted", res.Inserted,
		"duplicates", res.Duplicates,
		"published", res.Published,
		"next_cursor", nextCursor,
	)
	return nil
}

type config struct {
	Env                     string
	PostgresDSN             string
	PgxMaxConns             int
	MigrateOnStart          bool
	AccountID               string
	NatsURLs                []string
	AgeKeyVersion           int
	ConversationExternalID  string
	ConversationDisplayName string
	ConversationKind        string
	CliqChannelName         string
	Cursor                  string
	Since                   time.Time
	Until                   time.Time
	Limit                   int
}

func loadConfig() (config, error) {
	cfg := config{
		Env:                     envStr("MIO_ENV", "dev"),
		PostgresDSN:             os.Getenv("MIO_POSTGRES_DSN"),
		PgxMaxConns:             envInt("MIO_PGX_MAX_CONNS", 4),
		MigrateOnStart:          envBool("MIO_MIGRATE_ON_START", true),
		AccountID:               os.Getenv("MIO_ACCOUNT_ID"),
		NatsURLs:                envCSV("MIO_NATS_URLS", "nats://localhost:4222"),
		AgeKeyVersion:           envInt("MIO_AGE_KEY_VERSION", 1),
		ConversationExternalID:  envFirst("MIO_RECONCILE_CONVERSATION_EXTERNAL_ID", "MIO_CONVERSATION_EXTERNAL_ID"),
		ConversationDisplayName: os.Getenv("MIO_RECONCILE_CONVERSATION_DISPLAY_NAME"),
		ConversationKind:        envStr("MIO_RECONCILE_CONVERSATION_KIND", "CONVERSATION_KIND_CHANNEL_PUBLIC"),
		CliqChannelName:         os.Getenv("MIO_RECONCILE_CLIQ_CHANNEL_NAME"),
		Cursor:                  os.Getenv("MIO_RECONCILE_CURSOR"),
		Limit:                   envInt("MIO_RECONCILE_LIMIT", 100),
	}
	var err error
	cfg.Since, err = parseTimeEnv("MIO_RECONCILE_SINCE")
	if err != nil {
		return config{}, err
	}
	cfg.Until, err = parseTimeEnv("MIO_RECONCILE_UNTIL")
	if err != nil {
		return config{}, err
	}
	if cfg.PostgresDSN == "" {
		return config{}, fmt.Errorf("MIO_POSTGRES_DSN is required")
	}
	if cfg.AccountID == "" {
		return config{}, fmt.Errorf("MIO_ACCOUNT_ID is required")
	}
	if cfg.ConversationExternalID == "" {
		return config{}, fmt.Errorf("MIO_RECONCILE_CONVERSATION_EXTERNAL_ID is required")
	}
	if cfg.PgxMaxConns < 1 {
		cfg.PgxMaxConns = 1
	}
	return cfg, nil
}

func buildCipher(cfg config) (crypto.Cipher, error) {
	if os.Getenv("MIO_AGE_KEY_FILE") != "" {
		return crypto.NewAgeFileCipher("", cfg.AgeKeyVersion)
	}
	if cfg.Env == "dev" {
		return crypto.NewNoopCipher(cfg.Env), nil
	}
	return nil, fmt.Errorf("MIO_AGE_KEY_FILE is required outside dev")
}

func shouldRefresh(c channels.Credential) bool {
	return c.RefreshToken != "" && (c.AccessToken == "" || c.ExpiresAt.IsZero() || time.Until(c.ExpiresAt) < 2*time.Minute)
}

func adapterByChannelType(channelType string) (channels.Adapter, error) {
	for _, adapter := range channels.RegisteredAdapters() {
		if adapter.ChannelType() == channelType {
			return adapter, nil
		}
	}
	return nil, fmt.Errorf("no adapter registered for channel_type %q", channelType)
}

func parseTimeEnv(key string) (time.Time, error) {
	value := os.Getenv(key)
	if value == "" {
		return time.Time{}, nil
	}
	if millis, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.UnixMilli(millis), nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be unix milliseconds or RFC3339: %w", key, err)
	}
	return t, nil
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envFirst(keys ...string) string {
	for _, key := range keys {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	return ""
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
