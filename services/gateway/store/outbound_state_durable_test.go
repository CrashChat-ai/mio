package store

import (
	"context"
	"os"
	"testing"

	"github.com/crashchat-ai/mio/services/gateway/store/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func durableTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("MIO_TEST_DSN")
	if dsn == "" {
		t.Skip("MIO_TEST_DSN not set; skipping durable outbound-state integration tests")
	}
	MigrationsFS = migrations.FS
	if err := MigrateUp(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestDurableOutboundState_SurvivesRestart(t *testing.T) {
	pool := durableTestPool(t)
	ctx := t.Context()

	first := NewDurableOutboundState(pool, nil)
	first.Set(ctx, "cmd-restart-1", "acct-1", "ext-99")

	// Fresh instance = restarted process with cold cache.
	second := NewDurableOutboundState(pool, nil)
	extID, ok := second.Get(ctx, "cmd-restart-1")
	if !ok || extID != "ext-99" {
		t.Fatalf("want ext-99 from DB after restart, got %q ok=%v", extID, ok)
	}

	// Now cached: row deletion must not affect reads.
	if _, err := pool.Exec(ctx, `DELETE FROM outbound_state WHERE send_command_id = 'cmd-restart-1'`); err != nil {
		t.Fatal(err)
	}
	if extID, ok := second.Get(ctx, "cmd-restart-1"); !ok || extID != "ext-99" {
		t.Fatalf("want cache hit after delete, got %q ok=%v", extID, ok)
	}
}

func TestDurableOutboundState_UpsertOverwrites(t *testing.T) {
	pool := durableTestPool(t)
	ctx := t.Context()

	s := NewDurableOutboundState(pool, nil)
	s.Set(ctx, "cmd-up-1", "acct-1", "ext-1")
	s.Set(ctx, "cmd-up-1", "acct-1", "ext-2")

	fresh := NewDurableOutboundState(pool, nil)
	if extID, _ := fresh.Get(ctx, "cmd-up-1"); extID != "ext-2" {
		t.Fatalf("want ext-2 after upsert, got %q", extID)
	}
}

func TestDurableOutboundState_MissReturnsFalse(t *testing.T) {
	pool := durableTestPool(t)
	s := NewDurableOutboundState(pool, nil)
	if _, ok := s.Get(t.Context(), "cmd-never-existed"); ok {
		t.Fatal("want miss")
	}
}
