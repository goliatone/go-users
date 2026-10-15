# Role Registry Extension Guide

Phase 3 introduces a pluggable role registry so go-admin/go-cms can manage custom roles without touching the upstream auth provider. The default implementation (`registry.RoleRegistry`) composes `go-repository-bun` repositories, runs on any Bun-compatible database, and emits `types.RoleEvent` callbacks for cache invalidation or notification fan-out.

## Schema

Migration `00003_custom_roles.up.sql` creates the base tables; `00010_role_assignment_scopes.tx.up.sql` extends assignment identity:

- `custom_roles` – scoped by `tenant_id`/`org_id`, stores permissions as JSON, and tracks created/updated actors.
- `user_custom_roles` – intersection table with composite PK (`user_id`, `role_id`, `tenant_id`, `org_id`, `scope_key`). `labels` stores canonical custom scope dimensions; `scope_key` is their deterministic SHA-256 identity (empty for unlabeled grants).

Both tables default scope columns to the all-zero UUID which represents “global” roles in single-tenant setups.

## Catalog and Assignment Scopes

The Bun role catalog supports global/tenant/org namespaces. `CreateRole`, `GetRole`, `ListRoles`, `UpdateRole` and `DeleteRole` return `registry.ErrUnsupportedRoleScope` when passed nonempty `Scope.Labels`. Create a definition once in its tenant/org catalog, then assign that same role ID into different labeled scopes within the same tenant/org. There is no implicit fallback to a global definition in a tenant/org catalog. The optional `types.ExactScopeRoleRegistry` contract adds `AssignRoleFromCatalog(ctx, types.ScopedRoleGrant{...})` to address a definition in an explicit catalog independently from its assignment scope. It supports, for example, a tenant/org catalog role assigned into a Group-only scope. Legacy `AssignRole` retains its same-tenant/org catalog lookup.

Assignments persist the entire label map. Keys are trimmed and lowercased; blank keys, nil IDs and conflicting aliases return `types.ErrInvalidScopeLabels`. All assignment reads and deletes match the complete normalized scope: omitting labels selects only unlabeled grants, and a `group` filter does not also select grants carrying `group` plus `team`. `RoleEvent.Scope` carries normalized assignment labels. Catalog events carry tenant/org scope only; assignment events do not populate `RoleEvent.Role`.

Apply migration 00010 before using the updated Bun assignment model. Existing grants keep their scope and assignment metadata. Custom assignment repositories must adopt the additional fields and composite identity. Upgrade all readers/writers before creating labeled grants: older binaries ignore labels when listing or deleting. The new files use Bun's `.tx` suffix for transactional DDL; downgrade refuses to proceed while labeled grants exist. Explicitly reconcile those grants before downgrading. For a Bun rollback runner, enable `migrate.WithMarkAppliedOnSuccess(true)` to retain migration markers on refusal. The pinned go-persistence-bun v0.17.1 rollback APIs remove markers before running SQL: preflight `SELECT COUNT(*) FROM user_custom_roles WHERE scope_key <> ''` and stop writers before using those APIs. If a rollback fails, reconcile its migration marker before any retry.

Persistence isolation does not authorize an actor, establish resource ownership or advance permission versions. Hosts still compose those policies through the guard and existing hooks; role hooks run after persistence. `RoleRegistry.BindTx(tx)` binds all catalog/grant reads and writes to a caller transaction and disables hooks to avoid publication before commit. Native Group management persists audit in that transaction. Raw registry operations are trusted infrastructure; application-facing Group grant/revoke operations use `groups.Service.SetRole`.

## Registry API

`pkg/types.RoleRegistry` exposes:

- `CreateRole`, `UpdateRole`, `DeleteRole` – manage role definitions via `types.RoleMutation` and return `types.RoleDefinition`.
- `AssignRole`, `UnassignRole` – attach/detach users within an exact assignment scope, retaining assignment actor/time metadata.
- `ListRoles`, `GetRole` – paginate roles for admin UI filters via `types.RoleFilter`.
- `ListAssignments` – power dashboards that need to display which users inherit a role.

Each successful nonduplicate mutation triggers `hooks.AfterRoleChange` with a `types.RoleEvent` payload so hosts can fan-out cache busts or queue messages.

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

Definition events include `RoleEvent.Role`. All events include the `ScopeFilter`, `ActorID`, and optional `UserID` (for assignments). Downstream services can inspect these payloads to fan-out notifications or rebuild caches.

`registry.RoleRegistry.WithTx(bun.Tx)` returns a registry bound to a caller-owned
transaction. Catalog/assignment operations and typed commands using it share the
transaction; hooks are suppressed until the host can publish committed state.
The caller must bind policy reads, required audit and version advancement to the
same transaction and lock all relevant state before authorization. See
[transaction ordering](GUIDE_MULTITENANCY.md#transaction-ordering-audit-and-publication).
