package users

import "embed"

//go:embed data/sql/migrations/*.sql
var migrationsFS embed.FS

// GetMigrationsFS exposes the SQL migration files so host applications can
// register them with go-persistence-bun (or another migration runner).
func GetMigrationsFS() embed.FS {
	return migrationsFS
}
