# Workspace Integration Guide

Many SaaS products model tenants with nested “workspace” or “project” scopes
(`org → workspace → user`). go-users does not ship a first-class workspace
schema, but its multitenant guard, scoping DTOs, and hooks are designed so you
can add that layer in your own application. This document outlines the
recommended patterns.

## 1. Extend the Scope Filter

`types.ScopeFilter` now exposes `Labels map[string]uuid.UUID` plus helper
methods (`WithLabel`, `Label`, `Clone`). This allows you to attach an unlimited
number of scope dimensions without touching go-users internals. A resolver +
policy pairing might look like:

```go
resolver := types.ScopeResolverFunc(func(_ context.Context, actor types.ActorRef, requested types.ScopeFilter) (types.ScopeFilter, error) {
    claims := claimsFromContext(actor)
    scope := requested
    if scope.TenantID == uuid.Nil {
        scope.TenantID = claims.TenantID
    }
    return scope.WithLabel("workspace", claims.WorkspaceID), nil
})

policy := types.AuthorizationPolicyFunc(func(_ context.Context, check types.PolicyCheck) error {
    allowedWorkspace := workspaceIDFromClaims(check.Actor)
    if allowedWorkspace != uuid.Nil && allowedWorkspace != check.Scope.Label("workspace") {
        return types.ErrUnauthorizedScope
    }
    return nil
})
```

Because the shared scope guard runs before every command/query, once the label
is populated all operations (invites, lifecycle, roles, preferences, queries,
etc.) automatically honor the workspace boundary.

## 2. Capture Workspace Metadata in Invites & Activity

`command.UserInvite` and other lifecycle commands already accept arbitrary
metadata. When creating workspace-specific invites:

```go
metadata := map[string]any{
    "workspace_id": workspaceID.String(),
}
svc.Commands().UserInvite.Execute(ctx, command.UserInviteInput{
    Email:    "owner@example.com",
    Scope:    types.ScopeFilter{TenantID: tenantID}.WithLabel("workspace", workspaceID),
    Metadata: metadata,
    Actor:    actor,
})
```

Activity records emitted by commands persist `TenantID`/`OrgID` automatically,
so your dashboards can filter events per workspace. Hooks such as
`AfterLifecycle` and `AfterActivity` receive the same scope, letting you create
workspace membership rows, send WebSocket updates, or trigger background jobs.

## 3. Store Workspace Membership Outside go-users

Workspace entities often require custom attributes (billing, integrations,
roles). Model them in your application database and compose go-users like so:

1. Create the workspace (and its admin role) via your own domain service.
2. Call go-users commands (`UserInvite`, `AssignRole`, etc.) scoped via
   `ScopeFilter.WithLabel("workspace", workspaceID)`.
3. Use go-users queries (`ActivityFeed`, `RoleAssignments`, `PreferenceQuery`)
   with the same label to power workspace dashboards.

## 4. Preference & Profile Helpers

The preferences resolver already layers `system → tenant → org → user`. You can
map `org` to `workspace` and take advantage of provenance traces in
`types.PreferenceSnapshot` to show which workspace layer supplied each key. The
profile commands/queries behave similarly: pass the workspace-scoped filter when
reading/writing profile rows.

## 5. Testing Checklist

When adding workspace support, verify:

- Scope resolver/policy default correctly when requests omit workspace IDs.
- Invites, lifecycle transitions, and role assignments fail when actors target
  unauthorized workspaces.
- Queries (inventory, roles, activity, preferences, profiles) only return data
  for the workspace bound to the actor.
- Activity hooks include workspace IDs so analytics/streaming consumers can
  partition data appropriately.

## 6. Teams & Additional Labels

Need a `team` layer beneath workspaces? Add another label:

```go
scope := types.ScopeFilter{TenantID: tenantID}.
    WithLabel("workspace", workspaceID).
    WithLabel("team", teamID)

err := svc.Commands().AssignRole.Execute(ctx, command.AssignRoleInput{
    UserID: userID,
    RoleID: roleID,
    Actor:  actor,
    Scope:  scope,
})
```

Policies can verify `check.Scope.Label("team")`, invites can store the label in
metadata, and queries filter on the same label. The label map keeps the core
API minimal while letting you support any hierarchy (workspaces, teams,
projects, environments, etc.) that your product demands.

## 7. Schema ingestion for workspace-aware dashboards

After controllers register with `pkg/schema.Registry`, the exported
`/admin/schemas` document includes `x-admin-scope` and relation hints that
workspace dashboards can consume. Pair the registry with `schema.Notifier` to
push refresh events into your CMS whenever actors authenticate. The
`examples/web` app includes a `/admin/schema-demo` page that polls the endpoint
and lists recent registry events—use it as a reference implementation when
wiring schema polling into go-admin/go-cms workspaces.
