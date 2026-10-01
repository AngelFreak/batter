// Package db embeds the SQL migrations so the binary can apply them itself.
package db

import "embed"

// Migrations holds migrations/*.sql, applied in filename order.
//
//go:embed migrations/*.sql
var Migrations embed.FS
