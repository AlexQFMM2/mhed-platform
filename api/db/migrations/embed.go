package migrations

import "embed"

// Files contains all append-only Goose migrations.
//
//go:embed *.sql
var Files embed.FS
