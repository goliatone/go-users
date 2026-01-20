# go-users

`go-users` provides user management commands, queries, migrations, and scope controls that sit behind admin transports. Everything is transport agnostic and can be wired to HTTP, gRPC, jobs, or CLI pipelines.

## Package map

- `service`: builds the service façade and validates injected repositories.
- `command`: lifecycle, invite, password reset, role, profile, preference, and activity handlers.
- `query`: inventory, role, assignment, profile, preference, and activity read models.
- `preferences`: resolver helpers for scoped preference trees.
- `scope`: guard, policies, and resolver utilities.
- `registry`: Bun helpers for registering SQL migrations and schema metadata.
- `activity`: Bun repository, ActivitySink helpers, and fixtures for audit logging (see `activity/README.md`).
- `docs` and `examples`: runnable references for transports, guards, and schema feeds.

## Prerequisites

- Go 1.23+.
- A repository implementation for each interface located in `pkg/types`.
- Access to a SQL database when using the bundled migrations (PostgreSQL or SQLite).

## Quick start

1. `go get github.com/goliatone/go-users`.
2. Implement the interfaces in `pkg/types` or reuse the Bun repositories under `activity`, `preferences`, and `registry`.
3. Provide a scope resolver and authorization policy if multitenancy is required.
4. Wire activity by injecting an `ActivitySink`/`ActivityRepository` (Bun or custom) and use helpers (`activity.BuildRecordFromActor` or `activity.BuildRecordFromUUID`) to construct records; add a channel via `activity.WithChannel` and override scope/timestamps via `activity.WithTenant`, `activity.WithOrg`, or `activity.WithOccurredAt` when needed.
5. Call `service.New` and expose the returned commands and queries through your transport.
6. Run the SQL migrations under `data/sql/migrations` before serving traffic.

### Wiring the service

```go
package main

import (
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/service"
)

type Dependencies struct {
	AuthRepo             types.AuthRepository
	InventoryRepo        types.UserInventoryRepository
	ActivityRepo         types.ActivityRepository
	ActivitySink         types.ActivitySink
	ProfileRepo          types.ProfileRepository
	PreferenceRepo       types.PreferenceRepository
	PreferenceResolver   service.PreferenceResolver
	RoleRegistry         types.RoleRegistry
	ScopeResolver        types.ScopeResolver
	AuthorizationPolicy  types.AuthorizationPolicy
	Hooks                types.Hooks
	Logger               types.Logger
}

func buildService(deps Dependencies) *service.Service {
	return service.New(service.Config{
		AuthRepository:       deps.AuthRepo,
		InventoryRepository:  deps.InventoryRepo,
		ActivityRepository:   deps.ActivityRepo,
		ActivitySink:         deps.ActivitySink,
		ProfileRepository:    deps.ProfileRepo,
		PreferenceRepository: deps.PreferenceRepo,
		RoleRegistry:         deps.RoleRegistry,
		PreferenceResolver:   deps.PreferenceResolver,
		ScopeResolver:        deps.ScopeResolver,
		AuthorizationPolicy:  deps.AuthorizationPolicy,
		Hooks:                deps.Hooks,
		Logger:               deps.Logger,
	})
}
```

Check `service.Service.Ready` or `HealthCheck` before wiring transports.

## Commands

- `UserLifecycleTransition` and `BulkUserTransition`: lifecycle state changes with policy enforcement.
- `UserInvite` and `UserPasswordReset`: invite token and reset token workflows.
- `CreateRole`, `UpdateRole`, `DeleteRole`: custom role CRUD with registry notifications.
- `AssignRole` and `UnassignRole`: "actor-to-role" assignments, with guard checks.
- `ActivityLog`: structured audit trails stored through the configured repository or sink.
- `ProfileUpsert`, `PreferenceUpsert`, `PreferenceDelete`: profile and scoped preference management.

Every command runs through the scope guard before invoking repositories. Hooks fire after each command so transports can sync email, analytics, or caches.

## Queries

