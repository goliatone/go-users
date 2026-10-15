# Workspace Integration Guide

The optional `groups` subpackage persists flat Groups and independent memberships.
Other workspace/team ownership tables remain host-owned. `ScopeFilter.Labels` carries extra identifiers through APIs
that support them; it does not add database columns or authorize resources.
Do not substitute a Group for an organization to obtain implicit isolation.

## Capability boundaries

| API | Built-in behavior | Host responsibility / regression |
| --- | --- | --- |
| Scope helpers | Canonical keys and validated, detached label maps | Derive identifiers from trusted state; `pkg/types/scope_test.go`. |
| Strict guard / CRUD adapter | Requires resolver, policy and nonblank mapped actions | Authenticate actor; check current account, membership, Group state, explicit grants and target ownership; `scope/guard_test.go`, `service/ownership_test.go`, `service/live_authorization_test.go`. |
| Native Groups | Flat Group/membership tables, strict live membership plus exact-grant authorization, guarded management and transaction-ordered protected writes | Select `groups.NewStrict` and use its supplied transaction; creation grants no membership/permissions; resource ownership stays host-owned. `groups/service_test.go`. |
| Role catalog | Global/tenant/org definitions; rejects labels | Share definitions across labeled assignments; `registry/scope_test.go`. |
| Role assignments | Migration 00010 persists exact complete scope; independent reads/revocation | Upgrade every reader/writer before labeled grants; require candidate grant validation for escalation checks; `service/role_grant_test.go`. |
| Permission resolver | Optional `adapter/goauth.ScopedPermissionsResolver` separates canonical scope and authoritative versions in both caches | Resolve trusted current state before cache access; advance versions on every authorization change. Legacy JWT `Can*` helpers retain global fallback; `adapter/goauth/permissions_test.go`, `service/scoped_permissions_test.go`. |
| User lifecycle / direct IDs | Policy receives resolved scope and target | Enforce ownership in policy and SQL; lists, counts and exports need predicates before pagination; generic creation cannot attach host ownership automatically. |
| Activity feed / statistics / cursor | Migration 00011 persists complete labels and exact SQL predicates before pagination/count/cursor; unlabeled reads exclude labeled rows | Authorize trusted scope; metadata cannot impersonate ownership. `groups/service_test.go`, `activity/bun_repository_test.go`. |
| Profiles | Existing user profile with tenant/org columns; labeled get/upsert returns unsupported-scope error | Separate Group profiles require a host schema; `profile/bun_repository_test.go`. |
| Preferences | System → tenant → org → user precedence, versions and provenance; rejects labels on store operations and resolution | Group preference layers require a host store/resolver; best-effort bulk may partially succeed per record; `preferences/bun_repository_test.go`, resolver tests. |
| Secure links | Tenant/org payloads; labeled issuance, validation, consumption and reset confirmation deny before side effects | Group onboarding needs a host durable protected scope contract and current-state revalidation. `scope_labels` is reserved and rejected; arbitrary metadata cannot grant membership; `command/securelink_scope_test.go`. |
| Hooks / activity records | Role/lifecycle events carry Scope; `ActivityRecord` and `AfterActivity` carry tenant/org and canonical Labels | Transaction-bound role registries suppress hooks; required activity uses LogTx with disclosure policy. Host services own state locks, version/outbox persistence and delivery; `service/authorization_transaction_test.go`. |

## Compose protected application operations

Use `users.NewStrict` with an authoritative resolver and policy. Check current
account/Group state, membership and explicit permissions on each decision. Verify
that a direct target belongs to the resolved Group. Apply equivalent predicates
inside repositories for inventory, totals, exports and jobs. Deny unknown or
unavailable state. Rejoining does not recreate revoked grants.

For grant changes, select `RequireRoleGrantValidation` and a
`types.RoleGrantValidator` to examine the actor, target principal, operation,
current catalog role/permissions and complete proposed assignment scope. These
checks are sequential by default; transaction ordering requires the host-owned
boundary described in [the multitenancy guide](GUIDE_MULTITENANCY.md).

For an authorized Group history page, select the trusted canonical object:

```go
page, err := activityRepo.ListActivity(ctx, types.ActivityFilter{
    Scope:      types.ScopeFilter{}.WithLabel("group", authorizedGroupID),
    ObjectType: "group",
    ObjectID:   authorizedGroupID.String(),
})
```

This returns activity whose canonical object is that Group. It does not include
all child-resource activity or authorize the caller. Group metadata in an event
must never be used as an ownership predicate. Complete canonical Labels filter
SQL before counts/pagination after migration 00011; extra labels are exact
dimensions, not a subset match.

Links previously issued with Group identity only in metadata cannot recover a
protected Group scope. Revoke those links and use an explicit host onboarding
contract rather than accepting metadata as membership authorization.

## Schema ingestion

`pkg/schema.Registry` exports controller metadata such as `x-admin-scope` and
relation hints. It does not install authorization or workspace persistence.
`schema.Notifier` and `examples/web`'s schema demo illustrate refresh events.
