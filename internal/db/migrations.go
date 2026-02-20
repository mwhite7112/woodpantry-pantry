package db

import "embed"

// MigrationsFS holds the embedded SQL migration files.
//
//go:embed migrations/*.sql
var MigrationsFS embed.FS
