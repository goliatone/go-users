package users

import "embed"

// MigrationsFS contains SQL migrations for both PostgreSQL and SQLite.
//
// The migrations are organized in a dialect-aware structure:
//   - Root files (data/sql/migrations/*.sql) contain PostgreSQL migrations
//   - SQLite overrides are in data/sql/migrations/sqlite/*.sql
//
// The go-persistence-bun loader will automatically select the correct
// migrations based on the database dialect being used.
//
// Usage:
//
//	import "io/fs"
//	import users "github.com/goliatone/go-users"
//	import persistence "github.com/goliatone/go-persistence-bun"
//
//	migrationsFS, _ := fs.Sub(users.MigrationsFS, "data/sql/migrations")
//	client.RegisterDialectMigrations(
//	    migrationsFS,
//	    persistence.WithDialectSourceLabel("."),
//	    persistence.WithValidationTargets("postgres", "sqlite"),
//	)
//
//go:embed data/sql/migrations/*.sql data/sql/migrations/sqlite/*.sql
var MigrationsFS embed.FS
