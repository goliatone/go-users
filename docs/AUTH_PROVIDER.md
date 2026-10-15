# Auth Provider Integration

go-users relies on an upstream authentication service (go-auth today) that
implements the `types.AuthRepository` interface. This document explains how to
wire the repository, what adapters exist, and how to test integrations.

## Requirements

The auth repository must expose:

- CRUD operations for user records (`GetByID`, `GetByIdentifier`, `Create`, `Update`)
- Lifecycle transitions with reason/metadata hooks (`UpdateStatus`)
- A way to query allowed transitions for the current lifecycle state

go-users ships `types.AuthRepository` and helper types (`AuthUser`,
`LifecycleTransition`, `ActorRef`, `TransitionOption`) that define this contract.

## go-auth Adapter

If you are already using `github.com/goliatone/go-auth`, reuse the adapter in
`adapter/goauth`:

```go
import (
    auth "github.com/goliatone/go-auth"
    "github.com/goliatone/go-users/adapter/goauth"
    "github.com/goliatone/go-users/service"
)

usersRepo := auth.NewUsersRepository(bunDB)
authProvider := goauth.NewUsersAdapter(usersRepo)

svc := users.New(users.Config{
    AuthRepository: authProvider,
    // other dependencies…
})
```

The adapter:

- Wraps `auth.Users`
- Converts go-auth `UserStatus` values into go-users `LifecycleState`
- Proxies lifecycle transitions through go-auth’s `UserStateMachine`
- Applies the default transition policy; override it via `goauth.WithPolicy(...)`

## Scoped Permission Resolution with go-auth

For permission sets that depend on custom scope labels, use the optional adapter in `adapter/goauth`. It resolves trusted host state before both request-local and cross-request caching:

```go
resolver, err := goauth.NewScopedPermissionsResolver(goauth.ScopedPermissionsResolverConfig{
    RequestResolver: loadAuthorizedPermissionRequest,
    Cache: auth.CachedPermissionsResolverConfig{
        TTL: time.Minute,
        Resolver: func(ctx context.Context) ([]string, error) {
            request, ok := goauth.ScopedPermissionsRequestFromContext(ctx)
            if !ok {
                return nil, types.ErrUnauthorizedScope
            }
            return loadScopedPermissions(ctx,
                request.Current.Principal.ApplicationSubject(), request.Scope)
        },
    },
})
if err != nil {
    return err
}
```

`loadAuthorizedPermissionRequest` is a host function with signature `func(context.Context) (goauth.ScopedPermissionsRequest, error)`. On each call it authenticates/maps the caller, validates current account/Group/membership access and returns the authorized `ScopeFilter` plus a go-auth `CurrentPermissionsRequest` containing the normalized principal and current authoritative version. The adapter cannot establish that trust from a token version, `group_id` metadata or request parameters alone. The backend consumes the bound snapshot rather than rereading mutable Group selection hints.

The adapter canonicalizes/detaches labels and checks tenant/org UUIDs against the normalized principal. Empty principal tenant/org IDs correspond to nil UUIDs. Invalid labels, mismatched scope, missing principal/version or host callback errors deny before any cache lookup. `Cache.KeyFunc` must remain unset; the managed key hashes the complete scope and go-auth's existing identity/version/session/impersonation discriminators. Existing stores, cache error policy, timeout and bounded indexed invalidation are retained. `ResolverFunc()` supplies the normal go-auth function signature; do not place a scope-blind cached resolver around it, since an outer cache could skip the host checks.

Advance the authoritative version for every permission-affecting mutation, including grants, memberships, Group state and administrative exceptions. The host owns atomic mutation/version advancement through go-auth's existing `AuthorizationVersionStore` contract. The adapter calls the request resolver before every lookup, so version changes partition even an already-warmed request-local cache. It also preserves an upstream `ForceRefresh` request; go-auth keeps separate request-local cached/refresh entries, and forced refresh bypasses cross-request lookups.

`InvalidateScope(ctx, auth.PermissionInvalidationScope{...}, limit)` retains go-auth's `(deleted, more, error)` contract; repeat while `more` is true. Principal/tenant/session invalidation applies across the custom scopes beneath that identity. It clears cross-request entries; existing request-local entries become obsolete through a new authoritative version. Invalidation alone is not a substitute for version advancement. Custom stores must satisfy go-auth's atomic indexed-publication contract for scoped cross-request caching.

A nonpositive TTL disables cross-request storage, while request-local caching still uses the canonical full-scope key. No cache layer supplies authentication, ownership predicates or permission semantics; any extra permission-affecting backend inputs must be represented by the scope or current authorization version. Concurrent check/mutation ordering remains a host transaction concern.

## Custom Providers

To integrate a different auth store:

1. Implement `types.AuthRepository`
2. Map your user struct to `types.AuthUser`
3. Enforce lifecycle policies (you can reuse `types.DefaultTransitionPolicy()`)

## Testing

- Use the migration smoke test (`task migrate`) to verify DB connectivity
- Mock `types.AuthRepository` when testing go-users commands
- For go-auth setups, rely on its existing repository tests and only unit-test
  the adapter if you customize policies
