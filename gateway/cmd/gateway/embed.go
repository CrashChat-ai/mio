package main

import (
	"embed"
	"io/fs"

	"github.com/crashchat-ai/mio/gateway/internal/store"
)

// migrationsRaw embeds all SQL migration files from migrations/ (adjacent to
// this file in cmd/gateway/migrations/). The embed path is relative to
// cmd/gateway/ with no .. traversal, satisfying Go embed rules.
//
//go:embed migrations/*.sql
var migrationsRaw embed.FS

func init() {
	// Strip the "migrations" prefix so golang-migrate's iofs driver
	// sees files directly at "./<version>_name.{up,down}.sql".
	sub, err := fs.Sub(migrationsRaw, "migrations")
	if err != nil {
		panic("embed: sub migrations: " + err.Error())
	}
	store.MigrationsFS = sub
}
