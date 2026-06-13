package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crashchat-ai/mio/ui/web/internal/adminclient"
	"github.com/crashchat-ai/mio/ui/web/internal/audit"
	"github.com/crashchat-ai/mio/ui/web/internal/auth"
	"github.com/crashchat-ai/mio/ui/web/internal/rest"
)

func main() {
	logger := slog.Default()
	cfg, cleanup, err := configFromEnv(context.Background(), logger)
	if err != nil {
		logger.Error("mio-web configuration failed", "error", err)
		os.Exit(1)
	}
	defer cleanup()

	handler := rest.New(rest.Config{
		Admin:  adminclient.New(cfg.AdminURL),
		Auth:   cfg.Auth,
		Audit:  cfg.Audit,
		Logger: logger,
	})
	handler = auth.CORS(auth.CORSConfig{AllowedOrigins: cfg.CORSOrigins}, handler)

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting mio-web",
			"addr", cfg.Addr,
			"admin_url", cfg.AdminURL,
			"auth_mode", cfg.Auth.Mode(),
			"session_store", cfg.SessionStore)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("failed to shut down mio-web", "error", err)
			os.Exit(1)
		}
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("mio-web exited", "error", err)
			os.Exit(1)
		}
	}
}

type appConfig struct {
	Addr         string
	AdminURL     string
	Auth         *auth.Manager
	Audit        audit.Logger
	SessionStore string
	CORSOrigins  []string
}

func configFromEnv(ctx context.Context, logger *slog.Logger) (appConfig, func(), error) {
	addr := env("MIO_WEB_ADDR", ":8080")
	adminURL := env("MIO_ADMIN_URL", "http://127.0.0.1:9090")
	corsOrigins := auth.ParseCORSOrigins(os.Getenv("MIO_WEB_CORS_ORIGINS"))
	authMode := env("MIO_WEB_AUTH_MODE", auth.ModeGoogle)
	publicURL := strings.TrimRight(os.Getenv("MIO_WEB_PUBLIC_URL"), "/")
	cookieSecure := envBool("MIO_WEB_COOKIE_SECURE", strings.HasPrefix(publicURL, "https://"))
	allowlist := auth.ParseAllowlist(os.Getenv("MIO_WEB_OPERATOR_EMAILS"), os.Getenv("MIO_WEB_OPERATOR_DOMAINS"))
	defaultRole, err := auth.ParseRole(os.Getenv("MIO_WEB_OPERATOR_DEFAULT_ROLE"))
	if err != nil {
		return appConfig{}, nil, err
	}
	roles, err := auth.ParseRoleAssignments(os.Getenv("MIO_WEB_OPERATOR_ROLES"), defaultRole)
	if err != nil {
		return appConfig{}, nil, err
	}
	devRole, err := auth.ParseRole(os.Getenv("MIO_WEB_DEV_OPERATOR_ROLE"))
	if err != nil {
		return appConfig{}, nil, err
	}

	store, auditLogger, storeName, cleanup, err := storesFromEnv(ctx)
	if err != nil {
		return appConfig{}, nil, err
	}

	var provider auth.Provider
	if authMode == auth.ModeGoogle {
		redirectURL := os.Getenv("MIO_WEB_OIDC_REDIRECT_URL")
		if redirectURL == "" && publicURL != "" {
			redirectURL = publicURL + "/auth/callback"
		}
		if os.Getenv("MIO_WEB_GOOGLE_CLIENT_ID") != "" ||
			os.Getenv("MIO_WEB_GOOGLE_CLIENT_SECRET") != "" ||
			redirectURL != "" {
			provider, err = auth.NewGoogleProvider(auth.GoogleConfig{
				ClientID:     os.Getenv("MIO_WEB_GOOGLE_CLIENT_ID"),
				ClientSecret: os.Getenv("MIO_WEB_GOOGLE_CLIENT_SECRET"),
				RedirectURL:  redirectURL,
			})
			if err != nil {
				cleanup()
				return appConfig{}, nil, err
			}
		}
	}

	manager, err := auth.NewManager(auth.Config{
		Mode:         authMode,
		Provider:     provider,
		Store:        store,
		Allowlist:    allowlist,
		Roles:        roles,
		DevRole:      devRole,
		DevIdentity:  auth.Identity{Email: os.Getenv("MIO_WEB_DEV_OPERATOR_EMAIL"), Name: os.Getenv("MIO_WEB_DEV_OPERATOR_NAME")},
		SessionTTL:   envDuration("MIO_WEB_SESSION_TTL", 12*time.Hour),
		CookieSecure: cookieSecure,
		StateSecret:  []byte(os.Getenv("MIO_WEB_STATE_SECRET")),
		Logger:       logger,
	})
	if err != nil {
		cleanup()
		return appConfig{}, nil, err
	}

	return appConfig{
		Addr:         addr,
		AdminURL:     adminURL,
		Auth:         manager,
		Audit:        auditLogger,
		SessionStore: storeName,
		CORSOrigins:  corsOrigins,
	}, cleanup, nil
}

func storesFromEnv(ctx context.Context) (auth.Store, audit.Logger, string, func(), error) {
	dsn := os.Getenv("MIO_WEB_DATABASE_DSN")
	if dsn == "" {
		return auth.NewMemoryStore(), audit.NewMemoryLogger(), "memory", func() {}, nil
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, nil, "", nil, fmt.Errorf("mio-web postgres stores: %w", err)
	}
	store := auth.NewPostgresStore(pool)
	if err := store.CheckSchema(ctx); err != nil {
		pool.Close()
		return nil, nil, "", nil, err
	}
	auditLogger := audit.NewPostgresLogger(pool)
	if err := auditLogger.CheckSchema(ctx); err != nil {
		pool.Close()
		return nil, nil, "", nil, err
	}
	return store, auditLogger, "postgres", pool.Close, nil
}

func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
