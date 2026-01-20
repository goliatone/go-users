# GUIDE_MIGRATIONS.md

This guide covers database migrations for `go-users`, including the migration architecture, schema details, and strategies for managing migrations in your applications.

---

## Table of Contents

1. [Overview](#overview)
2. [Migration Architecture](#migration-architecture)
3. [Migration Files Structure](#migration-files-structure)
4. [PostgreSQL vs SQLite Support](#postgresql-vs-sqlite-support)
5. [Running Migrations](#running-migrations)
6. [Schema Overview](#schema-overview)
7. [Adding Custom Migrations](#adding-custom-migrations)
8. [Testing Migrations](#testing-migrations)
9. [Rollback Strategies](#rollback-strategies)
10. [Common Patterns](#common-patterns)

---

## Overview

`go-users` provides embedded SQL migrations that create the necessary database tables for user management, roles, activity logging, profiles, and preferences. The migrations support both PostgreSQL and SQLite databases through dialect-aware file organization.

### Key Features

- **Embedded migrations** - SQL files are embedded in the Go binary using `embed.FS`
- **Dialect-aware** - Automatic selection of PostgreSQL or SQLite migrations
- **Versioned** - Sequential numbering ensures consistent ordering
- **Reversible** - Each migration includes both up and down scripts
- **Integration-ready** - Works with `go-persistence-bun` migration runner
- **go-auth aware** - When `go-auth` is present, register only go-users core; standalone installs register auth bootstrap + auth extras.

---

## Migration Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        Your Application                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│   ┌─────────────────┐    ┌─────────────────────────────────┐   │
│   │ go-persistence  │───▶│  Migration Runner               │   │
│   │     -bun        │    │  • Dialect detection            │   │
│   └─────────────────┘    │  • Version tracking             │   │
│           │              │  • Up/Down execution            │   │
│           │              └─────────────────────────────────┘   │
│           │                            │                        │
│           ▼                            ▼                        │
│   ┌─────────────────────┐  ┌─────────────────────────────────┐ │
│   │ users.Core/Auth FS  │  │  Database                       │ │
│   │ (embedded SQL)      │  │  • PostgreSQL                   │ │
│   │                     │  │  • SQLite                       │ │
│   └─────────────────────┘  └─────────────────────────────────┘ │
│           │                                                     │
│           ▼                                                     │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │ data/sql/migrations/                                     │   │
│   │ ├── auth/                                                │   │
│   │ │   ├── 00001_users.up.sql      (PostgreSQL)            │   │
│   │ │   ├── 00001_users.down.sql                            │   │
│   │ │   └── sqlite/               (SQLite overrides)        │   │
│   │ ├── auth_extras/                                         │   │
│   │ │   ├── 00010_social_accounts.up.sql                    │   │
│   │ │   ├── 00010_social_accounts.down.sql                  │   │
│   │ │   └── sqlite/               (SQLite overrides)        │   │
│   │ ├── 00003_custom_roles.up.sql                          │   │
│   │ ├── sqlite/               (core SQLite overrides)       │   │
│   │ └── ...                                                 │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### Registration Flow

```go
// 1. go-users exports embedded filesystems
var MigrationsFS embed.FS           // All migrations (legacy all-in-one)
var CoreMigrationsFS embed.FS       // go-users core tables only
var AuthBootstrapMigrationsFS embed.FS // users/password_reset + auth columns
var AuthExtrasMigrationsFS embed.FS    // social_accounts/user_identifiers

// 2. Optional: Use migrations.Register for centralized registration
import "github.com/goliatone/go-users/migrations"
// init() automatically calls migrations.Register(users.GetCoreMigrationsFS())
// import _ "github.com/goliatone/go-users/migrations/bootstrap" for auth bootstrap
// import _ "github.com/goliatone/go-users/migrations/extras" for auth extras

// 3. Retrieve all registered filesystems
filesystems := migrations.Filesystems()

// 4. Or access directly for go-persistence-bun
coreFS, _ := fs.Sub(users.GetCoreMigrationsFS(), "data/sql/migrations")
```

---

## Migration Files Structure

Migrations follow a numbered naming convention that ensures consistent execution order:

```
data/sql/migrations/
├── auth/
│   ├── 00001_users.up.sql
│   ├── 00001_users.down.sql
│   ├── 00002_user_status.up.sql
│   ├── 00002_user_status.down.sql
│   ├── 00009_user_external_ids.up.sql
│   ├── 00009_user_external_ids.down.sql
│   └── sqlite/
│       ├── 00001_users.up.sql
│       ├── 00001_users.down.sql
│       ├── 00002_user_status.up.sql
│       ├── 00002_user_status.down.sql
│       ├── 00009_user_external_ids.up.sql
│       └── 00009_user_external_ids.down.sql
├── auth_extras/
│   ├── 00010_social_accounts.up.sql
│   ├── 00010_social_accounts.down.sql
│   ├── 00011_user_identifiers.up.sql
│   ├── 00011_user_identifiers.down.sql
│   └── sqlite/
│       ├── 00010_social_accounts.up.sql
│       ├── 00010_social_accounts.down.sql
│       ├── 00011_user_identifiers.up.sql
│       └── 00011_user_identifiers.down.sql
├── 00003_custom_roles.up.sql
├── 00003_custom_roles.down.sql
├── 00004_user_activity.up.sql
├── 00004_user_activity.down.sql
├── 00005_profiles_preferences.up.sql
├── 00005_profiles_preferences.down.sql
├── 00006_custom_roles_metadata.up.sql
├── 00006_custom_roles_metadata.down.sql
├── 00007_custom_roles_order.up.sql
├── 00007_custom_roles_order.down.sql
├── 00008_user_tokens.up.sql
├── 00008_user_tokens.down.sql
└── sqlite/
    ├── 00003_custom_roles.up.sql
    ├── 00003_custom_roles.down.sql
    └── ... (SQLite-specific versions)
```

Auth bootstrap migrations live under `data/sql/migrations/auth`, with SQLite
overrides in `data/sql/migrations/auth/sqlite`. Auth extras live under
`data/sql/migrations/auth_extras` (SQLite overrides in
`data/sql/migrations/auth_extras/sqlite`).

Register auth bootstrap (and auth extras, if used) before core so dependent tables exist.
When `go-auth` migrations are already registered, skip auth bootstrap/auth extras
to avoid duplicate tables.

If you use `GetMigrationsFS()`, register three sub-filesystems:
`data/sql/migrations/auth`, `data/sql/migrations/auth_extras`, and
`data/sql/migrations` (core), since the dialect
loader does not scan nested subfolders.

### Naming Convention

```
{version}_{description}.{direction}.sql
```

- **version**: 5-digit zero-padded number (00001, 00002, ...)
- **description**: Snake_case description of the migration
- **direction**: `up` for applying, `down` for reverting

### Statement Splitting

Use `---bun:split` to separate multiple statements within a single migration file:

```sql
-- auth/00001_users.up.sql
CREATE TABLE users (
    id TEXT NOT NULL PRIMARY KEY,
    ...
);

---bun:split

CREATE TABLE password_reset (
    id TEXT NOT NULL PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    ...
);
```

---

## PostgreSQL vs SQLite Support

`go-users` maintains parallel migration sets for PostgreSQL and SQLite. The key differences are:

| Feature | PostgreSQL | SQLite |
|---------|------------|--------|
| ID type | `TEXT` (UUID strings) | `TEXT` (UUID strings) |
| JSON type | `JSONB` | `TEXT` |
| Timestamp | `TIMESTAMP` | `TIMESTAMP` |
| Boolean | `BOOLEAN` | `BOOLEAN` (0/1) |
| Foreign keys | Enforced by default | Requires `_fk=1` pragma |

### PostgreSQL Migration Example

```sql
-- auth/00001_users.up.sql (PostgreSQL)
CREATE TABLE users (
    id TEXT NOT NULL PRIMARY KEY,
    user_role TEXT NOT NULL DEFAULT 'guest',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### SQLite Migration Example

```sql
-- auth/sqlite/00001_users.up.sql (SQLite)
CREATE TABLE users (
    id TEXT NOT NULL PRIMARY KEY,
    user_role TEXT NOT NULL DEFAULT 'guest',
    metadata TEXT NOT NULL DEFAULT '{}',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### Dialect Selection

The `go-persistence-bun` migration runner automatically selects the correct dialect:

```go
import (
    "io/fs"
    auth "github.com/goliatone/go-auth"
    users "github.com/goliatone/go-users"
    persistence "github.com/goliatone/go-persistence-bun"
)

// Create sub-FS rooted at data/sql/migrations
authFS, _ := fs.Sub(auth.GetMigrationsFS(), "data/sql/migrations")
coreFS, _ := fs.Sub(users.GetCoreMigrationsFS(), "data/sql/migrations")

// Register auth first, then core (migration ordering is lexicographic)
client.RegisterDialectMigrations(
    authFS,
    persistence.WithDialectSourceLabel("."),           // Root = PostgreSQL
    persistence.WithValidationTargets("postgres", "sqlite"),
)
client.RegisterDialectMigrations(
    coreFS,
    persistence.WithDialectSourceLabel("."),           // Root = PostgreSQL
    persistence.WithValidationTargets("postgres", "sqlite"),
)
```

---

## Running Migrations

When running alongside `go-auth`, register `go-auth` migrations first and then
go-users core only. Do not register auth bootstrap or auth extras in that
setup.

### With go-persistence-bun

```go
package main

import (
    "context"
    "database/sql"
    "io/fs"
    "log"

    auth "github.com/goliatone/go-auth"
    "github.com/goliatone/go-persistence-bun"
    users "github.com/goliatone/go-users"
    "github.com/uptrace/bun/dialect/pgdialect"
    _ "github.com/lib/pq"
)

func main() {
    ctx := context.Background()

    // Open database connection
    db, err := sql.Open("postgres", "postgres://user:pass@localhost/mydb?sslmode=disable")
    if err != nil {
        log.Fatal(err)
    }

    // Create persistence client
    cfg := &persistence.Config{
        Driver: "postgres",
    }
    client, err := persistence.New(cfg, db, pgdialect.New())
    if err != nil {
        log.Fatal(err)
    }

    // Register go-auth migrations and apply them first.
    authFS, err := fs.Sub(auth.GetMigrationsFS(), "data/sql/migrations")
    if err != nil {
        log.Fatal(err)
    }
    client.RegisterDialectMigrations(
        authFS,
        persistence.WithDialectSourceLabel("."),
        persistence.WithValidationTargets("postgres", "sqlite"),
    )
    if err := client.Migrate(ctx); err != nil {
        log.Fatal(err)
    }

    // Register go-users core migrations next.
    coreFS, err := fs.Sub(users.GetCoreMigrationsFS(), "data/sql/migrations")
    if err != nil {
        log.Fatal(err)
    }
    client.RegisterDialectMigrations(
        coreFS,
        persistence.WithDialectSourceLabel("."),
        persistence.WithValidationTargets("postgres", "sqlite"),
    )

    // Validate dialect coverage (optional)
    if err := client.ValidateDialects(ctx); err != nil {
        log.Printf("Warning: dialect validation failed: %v", err)
    }

    // Run migrations (core)
    if err := client.Migrate(ctx); err != nil {
        log.Fatal(err)
    }

    // Check migration report
    if report := client.Report(); report != nil && !report.IsZero() {
        log.Printf("Migrations applied: %s", report.String())
    }
}
```

### Standalone Example (No go-auth)

```go
package main

import (
    "context"
    "database/sql"
    "io/fs"
    "log"

    "github.com/goliatone/go-persistence-bun"
    users "github.com/goliatone/go-users"
    "github.com/goliatone/go-users/migrations"
    "github.com/uptrace/bun/dialect/pgdialect"
    _ "github.com/lib/pq"
)

func main() {
    ctx := context.Background()
    db, err := sql.Open("postgres", "postgres://user:pass@localhost/mydb?sslmode=disable")
    if err != nil {
        log.Fatal(err)
    }

    cfg := &persistence.Config{
        Driver: "postgres",
    }
    client, err := persistence.New(cfg, db, pgdialect.New())
    if err != nil {
        log.Fatal(err)
    }

    authFS, err := fs.Sub(users.GetAuthBootstrapMigrationsFS(), "data/sql/migrations/auth")
    if err != nil {
        log.Fatal(err)
    }
    extrasFS, err := fs.Sub(users.GetAuthExtrasMigrationsFS(), "data/sql/migrations/auth_extras")
    if err != nil {
        log.Fatal(err)
    }
    coreFS, err := fs.Sub(users.GetCoreMigrationsFS(), "data/sql/migrations")
    if err != nil {
        log.Fatal(err)
    }

    client.RegisterDialectMigrations(authFS, persistence.WithDialectSourceLabel("."))
    client.RegisterDialectMigrations(extrasFS, persistence.WithDialectSourceLabel("."))
    client.RegisterDialectMigrations(coreFS, persistence.WithDialectSourceLabel("."))

    if err := client.Migrate(ctx); err != nil {
        log.Fatal(err)
    }

    if err := migrations.ValidateAuthSchema(ctx, db, "postgres"); err != nil {
        log.Fatal(err)
    }
}
```

#### Auth Schema Validation

`migrations.ValidateAuthSchema` validates that auth-owned tables expose the
columns go-users relies on. Override the defaults with
`migrations.WithAuthSchemaChecks` if you have a custom schema.

### SQLite Example

```go
package main

import (
    "context"
    "database/sql"
    "io/fs"
    "log"

    auth "github.com/goliatone/go-auth"
    "github.com/goliatone/go-persistence-bun"
    users "github.com/goliatone/go-users"
    "github.com/uptrace/bun/dialect/sqlitedialect"
    "github.com/uptrace/bun/driver/sqliteshim"
)

func main() {
    ctx := context.Background()

    // SQLite with WAL mode and foreign keys
    dsn := "file:mydb.sqlite?_journal_mode=WAL&cache=shared&_fk=1"
    db, err := sql.Open(sqliteshim.ShimName, dsn)
    if err != nil {
        log.Fatal(err)
    }

    cfg := &persistence.Config{
        Driver: "sqlite",
    }
    client, err := persistence.New(cfg, db, sqlitedialect.New())
    if err != nil {
        log.Fatal(err)
    }

    authFS, err := fs.Sub(auth.GetMigrationsFS(), "data/sql/migrations")
    if err != nil {
        log.Fatal(err)
    }
    client.RegisterDialectMigrations(
        authFS,
        persistence.WithDialectSourceLabel("."),
        persistence.WithValidationTargets("postgres", "sqlite"),
    )
    if err := client.Migrate(ctx); err != nil {
        log.Fatal(err)
    }

    coreFS, err := fs.Sub(users.GetCoreMigrationsFS(), "data/sql/migrations")
    if err != nil {
        log.Fatal(err)
    }
    client.RegisterDialectMigrations(
        coreFS,
        persistence.WithDialectSourceLabel("."),
        persistence.WithValidationTargets("postgres", "sqlite"),
    )

    if err := client.Migrate(ctx); err != nil {
        log.Fatal(err)
    }
}
```

### Using the Migrations Registry

For applications that need centralized migration management:

```go
import (
    "github.com/goliatone/go-users/migrations"
)

func init() {
    // Core migrations automatically registered via migrations package init()
    // import _ "github.com/goliatone/go-users/migrations/bootstrap" for auth bootstrap
    // import _ "github.com/goliatone/go-users/migrations/extras" for auth extras
    // You can also register additional migration filesystems:
    migrations.Register(myCustomMigrationsFS)
}

func runMigrations() {
    // Get all registered filesystems
    filesystems := migrations.Filesystems()
    for _, fsys := range filesystems {
        // Apply each filesystem
    }
}
```

---

## Schema Overview

### Users Table (00001)

Core user identity and authentication data:

```sql
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    user_role TEXT NOT NULL DEFAULT 'guest',  -- guest, member, admin, owner
    first_name TEXT NOT NULL,
    last_name TEXT NOT NULL,
    username TEXT NOT NULL UNIQUE,
    profile_picture TEXT,
    email TEXT NOT NULL UNIQUE,
    phone_number TEXT,
    password_hash TEXT,
    metadata JSONB NOT NULL DEFAULT '{}',
    is_email_verified BOOLEAN DEFAULT FALSE,
    login_attempts INTEGER DEFAULT 0,
    login_attempt_at TIMESTAMP,
    loggedin_at TIMESTAMP,
    reseted_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,
    updated_at TIMESTAMP
);
```

**Indexes:**
- `users_username_unique` - Unique username
- `users_email_unique` - Unique email
- `users_is_email_verified_index` - Email verification queries

### Password Reset Table (00001)

Password reset token management:

```sql
CREATE TABLE password_reset (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'unknown',  -- unknown, requested, expired, changed
    jti TEXT,
    issued_at TIMESTAMP,
    expires_at TIMESTAMP,
    used_at TIMESTAMP,
    scope_tenant_id TEXT,
    scope_org_id TEXT,
    reseted_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,
    updated_at TIMESTAMP
);
```

### User Tokens Table (00008)

Invite and registration token registry:

```sql
CREATE TABLE user_tokens (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_type TEXT NOT NULL,
    jti TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'issued',  -- issued, used, expired
    issued_at TIMESTAMP,
    expires_at TIMESTAMP,
    used_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP
);
```

### User Status (00002)

Adds lifecycle state management:

```sql
ALTER TABLE users
    ADD COLUMN status TEXT NOT NULL DEFAULT 'active',  -- pending, active, suspended, disabled, archived
    ADD COLUMN suspended_at TIMESTAMP;
```

### External IDs (00009)

Adds optional external identity mapping columns and a unique index:

```sql
ALTER TABLE users
    ADD COLUMN external_id TEXT,
    ADD COLUMN external_id_provider TEXT;

CREATE UNIQUE INDEX users_external_id_unique
    ON users (external_id_provider, external_id)
    WHERE external_id IS NOT NULL;
```

### Social Accounts (00010)

Optional social profile links for external providers:

```sql
CREATE TABLE social_accounts (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    provider_user_id TEXT NOT NULL,
    email TEXT,
    name TEXT,
    username TEXT,
    avatar_url TEXT,
    access_token TEXT,
    refresh_token TEXT,
    token_expires_at TIMESTAMP,
    profile_data JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP,
    CONSTRAINT uq_social_accounts_provider_id UNIQUE (provider, provider_user_id),
    CONSTRAINT uq_social_accounts_user_provider UNIQUE (user_id, provider)
);
```

### User Identifiers (00011)

Secondary identifiers for auth providers:

```sql
CREATE TABLE user_identifiers (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    identifier TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP,
    CONSTRAINT uq_user_identifiers_provider_id UNIQUE (provider, identifier)
);
```

### Custom Roles (00003)

Role definitions with tenant/org scoping:

```sql
CREATE TABLE custom_roles (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    permissions JSONB NOT NULL DEFAULT '[]',
    is_system BOOLEAN NOT NULL DEFAULT FALSE,
    tenant_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    org_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT NOT NULL,
    updated_by TEXT NOT NULL
);

CREATE TABLE user_custom_roles (
    user_id TEXT NOT NULL,
    role_id TEXT NOT NULL REFERENCES custom_roles(id) ON DELETE CASCADE,
    tenant_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    org_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    assigned_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    assigned_by TEXT NOT NULL,
    PRIMARY KEY (user_id, role_id, tenant_id, org_id)
);
```

**Indexes:**
- `custom_roles_scope_name_idx` - Unique name per tenant/org
- `custom_roles_scope_idx` - Scope-based queries
- `user_custom_roles_scope_idx` - Assignment scope queries
- `user_custom_roles_user_idx` - User role lookups
- `user_custom_roles_role_idx` - Role assignment queries

### User Activity (00004)

Audit logging and activity feeds:

```sql
CREATE TABLE user_activity (
    id TEXT PRIMARY KEY,
    user_id TEXT,
    actor_id TEXT,
    tenant_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    org_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    verb TEXT NOT NULL,
    object_type TEXT,
    object_id TEXT,
    channel TEXT,
    ip TEXT,
    data JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

**Indexes:**
- `user_activity_scope_idx` - Tenant/org timeline queries (ordered by created_at DESC)
- `user_activity_user_idx` - User activity history
- `user_activity_object_idx` - Object-based lookups
- `user_activity_verb_idx` - Verb filtering

### Profiles and Preferences (00005)

User profiles and hierarchical preferences:

```sql
CREATE TABLE user_profiles (
    user_id TEXT PRIMARY KEY,
    display_name TEXT,
    avatar_url TEXT,
    locale TEXT,
    timezone TEXT,
    bio TEXT,
    contact JSONB NOT NULL DEFAULT '{}',
    metadata JSONB NOT NULL DEFAULT '{}',
    tenant_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    org_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_by TEXT NOT NULL
);

CREATE TABLE user_preferences (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    tenant_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    org_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    scope_level TEXT NOT NULL DEFAULT 'user',  -- system, tenant, org, user
    key TEXT NOT NULL,
    value JSONB NOT NULL DEFAULT '{}',
    version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_by TEXT NOT NULL
);
```

**Indexes:**
- `user_profiles_scope_idx` - Profile scope queries
- `user_preferences_scope_key_idx` - Unique preference per scope/key
- `user_preferences_scope_idx` - Scope-based preference queries
- `user_preferences_user_idx` - User preference lookups

### Role Metadata (00006)

Adds role_key and metadata to custom roles:

```sql
ALTER TABLE custom_roles
    ADD COLUMN role_key TEXT,
    ADD COLUMN metadata JSONB NOT NULL DEFAULT '{}';
```

### Role Ordering (00007)

Adds ordering for deterministic role display:

```sql
ALTER TABLE custom_roles
    ADD COLUMN "order" INT NOT NULL DEFAULT 0;
```

---

## Adding Custom Migrations

### Creating Your Own Migrations

For application-specific schema extensions:

```go
package mymigrations

import "embed"

//go:embed sql/*.sql
var CustomMigrationsFS embed.FS
```

```
sql/
├── 00100_custom_user_fields.up.sql
├── 00100_custom_user_fields.down.sql
└── sqlite/
    ├── 00100_custom_user_fields.up.sql
    └── 00100_custom_user_fields.down.sql
```

### Extending the Users Table

```sql
-- 00100_custom_user_fields.up.sql
ALTER TABLE users
    ADD COLUMN department TEXT,
    ADD COLUMN employee_id TEXT UNIQUE,
    ADD COLUMN hire_date DATE;

CREATE INDEX users_department_idx ON users(department);
```

```sql
-- 00100_custom_user_fields.down.sql
DROP INDEX IF EXISTS users_department_idx;
ALTER TABLE users
    DROP COLUMN IF EXISTS department,
    DROP COLUMN IF EXISTS employee_id,
    DROP COLUMN IF EXISTS hire_date;
```

### Registering Custom Migrations

```go
import (
    "io/fs"
    users "github.com/goliatone/go-users"
    mymigrations "myapp/migrations"
)

func setupMigrations(client *persistence.Client) error {
    // Register go-users core migrations (go-auth migrations should be registered first)
    coreFS, _ := fs.Sub(users.GetCoreMigrationsFS(), "data/sql/migrations")
    client.RegisterDialectMigrations(coreFS,
        persistence.WithDialectSourceLabel("."),
        persistence.WithValidationTargets("postgres", "sqlite"),
    )

    // Register custom migrations (higher version numbers)
    customFS, _ := fs.Sub(mymigrations.CustomMigrationsFS, "sql")
    client.RegisterDialectMigrations(customFS,
        persistence.WithDialectSourceLabel("."),
        persistence.WithValidationTargets("postgres", "sqlite"),
    )

    return nil
}
```

---

## Testing Migrations

### Smoke Test Pattern

```go
package migrations_test

import (
    "context"
    "database/sql"
    "io/fs"
    "sort"
    "strings"
    "testing"

    _ "github.com/mattn/go-sqlite3"
    users "github.com/goliatone/go-users"
)

func TestMigrationsApplyToSQLite(t *testing.T) {
    t.Parallel()

    db, err := sql.Open("sqlite3", ":memory:")
    if err != nil {
        t.Fatalf("failed to open sqlite db: %v", err)
    }
    t.Cleanup(func() { _ = db.Close() })

    ctx := context.Background()

    // Load SQLite-specific migrations
    authFS, err := fs.Sub(users.GetAuthBootstrapMigrationsFS(), "data/sql/migrations/auth/sqlite")
    if err != nil {
        t.Fatalf("failed to load auth bootstrap migrations: %v", err)
    }
    if err := applyFilesystem(ctx, db, authFS); err != nil {
        t.Fatalf("failed to apply auth bootstrap migrations: %v", err)
    }

    extrasFS, err := fs.Sub(users.GetAuthExtrasMigrationsFS(), "data/sql/migrations/auth_extras/sqlite")
    if err != nil {
        t.Fatalf("failed to load auth extras migrations: %v", err)
    }
    if err := applyFilesystem(ctx, db, extrasFS); err != nil {
        t.Fatalf("failed to apply auth extras migrations: %v", err)
    }

    coreFS, err := fs.Sub(users.GetCoreMigrationsFS(), "data/sql/migrations/sqlite")
    if err != nil {
        t.Fatalf("failed to load core migrations: %v", err)
    }
    if err := applyFilesystem(ctx, db, coreFS); err != nil {
        t.Fatalf("failed to apply core migrations: %v", err)
    }

    // Verify tables exist
    tables := []string{"users", "password_reset", "user_tokens", "custom_roles", "user_activity", "user_profiles", "user_preferences", "social_accounts", "user_identifiers"}
    for _, table := range tables {
        var name string
        err := db.QueryRowContext(ctx,
            "SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
        ).Scan(&name)
        if err != nil {
            t.Errorf("table %s not found: %v", table, err)
        }
    }
}

func applyFilesystem(ctx context.Context, db *sql.DB, filesystem fs.FS) error {
    entries, err := fs.Glob(filesystem, "*.up.sql")
    if err != nil {
        return err
    }
    sort.Strings(entries)

    for _, entry := range entries {
        sqlBytes, err := fs.ReadFile(filesystem, entry)
        if err != nil {
            return err
        }
        statements := splitStatements(string(sqlBytes))
        for _, stmt := range statements {
            if strings.TrimSpace(stmt) == "" {
                continue
            }
            if _, err := db.ExecContext(ctx, stmt); err != nil {
                return err
            }
        }
    }
    return nil
}

func splitStatements(sql string) []string {
    parts := strings.Split(sql, ";")
    out := make([]string, 0, len(parts))
    for _, part := range parts {
        part = strings.TrimSpace(part)
        if part != "" {
            out = append(out, part)
        }
    }
    return out
}
```

### Integration Test with Full Runner

```go
func TestMigrationsWithPersistence(t *testing.T) {
    ctx := context.Background()

    db, err := sql.Open(sqliteshim.ShimName, ":memory:")
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() { _ = db.Close() })

    cfg := &persistence.Config{Driver: "sqlite"}
    client, err := persistence.New(cfg, db, sqlitedialect.New())
    if err != nil {
        t.Fatal(err)
    }

    authFS, _ := fs.Sub(auth.GetMigrationsFS(), "data/sql/migrations")
    client.RegisterDialectMigrations(authFS,
        persistence.WithDialectSourceLabel("."),
        persistence.WithValidationTargets("postgres", "sqlite"),
    )
    if err := client.Migrate(ctx); err != nil {
        t.Fatalf("migration failed: %v", err)
    }

    coreFS, _ := fs.Sub(users.GetCoreMigrationsFS(), "data/sql/migrations")
    client.RegisterDialectMigrations(coreFS,
        persistence.WithDialectSourceLabel("."),
        persistence.WithValidationTargets("postgres", "sqlite"),
    )
    if err := client.Migrate(ctx); err != nil {
        t.Fatalf("migration failed: %v", err)
    }

    report := client.Report()
    if report == nil || report.IsZero() {
        t.Error("expected migration report")
    }
}
```

### Testing Rollbacks

```go
func TestMigrationRollback(t *testing.T) {
    ctx := context.Background()

    db, err := sql.Open(sqliteshim.ShimName, ":memory:")
    if err != nil {
        t.Fatal(err)
    }

    cfg := &persistence.Config{Driver: "sqlite"}
    client, _ := persistence.New(cfg, db, sqlitedialect.New())

    authFS, _ := fs.Sub(auth.GetMigrationsFS(), "data/sql/migrations")
    client.RegisterDialectMigrations(authFS,
        persistence.WithDialectSourceLabel("."),
    )
    if err := client.Migrate(ctx); err != nil {
        t.Fatal(err)
    }

    coreFS, _ := fs.Sub(users.GetCoreMigrationsFS(), "data/sql/migrations")
    client.RegisterDialectMigrations(coreFS,
        persistence.WithDialectSourceLabel("."),
    )
    if err := client.Migrate(ctx); err != nil {
        t.Fatal(err)
    }

    // Rollback one migration
    if err := client.Rollback(ctx); err != nil {
        t.Fatal(err)
    }

    // Verify the last migration was reverted
    // (depends on your rollback implementation)
}
```

---

## Rollback Strategies

### Automatic Rollback (Single Step)

```go
// Roll back the most recent migration
if err := client.Rollback(ctx); err != nil {
    log.Printf("rollback failed: %v", err)
}
```

### Manual Rollback to Version

```go
// Roll back to a specific version
targetVersion := int64(5) // Roll back to migration 00005
if err := client.RollbackTo(ctx, targetVersion); err != nil {
    log.Printf("rollback to version %d failed: %v", targetVersion, err)
}
```

### Full Rollback

```go
// Roll back all migrations (use with caution!)
for {
    if err := client.Rollback(ctx); err != nil {
        if errors.Is(err, persistence.ErrNoMigrationsToRollback) {
            break
        }
        log.Fatal(err)
    }
}
```

### Safe Rollback Practices

1. **Always backup before rollback** in production
2. **Test rollbacks in staging** before production
3. **Use transactions** when possible (PostgreSQL)
4. **Verify data integrity** after rollback

```go
func safeRollback(ctx context.Context, client *persistence.Client) error {
    // Check current version
    currentVersion, err := client.CurrentVersion(ctx)
    if err != nil {
        return fmt.Errorf("failed to get current version: %w", err)
    }

    log.Printf("Current migration version: %d", currentVersion)
    log.Printf("Rolling back one migration...")

    if err := client.Rollback(ctx); err != nil {
        return fmt.Errorf("rollback failed: %w", err)
    }

    newVersion, _ := client.CurrentVersion(ctx)
    log.Printf("New migration version: %d", newVersion)

    return nil
}
```

---

## Common Patterns

### Environment-Based Migration Control

```go
func runMigrations(ctx context.Context, client *persistence.Client, env string) error {
    migrateAll := func(ctx context.Context) error {
        authFS, _ := fs.Sub(auth.GetMigrationsFS(), "data/sql/migrations")
        client.RegisterDialectMigrations(authFS,
            persistence.WithDialectSourceLabel("."),
            persistence.WithValidationTargets("postgres", "sqlite"),
        )
        if err := client.Migrate(ctx); err != nil {
            return err
        }

        coreFS, _ := fs.Sub(users.GetCoreMigrationsFS(), "data/sql/migrations")
        client.RegisterDialectMigrations(coreFS,
            persistence.WithDialectSourceLabel("."),
            persistence.WithValidationTargets("postgres", "sqlite"),
        )
        return client.Migrate(ctx)
    }

    switch env {
    case "development":
        // Always migrate in development
        return migrateAll(ctx)

    case "testing":
        // Fresh database for each test run
        if err := client.Reset(ctx); err != nil {
            return err
        }
        return migrateAll(ctx)

    case "production":
        // Validate before migrating
        if err := client.ValidateDialects(ctx); err != nil {
            return fmt.Errorf("dialect validation failed: %w", err)
        }
        return migrateAll(ctx)

    default:
        return migrateAll(ctx)
    }
}
```

### Migration Status Reporting

```go
func reportMigrationStatus(ctx context.Context, client *persistence.Client) {
    report := client.Report()
    if report == nil || report.IsZero() {
        log.Println("No migrations applied")
        return
    }

    log.Printf("Migration Report:\n%s", report.String())

    // Log applied migrations
    for _, m := range report.Applied {
        log.Printf("  Applied: %s", m.Name)
    }

    // Log pending migrations
    for _, m := range report.Pending {
        log.Printf("  Pending: %s", m.Name)
    }
}
```

### Combining Multiple Migration Sources

```go
func registerAllMigrations(client *persistence.Client) error {
    sources := []struct {
        name string
        fsys fs.FS
        path string
    }{
        {"go-auth", auth.GetMigrationsFS(), "data/sql/migrations"},
        {"go-users-core", users.GetCoreMigrationsFS(), "data/sql/migrations"},
        {"custom", customMigrationsFS, "sql"},
    }

    for _, src := range sources {
        subFS, err := fs.Sub(src.fsys, src.path)
        if err != nil {
            return fmt.Errorf("failed to load %s migrations: %w", src.name, err)
        }

        client.RegisterDialectMigrations(subFS,
            persistence.WithDialectSourceLabel("."),
            persistence.WithValidationTargets("postgres", "sqlite"),
        )
    }

    return nil
}
```

---

## Summary

The `go-users` migration system provides:

- **Embedded SQL migrations** for PostgreSQL and SQLite
- **Dialect-aware file organization** with automatic selection
- **Sequential versioning** for consistent ordering
- **Reversible migrations** with up/down scripts
- **Integration with go-persistence-bun** for migration execution

Key tables created:
- `users` - Core user identity
- `password_reset` - Password reset tokens
- `user_tokens` - Invite and registration token registry
- `custom_roles` - Role definitions
- `user_custom_roles` - Role assignments
- `user_activity` - Audit logging
- `user_profiles` - User profiles
- `user_preferences` - Hierarchical preferences

For more details, see:
- [GUIDE_REPOSITORIES.md](GUIDE_REPOSITORIES.md) - Repository implementations
- [GUIDE_TESTING.md](GUIDE_TESTING.md) - Testing strategies
- [go-persistence-bun documentation](https://github.com/goliatone/go-persistence-bun)
