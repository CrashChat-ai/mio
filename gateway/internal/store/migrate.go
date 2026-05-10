package store

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// MigrationsFS must be set by the gateway entrypoint (cmd/gateway/main.go)
// before MigrateUp is called. Using an injected fs.FS avoids the //go:embed
// path restriction (embed cannot traverse .. from internal/store to
// gateway/migrations). The entrypoint embeds the migrations adjacent to itself
// and injects the sub-FS here.
var MigrationsFS fs.FS

// MigrateUp runs all pending up-migrations against the given DSN.
// Returns nil if already at the latest version (ErrNoChange is swallowed).
// On dirty-state failure, manual `migrate force` is required — acceptable
// for the POC's 4-table schema.
func MigrateUp(dsn string) error {
	if MigrationsFS == nil {
		return fmt.Errorf("store: MigrationsFS not set — call store.SetMigrationsFS before MigrateUp")
	}
	src, err := iofs.New(MigrationsFS, ".")
	if err != nil {
		return fmt.Errorf("store: migrate source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, dsn)
	if err != nil {
		return fmt.Errorf("store: migrate init: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("store: migrate up: %w", err)
	}
	return nil
}
