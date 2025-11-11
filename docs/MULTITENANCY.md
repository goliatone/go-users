# Multi-Tenancy & Policy Guide

Phase 6 introduces a scope guard that sits in front of every command and query. The guard performs two duties:

1. **Scope resolution** – normalize/augment the caller-supplied `types.ScopeFilter` so tenant/org defaults are always filled in.
2. **Authorization** – evaluate whether the actor is allowed to perform the requested `types.PolicyAction` within that scope.

Consumers provide these behaviors through the new `users.Config` fields:

```go
svc := users.New(users.Config{
    // ...repositories, sinks, hooks...
    ScopeResolver: types.ScopeResolverFunc(func(_ context.Context, actor types.ActorRef, requested types.ScopeFilter) (types.ScopeFilter, error) {
        scope := requested
        claims := claimsFromActor(actor)
        if scope.TenantID == uuid.Nil {
            scope.TenantID = claims.TenantID
        }
        if claims.WorkspaceID != uuid.Nil {
            scope = scope.WithLabel("workspace", claims.WorkspaceID)
        }
        if claims.TeamID != uuid.Nil {
            scope = scope.WithLabel("team", claims.TeamID)
        }
        return scope, nil
    }),
    AuthorizationPolicy: types.AuthorizationPolicyFunc(func(_ context.Context, check types.PolicyCheck) error {
        allowedTenant := tenantIDFromClaims(check.Actor)
        if allowedTenant != uuid.Nil && check.Scope.TenantID != allowedTenant {
            return types.ErrUnauthorizedScope
        }
        if workspace := check.Scope.Label("workspace"); workspace != uuid.Nil {
            allowedWorkspace := workspaceIDFromClaims(check.Actor)
            if allowedWorkspace != uuid.Nil && allowedWorkspace != workspace {
                return types.ErrUnauthorizedScope
            }
        }
        return nil
    }),
})
```

Both helpers are optional—if omitted, the guard behaves like the previous releases (no normalization or authorization). For multi-tenant scenarios, wire them up in your transport layer (REST, gRPC, jobs, etc.) so each command/query share the same enforcement logic.

## Policy Actions

Each command/query passes a `types.PolicyAction` into the guard. Host applications can switch on this value inside their policy implementation to apply fine-grained checks. The current action list is:

| Action Constant                      | Description / Used By                                                      |
| ------------------------------------ | ------------------------------------------------------------------------- |
| `types.PolicyActionUsersRead`        | User inventory queries                                                     |
| `types.PolicyActionUsersWrite`       | Lifecycle transitions, invites, password resets                           |
| `types.PolicyActionRolesRead`        | Role list/detail/assignment queries                                       |
| `types.PolicyActionRolesWrite`       | Role create/update/delete/assign/unassign commands                        |
| `types.PolicyActionActivityRead`     | Activity feed + stats queries                                             |
| `types.PolicyActionActivityWrite`    | `LogActivity` command (if you add your own wrapper, pass this action)     |
| `types.PolicyActionProfilesRead`     | Profile detail query                                                       |
| `types.PolicyActionProfilesWrite`    | Profile upsert command                                                     |
| `types.PolicyActionPreferencesRead`  | Preference query (effective resolver)                                     |
| `types.PolicyActionPreferencesWrite` | Preference upsert/delete commands                                         |

Return `types.ErrUnauthorizedScope` (or any `error`) to abort the operation before repositories are invoked.

## Resolver Best Practices

- Treat explicitly supplied scope values as authoritative. Only backfill defaults when the caller omitted tenant/org IDs. This allows transports to intentionally target a different scope (e.g., system admins impersonating another tenant) while still letting your policy decide whether that’s okay.
- Keep resolver logic idempotent and side-effect free. It should be safe to call for every request on the hot path.
- Favor deterministic tenant lookups—pass actor → tenant mappings through context/session once per request instead of hitting the database inside the resolver.

## Supplying Actor Metadata to Queries

All read-side filters now include an `Actor types.ActorRef` field (e.g., `types.UserInventoryFilter.Actor`). Set this when you execute queries so the guard can enforce visibility rules identically for reads and writes:

```go
filter := types.UserInventoryFilter{
    Actor: actorFromRequest(r),
    Scope: types.ScopeFilter{TenantID: tenantIDFromRequest(r)}.
        WithLabel("workspace", workspaceIDFromRequest(r)),
    Pagination: types.Pagination{Limit: 50},
}
page, err := svc.Queries().UserInventory.Query(r.Context(), filter)
```

Commands already require an `Actor` field; nothing changes there except that the guard now consumes the value.

## Transport Middleware Pattern

A simple HTTP middleware can derive the actor + default scope once, stash them on the request context, and reuse them when invoking commands/queries:

```go
func withUserContext(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        actor := types.ActorRef{ID: userIDFromJWT(r), Type: \"admin\"}
        scope := types.ScopeFilter{TenantID: tenantIDFromJWT(r)}.
            WithLabel("workspace", workspaceIDFromJWT(r)).
            WithLabel("team", teamIDFromJWT(r))
        ctx := context.WithValue(r.Context(), actorKey{}, actor)
        ctx = context.WithValue(ctx, scopeKey{}, scope)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

func lifecycleHandler(w http.ResponseWriter, r *http.Request) {
    actor := r.Context().Value(actorKey{}).(types.ActorRef)
    scope := r.Context().Value(scopeKey{}).(types.ScopeFilter)
    err := svc.Commands().UserLifecycleTransition.Execute(r.Context(), command.UserLifecycleTransitionInput{
        UserID: userIDFromPath(r),
        Target: types.LifecycleStateSuspended,
        Actor:  actor,
        Scope:  scope,
    })
    // ...
}
```

By pairing middleware with the guard-enabled service configuration, you ensure scoping rules are centralized and transport-agnostic.
