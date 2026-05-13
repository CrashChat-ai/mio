package main

import (
	"github.com/crashchat-ai/mio/services/gateway/store"
	"github.com/crashchat-ai/mio/services/gateway/store/migrations"
)

// init wires the shared migration FS into the store package. Both
// cmd/gateway and cmd/admin import the same migrations/ package so the
// embed.FS is built exactly once; cmd/gateway alone calls MigrateUp,
// while cmd/admin only checks that the schema is present (avoids
// golang-migrate lock contention from two binaries racing on boot).
func init() {
	store.MigrationsFS = migrations.FS
}