- `UserInventory`: list, filter, and search users.
- `RoleList` and `RoleDetail`: role registry lookups.
- `RoleAssignments`: view assignments per role or user.
- `ActivityFeed` and `ActivityStats`: feed and aggregate views backed by Bun repositories.
- `ProfileDetail` and `Preferences`: scoped profile and preference snapshots.

Queries also rely on the guard to derive the effective scope passed to repositories.

## Scope guard

- Provide a `types.ScopeResolver` that understands tenant, workspace, or organization hints carried in the request.
- Provide a `types.AuthorizationPolicy` that asserts whether the actor can operate in the requested scope.
- Call `service.ScopeGuard()` when building HTTP or job adapters so the same guard instance drives controllers.
- The guard runs before go-crud controllers inside `crudguard.Adapter` (see `examples/web`).

More details live in `docs/MULTITENANCY.md` and `docs/WORKSPACES.md`.

## Storage and migrations

- SQL definitions live under `data/sql/migrations`. Files are numbered and include `.up.sql` and `.down.sql`.
- Register migrations through `migrations.Register(fsys)` and feed the returned filesystems to your runner.
- Standalone installs should register auth bootstrap + auth extras (social accounts/identifiers) before core; go-auth installs should register core only.
- `migrations.TestMigrationsApplyToSQLite` verifies that the SQL stack applies cleanly to SQLite.
- Bun repositories under `activity`, `preferences`, and `registry` are thin wrappers around `bun.DB` and match the interfaces in `pkg/types`.
- You can replace any repository with your own implementation as long as it satisfies the interface.

### User tables

`users` stores core auth identities and lifecycle state. Use `user_role` for the built-in auth tier; use custom roles for app-specific capabilities.

- `id`: UUID primary key.
- `user_role`: core auth tier (`guest`, `member`, `admin`, `owner`).
- `first_name`/`last_name`: required user display names.
- `username`: unique login handle.
- `profile_picture`: optional avatar URL.
- `email`: unique login email.
- `phone_number`: optional contact number.
- `password_hash`: stored password hash (nullable for external providers).
- `metadata`: JSON object for provider or app-specific attributes.
- `is_email_verified`: email verification flag.
- `login_attempts`/`login_attempt_at`: throttling and lockout tracking.
- `loggedin_at`: last successful login timestamp.
- `reseted_at`: last password reset timestamp.
- `status`: lifecycle state (`pending`, `active`, `suspended`, `disabled`, `archived`).
- `suspended_at`: suspension timestamp for audit.
- `created_at`/`updated_at`, `deleted_at`: audit and soft delete timestamps.

`password_reset` tracks reset requests and their lifecycle.

- `id`: UUID primary key.
- `user_id`: user receiving the reset.
- `email`: email used for the reset flow.
- `status`: reset lifecycle (`unknown`, `requested`, `expired`, `changed`).
- `reseted_at`: completion timestamp.
- `created_at`/`updated_at`, `deleted_at`: audit and soft delete timestamps.

### Activity tables

`user_activity` stores audit log entries and feed events.

- `id`: UUID primary key.
- `user_id`: target user (optional).
- `actor_id`: actor who performed the action.
- `tenant_id`/`org_id`: scope identifiers.
- `verb`: action name (e.g. `role.assigned`).
- `object_type`/`object_id`: optional object for the action.
- `channel`: optional grouping (e.g. `settings`).
- `ip`: optional actor IP address.
- `data`: JSON payload with action-specific details.
- `created_at`: event timestamp.

### Profile and preference tables

`user_profiles` stores profile attributes separate from core auth fields.

- `user_id`: primary key and foreign key to users.
- `display_name`: friendly name for UI.
- `avatar_url`: profile avatar URL.
- `locale`: locale code (e.g. `en-US`).
- `timezone`: IANA timezone string.
- `bio`: optional profile summary.
- `contact`: JSON object for structured contact data.
- `metadata`: JSON object for app-specific profile attributes.
- `tenant_id`/`org_id`: scope identifiers.
- `created_at`/`updated_at`, `created_by`/`updated_by`: audit fields.

