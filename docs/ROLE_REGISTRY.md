# Role Registry Extension Guide

Phase 3 introduces a pluggable role registry so go-admin/go-cms can manage custom roles without touching the upstream auth provider. The default implementation (`registry.RoleRegistry`) composes `go-repository-bun` repositories, runs on any Bun-compatible database, and emits `types.RoleEvent` callbacks for cache invalidation or notification fan-out.

## Schema

The migration `000002_custom_roles.sql` creates:

- `custom_roles` – scoped by `tenant_id`/`org_id`, stores permissions as JSON, and tracks created/updated actors.
- `user_custom_roles` – intersection table with composite PK (`user_id`, `role_id`, `tenant_id`, `org_id`) so multi-tenant assignments remain isolated.

Both tables default scope columns to the all-zero UUID which represents “global” roles in single-tenant setups.

## Registry API

`pkg/types.RoleRegistry` exposes:

- `CreateRole`, `UpdateRole`, `DeleteRole` – manage role definitions via `types.RoleMutation` and return `types.RoleDefinition`.
- `AssignRole`, `UnassignRole` – attach/detach users with scoped enforcement and auditing metadata.
- `ListRoles`, `GetRole` – paginate roles for admin UI filters via `types.RoleFilter`.
- `ListAssignments` – power dashboards that need to display which users inherit a role.

Each mutating method triggers `hooks.AfterRoleChange` with a `types.RoleEvent` payload so hosts can fan-out cache busts or queue messages.

## Swapping Implementations

Applications with bespoke storage can satisfy `types.RoleRegistry` by:

1. Implementing the interface (e.g., wrapping go-repository-cache or a remote service).
2. Wiring the implementation into `users.Config.RoleRegistry`.
3. Optional: invoking `hooks.AfterRoleChange` after persistence to keep downstream consumers informed.

The Bun implementation accepts the following config to keep DI flexible:

```go
registry.NewRoleRegistry(registry.RoleRegistryConfig{
    DB:          bunDB,
    Roles:       customRepo,        // optional override
    Assignments: customAssignRepo,  // optional override
    Clock:       customClock,
    Hooks:       hooks,
    Logger:      logger,
})
```

Override the repositories if you want to wrap them with caching decorators or feature flags. Otherwise, supplying `DB` is enough for the registry to create default repos.

## Hook Actions

Role events use the following actions:

- `role.created`, `role.updated`, `role.deleted`
- `role.assigned`, `role.unassigned`

Each event includes `RoleEvent.Role` with the current definition plus the `ScopeFilter`, `ActorID`, and optional `UserID` (for assignments). Downstream services can inspect these payloads to fan-out notifications or rebuild caches.
