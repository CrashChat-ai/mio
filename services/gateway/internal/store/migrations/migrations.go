// Package migrations exposes the embedded SQL migration set as an fs.FS
// for golang-migrate. Both cmd/gateway (which runs migrations) and
// cmd/admin (which only asserts they ran) import this package.
//
// Only cmd/gateway should ever call store.MigrateUp(); the admin binary
// asserts schema presence via a SELECT on the credentials table and
// refuses to start if absent. Multiple processes calling MigrateUp
// concurrently risks the golang-migrate advisory-lock contention path.
package migrations

import (
	"embed"
	"io/fs"
)

//go:embed *.sql
var raw embed.FS

// FS exposes the embedded migration files at the top-level path expected
// by golang-migrate's iofs source (`./<version>_<name>.{up,down}.sql`).
// embed paths cannot have a leading "./" so we publish the raw FS as-is —
// the migrations live directly at the package root.
var FS fs.FS = raw
