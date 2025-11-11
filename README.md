# go-users

go-users provides user-management commands, queries, migrations, and scope controls that sit behind admin transports. Everything is transport agnostic and can be wired to HTTP, gRPC, jobs, or CLI pipelines.

## Package map
- `service`: builds the service façade and validates injected repositories.
- `command`: lifecycle, invite, password reset, role, profile, preference, and activity handlers.
- `query`: inventory, role, assignment, profile, preference, and activity read models.
- `preferences`: resolver helpers for scoped preference trees.
- `scope`: guard, policies, and resolver utilities.
- `registry`: Bun helpers for registering SQL migrations and schema metadata.
- `docs` and `examples`: runnable references for transports, guards, and schema feeds.

## Prerequisites
- Go 1.23+.
- A repository implementation for each interface located in `pkg/types`.
- Access to a SQL database when using the bundled migrations (PostgreSQL or SQLite).

## Quick start
1. `go get github.com/goliatone/go-users`.
2. Implement the interfaces in `pkg/types` or reuse the Bun repositories under `activity`, `preferences`, and `registry`.
3. Provide a scope resolver and authorization policy if multitenancy is required.
4. Call `service.New` and expose the returned commands and queries through your transport.
5. Run the SQL migrations under `data/sql/migrations` before serving traffic.

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
- `AssignRole` and `UnassignRole`: actor-to-role assignments, with guard checks.
- `ActivityLog`: structured audit trails stored through the configured repository or sink.
- `ProfileUpsert`, `PreferenceUpsert`, `PreferenceDelete`: profile and scoped preference management.

Every command runs through the scope guard before invoking repositories. Hooks fire after each command so transports can sync e-mail, analytics, or caches.

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
- `migrations.TestMigrationsApplyToSQLite` verifies that the SQL stack applies cleanly to SQLite.
- Bun repositories under `activity`, `preferences`, and `registry` are thin wrappers around `bun.DB` and match the interfaces in `pkg/types`.
- You can replace any repository with your own implementation as long as it satisfies the interface.

## Examples
- `examples/commands`: runs the service with in-memory repositories. `go run ./examples/commands`.
- `examples/internal/memory`: shared fixtures for the sample binaries.
- `examples/web`: Go-Fiber admin site showing guard-first controllers, schema registry feeds, and CRUD adapters.

Use the examples to confirm wiring and to capture request traces during development.

## Development workflow
- `./taskfile lint`: runs `go vet` across the module.
- `./taskfile test`: runs `go test ./...`.
- `./taskfile migrate`: exercises the SQLite migration test.
- Standard `go test ./...` and `golangci-lint` setups also work if you prefer direct tool invocations.

## Documentation index
- `docs/USER_MANAGEMENT_REPORT.md`: design notes for the service surface.
- `docs/SERVICE_REFERENCE.md`: detailed command and query inputs and outputs.
- `docs/ACTIVITY.md`, `docs/PROFILES_PREFERENCES.md`, `docs/WORKSPACES.md`: feature-specific guidance.
- `docs/EXAMPLES.md`, `docs/MULTITENANCY.md`, `docs/ROLE_REGISTRY.md`: integration recipes.
- `MIGRATIONS.md`: SQL execution guidance and versioning notes.
- `docs/RELEASE_NOTES.md`: change history.
