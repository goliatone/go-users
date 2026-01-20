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

// CoreMigrationsFS contains go-users migrations that extend the auth users table
// (roles, activity, profiles/preferences, tokens). It omits auth bootstrap tables.
//
//go:embed data/sql/migrations/00003_custom_roles*.sql data/sql/migrations/00004_user_activity*.sql data/sql/migrations/00005_profiles_preferences*.sql data/sql/migrations/00006_custom_roles_metadata*.sql data/sql/migrations/00007_custom_roles_order*.sql data/sql/migrations/00008_user_tokens*.sql data/sql/migrations/sqlite/00003_custom_roles*.sql data/sql/migrations/sqlite/00004_user_activity*.sql data/sql/migrations/sqlite/00005_profiles_preferences*.sql data/sql/migrations/sqlite/00006_custom_roles_metadata*.sql data/sql/migrations/sqlite/00007_custom_roles_order*.sql data/sql/migrations/sqlite/00008_user_tokens*.sql
var CoreMigrationsFS embed.FS

// AuthBootstrapMigrationsFS contains the minimum auth tables/columns needed to
// run go-users without go-auth (users, status, external IDs).
//
//go:embed data/sql/migrations/00001_users*.sql data/sql/migrations/00002_user_status*.sql data/sql/migrations/00009_user_external_ids*.sql data/sql/migrations/sqlite/00001_users*.sql data/sql/migrations/sqlite/00002_user_status*.sql data/sql/migrations/sqlite/00009_user_external_ids*.sql
var AuthBootstrapMigrationsFS embed.FS
