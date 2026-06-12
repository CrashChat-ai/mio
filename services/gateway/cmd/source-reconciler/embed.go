package main

import (
	"github.com/crashchat-ai/mio/services/gateway/store"
	"github.com/crashchat-ai/mio/services/gateway/store/migrations"
)

func init() {
	store.MigrationsFS = migrations.FS
}