`user_preferences` stores scoped preference values.

- `id`: UUID primary key.
- `user_id`: user receiving the preference.
- `tenant_id`/`org_id`: scope identifiers.
- `scope_level`: preference tier (`system`, `tenant`, `org`, `user`).
- `key`: preference key (case insensitive).
- `value`: JSON payload for the preference.
- `version`: monotonically increasing version per key/scope.
- `created_at`/`updated_at`, `created_by`/`updated_by`: audit fields.

### Role registry tables

`custom_roles` stores role definitions scoped by tenant and org. Use `name` as the short display label, `description` as the longer admin facing explanation, and `role_key` as the stable machine key for grouping/filtering.

- `id`: UUID primary key.
- `name`: short display label shown in admin UIs, unique per scope.
- `description`: longer help text for admins, not intended for filtering.
- `role_key`: optional, stable key for app level grouping (e.g. editor).
- `permissions`: JSON array of permission slugs.
- `metadata`: JSON object for app specific attributes or UI hints.
- `is_system`: marks built-in roles versus admin defined roles.
- `tenant_id`/`org_id`: scope identifiers.
- `created_at`/`updated_at`, `created_by`/`updated_by`: audit fields.

`user_custom_roles` stores role assignments within the same scope.

- `user_id`: assigned user.
- `role_id`: assigned role.
- `tenant_id`/`org_id`: scope identifiers.
- `assigned_at`/`assigned_by`: audit fields.

## Examples

- `examples/commands`: runs the service with in-memory repositories. `go run ./examples/commands`.
- `examples/internal/memory`: shared fixtures for the sample binaries.
- `examples/web`: Go Fiber admin site showing guard first controllers, schema registry feeds, and CRUD adapters.

Use the examples to confirm wiring and to capture request traces during development.

### Activity helper usage

The activity helpers keep DI minimal (`ActivitySink.Log(ctx, ActivityRecord)`) and cover both request and background flows:

```go
// From an HTTP request with go-auth middleware.
actor := &auth.ActorContext{
    ActorID:  adminID.String(),
    TenantID: tenantID.String(),
    Role:     "admin",
}
record, err := activity.BuildRecordFromActor(actor,
    "settings.updated",
    "settings",
    "global",
    map[string]any{"path": "ui.theme", "from": "light", "to": "dark"},
    activity.WithChannel("settings"),
)

// From a background worker with only an actor UUID.
record, err = activity.BuildRecordFromUUID(adminID,
    "export.completed",
    "export.job",
    jobID,
    map[string]any{"format": "csv", "count": 120},
    activity.WithTenant(tenantID),
    activity.WithOrg(orgID),
    activity.WithOccurredAt(startedAt),
)

if err != nil {
    return err
}
if err := svc.ActivitySink.Log(ctx, record); err != nil {
    return err
}
```

Options:

- `WithChannel` for module level filtering.
- `WithTenant` / `WithOrg` to set scope when not present on the actor context.
- `WithOccurredAt` to override the default `time.Now().UTC()`.

See `activity/README.md` for wiring tips and `docs/ACTIVITY.md` for verb/object conventions.

## Development workflow

- `./taskfile lint`: runs `go vet` across the module.
- `./taskfile test`: runs `go test ./...`.
- `./taskfile migrate`: exercises the SQLite migration test.
- Standard `go test ./...` and `golangci-lint` setups also work if you prefer direct tool invocations.

## Documentation index

- `docs/USER_MANAGEMENT_REPORT.md`: design notes for the service surface.
- `docs/SERVICE_REFERENCE.md`: detailed command and query inputs and outputs.
- `docs/ACTIVITY.md`, `docs/PROFILES_PREFERENCES.md`, `docs/WORKSPACES.md`: feature specific guidance.
- `docs/EXAMPLES.md`, `docs/MULTITENANCY.md`, `docs/ROLE_REGISTRY.md`: integration recipes.
- `MIGRATIONS.md`: SQL execution guidance and versioning notes.
- `docs/RELEASE_NOTES.md`: change history.
