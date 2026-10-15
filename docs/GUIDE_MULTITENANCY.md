# Multi-Tenancy Guide

This guide covers implementing multi-tenant user management in `go-users`, including scope filters, scope resolvers, authorization policies, and the scope guard system.

## Optional native Groups

The `groups` subpackage provides flat Groups with no tenant/org parent. Apply migrations 00010–00012 before enabling it. `groups.NewStrict(groups.Config{DB: db, Resolver: resolver, Policy: policy, CatalogScope: catalog, Actions: actions})` requires explicit resolver and policy dependencies. The host policy grants bootstrap creation authority and remains an additional restriction for every native decision; the built-in allow-all policy is rejected.

`Service.Create` creates no membership or permission. Provision initial manager membership and an exact grant through trusted deployment SQL/registry in one transaction; principals are opaque UUIDs and need no auth-user row. Routine mutations use `SetMember`, `SetRole`, `SetActive` or `NewManageCommand`. Assignments reuse existing role definitions from the configured catalog. A manager can delegate only permissions currently held in the same Group and allowed by the host policy. Removing membership destroys that Group's grants; rejoining creates none.

`Service.Guard()` returns a built-in strict guard for existing commands/queries. It reads Group state, membership and exact role grants on each decision. Unknown actions/Groups, archived Groups, scope rewriting and global-role fallback deny. Declare resource action names in `Config.Actions`; membership alone never permits them. `Discover` returns active Groups with membership and an effective `groups:read` grant.

For writes requiring revocation ordering, use `Service.WithAuthorized(ctx, actor, groups.Scope(groupID), action, targetID, callback)` and perform all protected SQL through the callback's `bun.Tx`. Managed grant/member/state changes acquire the same Group lock. A write already holding the lock finishes before revoke; a revoke that acquires it first prevents the write callback. The callback must not recursively invoke the service or perform external side effects. Direct SQL/registry writes remain trusted infrastructure and must follow this locking protocol. Hosts still enforce resource ownership and list predicates; this API does not add Group support to profile, preference or secure-link stores.

## Overview

The multi-tenancy system provides:

- **Scope filters** with tenant, organization, and custom label support
- **Scope resolvers** that normalize and augment scope from actor context
- **Authorization policies** that enforce access control per action
- **Scope guard** that combines resolution and authorization for guarded operations
- **Label-based hierarchies** for workspaces, teams, projects, etc.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Command / Query                          │
│            (with Actor + ScopeFilter + Action)              │
└──────────────────────────┬──────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────┐
│                      Scope Guard                            │
├─────────────────────────────────────────────────────────────┤
│  1. Scope Resolution    │  2. Authorization Check           │
│  ─────────────────────  │  ─────────────────────────────    │
│  ScopeResolver          │  AuthorizationPolicy              │
│  - Fill defaults        │  - Check actor permissions        │
│  - Add labels           │  - Validate scope access          │
│  - Normalize IDs        │  - Return error or allow          │
└──────────────────────────┴──────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────┐
│                      Repository                             │
│              (receives resolved scope)                      │
└─────────────────────────────────────────────────────────────┘
```

## Strict Authorization Configuration

Use `users.NewStrict` (also available as `service.NewStrict`) when scope authorization is mandatory:

```go
svc, err := users.NewStrict(users.Config{
    // ...repositories, sinks, hooks...
    ScopeResolver:       resolver,
    AuthorizationPolicy: policy,
})
if err != nil {
    return err
}
if err := svc.HealthCheck(ctx); err != nil {
    return err
}
```

Strict startup requires both dependencies and rejects nil interfaces, typed nil pointers and nil function adapters. `NewStrict` validates authorization wiring; `Ready`/`HealthCheck` retain the existing repository readiness checks. With `users.New`, set `Config.RequireScopeAuthorization = true` for the same guard semantics: invalid wiring makes `Ready` false, `HealthCheck` returns the dependency error, and guarded commands/queries deny even when readiness is ignored.

For standalone command/query composition, use `scope.NewStrictGuard(resolver, policy)` and check its error. On failure it returns a guard that denies enforcement; zero/nil `*scope.StrictGuard` values also deny. `scope.ValidateStrict` checks this built-in contract and rejects `NopGuard`, legacy guards and arbitrary custom wrappers. It does not prove that the supplied policy's access rules are adequate; explicitly selecting `AllowAllAuthorizationPolicy` still allows every nonblank action.

Strict enforcement rejects empty or whitespace-only actions with `scope.ErrPolicyActionRequired` before invoking dependencies. Nonblank actions resolve scope, then evaluate policy; either error stops the guarded operation. Dependency errors support `errors.Is` with `scope.ErrScopeResolverRequired` and `scope.ErrAuthorizationPolicyRequired`.

Legacy `users.New` with the flag unset, `scope.NewGuard`, `scope.Ensure(nil)` and `scope.NopGuard` keep their permissive defaults, including skipping policy for an empty action. A nonnil guard alone is not evidence of strict authorization. Strict mode applies to paths that invoke the guard; direct repository access, activity logging and token validation have their own contracts. Authentication, resource ownership, current membership, permission freshness and transaction ordering remain host responsibilities.

## Host-Owned Resource and Membership Checks

Strict guard wiring requires a resolver/policy and a nonblank action. It cannot discover application-owned Groups, memberships or target ownership. `AuthRepository` has ID-only operations, and `AuthUser` has no canonical Group ownership field. A policy that merely authorizes scope A can still permit an A-scoped command against a B-owned ID.

| Protected path | Host responsibility |
| --- | --- |
| Direct reads, updates and archive/delete | Interpret `PolicyCheck.TargetID` for its action and verify authoritative ownership before access. Enforce ID plus Group predicates at the repository boundary as well. Return a denial for unknown/foreign targets without disclosing them. |
| Creation | Validate supplied data and attach trusted ownership in the host application service and transaction. `TargetID == uuid.Nil` is not ownership proof; deny generic creation for Group-protected flows until that boundary exists. Client metadata is not trusted ownership. |
| Lists, counts and exports | Apply the authorized Group predicate in the repository before counts, pagination and export iteration. Filtering returned rows cannot repair leaked totals or skipped pages. Built-in repositories do not universally implement arbitrary labels. |
| Bulk commands and jobs | Reuse the protected application command for each target. Bulk lifecycle uses the single-target guard and retains partial-success semantics; all-or-nothing behavior requires a separate transactional contract. Background callers receive no policy exemption. |
| Current state | Read authoritative account status, Group status, membership and explicit grants on each protected decision. Deny inactive/unknown state and dependency failures. Membership alone is not permission; rejoining must not recreate revoked grants. |
| Grant/revoke | Use `RoleGrantValidator` for target-principal eligibility and delegation rules; generic `PolicyCheck.TargetID` identifies the role rather than the target principal. |

Compose the live-state and ownership checks in the host `AuthorizationPolicy` used by `users.NewStrict`. Use existing account-state/freshness mechanisms and host Group/membership stores, and keep permission evaluation separate from membership. A stale session/claim snapshot cannot establish current membership or an active grant. The grant validator should independently check the target principal's eligibility in the resolved assignment scope.

The public integration fixtures in `service/ownership_test.go` and `service/live_authorization_test.go` demonstrate unsafe partial composition and the safe policy/repository boundary, including transport-free commands, mixed batches, paginated totals and subsequent denial after state changes. These are in-memory host fixtures; they do not establish production SQL ownership predicates, PostgreSQL revocation ordering or a built-in Groups schema. Authorization-to-mutation ordering still requires a host transaction/locking contract.

## Scoped Permissions and Legacy Claim Fallback

For Group-protected operations, evaluate explicit permission strings from current scoped grants inside the configured `AuthorizationPolicy`. The [scoped go-auth adapter](AUTH_PROVIDER.md#scoped-permission-resolution-with-go-auth) can cache those strings using complete authorized scope and authoritative version. The host maps each `PolicyAction` to its required permission and returns `types.ErrUnauthorizedScope` when absent; resolver failures also deny. Keep live membership/state and target-ownership checks in that path before permission-cache access.

`JWTClaims.CanRead/CanEdit/CanCreate/CanDelete` retain their existing global-role fallback when a resource mapping is absent. They do not evaluate go-users custom permission strings. Do not use them as fallback after a scoped denial, and do not implicitly merge a global admin claim into Group permissions. `NewStrict` validates wiring/actions; it does not select a scoped permission evaluator automatically.

If cross-Group administrators are required, select the exception through authoritative host policy, independently of the claim role. Test its account/Group eligibility, target boundary, approval and revocation, and advance the same authorization version when the exception changes. The SQLite-backed public service regressions in `service/scoped_permissions_test.go` cover A editor/B reader isolation, an unassigned B denying despite a global admin claim, grant revocation within a warmed request cache, and a separately approved/revoked administrative exception. The legacy claim helper remains unchanged in those tests.

## Scope Filter Structure

The `ScopeFilter` carries tenant/org identifiers and arbitrary labels:

```go
type ScopeFilter struct {
    TenantID uuid.UUID            // Primary tenant scope
    OrgID    uuid.UUID            // Organization within tenant
    Labels   map[string]uuid.UUID // Custom dimensions (workspace, team, project)
}
```

### Creating Scope Filters

```go
// Simple tenant scope
scope := types.ScopeFilter{
    TenantID: tenantID,
}

// Tenant + organization
scope := types.ScopeFilter{
    TenantID: tenantID,
    OrgID:    orgID,
}

// With custom labels
scope := types.ScopeFilter{TenantID: tenantID}.
    WithLabel("workspace", workspaceID).
    WithLabel("team", teamID)
```

### Scope Filter Methods

```go
// Clone creates a copy with detached label map
clone := scope.Clone()

// WithLabel returns a new scope with the label set
scope = scope.WithLabel("project", projectID)

// Label retrieves a stored label (case-insensitive)
workspaceID := scope.Label("workspace")
```

## Actor Reference

Every operation requires an `ActorRef` identifying who is performing the action:

```go
type ActorRef struct {
    ID   uuid.UUID  // Actor's unique identifier
    Type string     // Actor role type
}
```

### Actor Role Types

| Type | Description | Typical Use |
|------|-------------|-------------|
| `system_admin` | Global administrator | Full platform access |
| `tenant_admin` | Tenant administrator | Scoped to their tenant |
| `org_admin` | Organization administrator | Scoped to their org |
| `support` | Support agent | Limited to specific users/scopes |

```go
// System administrator
actor := types.ActorRef{
    ID:   adminID,
    Type: types.ActorRoleSystemAdmin,
}

// Tenant administrator
actor := types.ActorRef{
    ID:   userID,
    Type: types.ActorRoleTenantAdmin,
}

// Check actor role
if actor.IsSystemAdmin() {
    // Allow all operations
}
if actor.IsTenantAdmin() {
    // Restrict to tenant scope
}
if actor.IsSupport() {
    // Limited access
}
```

## Policy Actions

Each command/query specifies a `PolicyAction` that the guard enforces:

| Action | Description | Used By |
|--------|-------------|---------|
| `users:read` | View user data | User inventory queries |
| `users:write` | Modify users | Lifecycle, invites, password reset |
| `roles:read` | View roles | Role list, detail, assignments |
| `roles:write` | Modify roles | Create, update, delete, assign |
| `activity:read` | View activity | Activity feed, stats |
| `activity:write` | Log activity | LogActivity command |
| `profiles:read` | View profiles | Profile detail query |
| `profiles:write` | Modify profiles | Profile upsert |
| `preferences:read` | View preferences | Preference query |
| `preferences:write` | Modify preferences | Preference upsert, delete |

```go
// Constants for policy actions
types.PolicyActionUsersRead        // "users:read"
types.PolicyActionUsersWrite       // "users:write"
types.PolicyActionRolesRead        // "roles:read"
types.PolicyActionRolesWrite       // "roles:write"
types.PolicyActionActivityRead     // "activity:read"
types.PolicyActionActivityWrite    // "activity:write"
types.PolicyActionProfilesRead     // "profiles:read"
types.PolicyActionProfilesWrite    // "profiles:write"
types.PolicyActionPreferencesRead  // "preferences:read"
types.PolicyActionPreferencesWrite // "preferences:write"
```

## Scope Resolver

The `ScopeResolver` normalizes and augments the requested scope based on actor context.

### Interface

```go
type ScopeResolver interface {
    ResolveScope(ctx context.Context, actor ActorRef, requested ScopeFilter) (ScopeFilter, error)
}
```

### Using ScopeResolverFunc

```go
resolver := types.ScopeResolverFunc(func(ctx context.Context, actor types.ActorRef, requested types.ScopeFilter) (types.ScopeFilter, error) {
    scope := requested

    // Get claims from your auth system
    claims := getClaimsFromContext(ctx, actor)

    // Fill in tenant if not provided
    if scope.TenantID == uuid.Nil {
        scope.TenantID = claims.TenantID
    }

    // Add workspace label from claims
    if claims.WorkspaceID != uuid.Nil {
        scope = scope.WithLabel("workspace", claims.WorkspaceID)
    }

    // Add team label if present
    if claims.TeamID != uuid.Nil {
        scope = scope.WithLabel("team", claims.TeamID)
    }

    return scope, nil
})
```

### Resolver Best Practices

1. **Treat explicit values as authoritative** - Only fill defaults when caller omits IDs
2. **Keep it idempotent** - Safe to call multiple times with same input
3. **Avoid database calls** - Use cached claims from context/session
4. **Handle nil labels** - Always use `WithLabel` to add labels safely

```go
resolver := types.ScopeResolverFunc(func(ctx context.Context, actor types.ActorRef, requested types.ScopeFilter) (types.ScopeFilter, error) {
    scope := requested.Clone()  // Always clone to avoid mutation

    claims := claimsFromContext(ctx)

    // Only fill tenant if not explicitly provided
    if scope.TenantID == uuid.Nil {
        if claims.TenantID == uuid.Nil {
            return types.ScopeFilter{}, errors.New("tenant required")
        }
        scope.TenantID = claims.TenantID
    }

    // Only fill org if not explicitly provided
    if scope.OrgID == uuid.Nil && claims.OrgID != uuid.Nil {
        scope.OrgID = claims.OrgID
    }

    return scope, nil
})
```

## Authorization Policy

The `AuthorizationPolicy` validates whether an actor can perform an action in a scope.

### Interface

```go
type AuthorizationPolicy interface {
    Authorize(ctx context.Context, check PolicyCheck) error
}

type PolicyCheck struct {
    Actor    ActorRef     // Who is performing the action
    Scope    ScopeFilter  // The resolved scope
    Action   PolicyAction // What action is being performed
    TargetID uuid.UUID    // Optional target resource ID
}
```

### Using AuthorizationPolicyFunc

```go
policy := types.AuthorizationPolicyFunc(func(ctx context.Context, check types.PolicyCheck) error {
    // System admins can do anything
    if check.Actor.IsSystemAdmin() {
        return nil
    }

    // Get actor's allowed tenant
    claims := claimsFromContext(ctx)

    // Verify tenant access
    if claims.TenantID != uuid.Nil && check.Scope.TenantID != claims.TenantID {
        return types.ErrUnauthorizedScope
    }

    // Verify workspace access
    if workspace := check.Scope.Label("workspace"); workspace != uuid.Nil {
        if claims.WorkspaceID != uuid.Nil && claims.WorkspaceID != workspace {
            return types.ErrUnauthorizedScope
        }
    }

    return nil
})
```

### Action-Based Authorization

```go
policy := types.AuthorizationPolicyFunc(func(ctx context.Context, check types.PolicyCheck) error {
    claims := claimsFromContext(ctx)

    // System admins bypass checks
    if check.Actor.IsSystemAdmin() {
        return nil
    }

    // Check action-specific permissions
    switch check.Action {
    case types.PolicyActionUsersWrite:
        if !claims.HasPermission("users.write") {
            return types.ErrUnauthorizedScope
        }
    case types.PolicyActionRolesWrite:
        if !claims.HasPermission("roles.write") {
            return types.ErrUnauthorizedScope
        }
    case types.PolicyActionUsersRead, types.PolicyActionRolesRead:
        // Read actions allowed with basic access
    default:
        // Require explicit permission for other actions
        if !claims.HasPermission(string(check.Action)) {
            return types.ErrUnauthorizedScope
        }
    }

    // Always verify scope access
    if check.Scope.TenantID != claims.TenantID {
        return types.ErrUnauthorizedScope
    }

    return nil
})
```

## Default Implementations

### PassthroughScopeResolver

Returns the requested scope unchanged (no normalization):

```go
resolver := types.PassthroughScopeResolver{}
// scope in == scope out
```

### AllowAllAuthorizationPolicy

Allows all actions without checks:

```go
policy := types.AllowAllAuthorizationPolicy{}
// Always returns nil (allowed)
```

These are used when you don't configure custom resolver/policy.

## Configuring the Service

Wire resolver and policy into the service configuration:

```go
svc := users.New(users.Config{
    AuthRepository:   authRepo,
    RoleRegistry:     roleRegistry,
    ActivitySink:     activityStore,
    // ... other dependencies ...

    ScopeResolver: types.ScopeResolverFunc(func(ctx context.Context, actor types.ActorRef, requested types.ScopeFilter) (types.ScopeFilter, error) {
        claims := claimsFromContext(ctx)
        scope := requested
        if scope.TenantID == uuid.Nil {
            scope.TenantID = claims.TenantID
        }
        return scope.WithLabel("workspace", claims.WorkspaceID), nil
    }),

    AuthorizationPolicy: types.AuthorizationPolicyFunc(func(ctx context.Context, check types.PolicyCheck) error {
        if check.Actor.IsSystemAdmin() {
            return nil
        }
        claims := claimsFromContext(ctx)
        if check.Scope.TenantID != claims.TenantID {
            return types.ErrUnauthorizedScope
        }
        return nil
    }),
})
```

## Scope Guard Flow

The scope guard runs before every command/query:

```go
// 1. Command provides actor, requested scope, and action
input := command.UserLifecycleTransitionInput{
    UserID: userID,
    Target: types.LifecycleStateActive,
    Actor:  actor,
    Scope:  requestedScope,
}

// 2. Guard resolves scope
resolvedScope, err := resolver.ResolveScope(ctx, actor, requestedScope)

// 3. Guard authorizes action
err := policy.Authorize(ctx, types.PolicyCheck{
    Actor:    actor,
    Scope:    resolvedScope,
    Action:   types.PolicyActionUsersWrite,
    TargetID: userID,
})

// 4. If authorized, command executes with resolved scope
// 5. Repository receives resolved scope for data filtering
```

## Transport Middleware Pattern

Extract actor and scope once per request, then reuse across operations:

```go
type contextKey string

const (
    actorKey contextKey = "actor"
    scopeKey contextKey = "scope"
)

func withUserContext(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Extract from JWT/session
        claims := getClaimsFromJWT(r)

        actor := types.ActorRef{
            ID:   claims.UserID,
            Type: claims.Role,
        }

        scope := types.ScopeFilter{
            TenantID: claims.TenantID,
            OrgID:    claims.OrgID,
        }.WithLabel("workspace", claims.WorkspaceID)

        // Store in context
        ctx := context.WithValue(r.Context(), actorKey, actor)
        ctx = context.WithValue(ctx, scopeKey, scope)

        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

func getActor(ctx context.Context) types.ActorRef {
    if actor, ok := ctx.Value(actorKey).(types.ActorRef); ok {
        return actor
    }
    return types.ActorRef{}
}

func getScope(ctx context.Context) types.ScopeFilter {
    if scope, ok := ctx.Value(scopeKey).(types.ScopeFilter); ok {
        return scope
    }
    return types.ScopeFilter{}
}
```

### Using in Handlers

```go
func lifecycleHandler(w http.ResponseWriter, r *http.Request) {
    actor := getActor(r.Context())
    scope := getScope(r.Context())

    err := svc.Commands().UserLifecycleTransition.Execute(r.Context(), command.UserLifecycleTransitionInput{
        UserID: userIDFromPath(r),
        Target: types.LifecycleStateSuspended,
        Actor:  actor,
        Scope:  scope,
    })
    if err != nil {
        if errors.Is(err, types.ErrUnauthorizedScope) {
            http.Error(w, "Forbidden", http.StatusForbidden)
            return
        }
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    w.WriteHeader(http.StatusOK)
}

func listUsersHandler(w http.ResponseWriter, r *http.Request) {
    actor := getActor(r.Context())
    scope := getScope(r.Context())

    page, err := svc.Queries().UserInventory.Query(r.Context(), types.UserInventoryFilter{
        Actor:      actor,
        Scope:      scope,
        Pagination: types.Pagination{Limit: 50},
    })
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    json.NewEncoder(w).Encode(page)
}
```

## Working with Labels

Labels carry custom dimensions only where the concrete API supports them. See [Workspace capability boundaries](WORKSPACES.md#capability-boundaries). Activity persists and filters exact labels after migration 00011; profiles, preferences and secure links reject nonempty labels. Role catalogs use tenant/org; assignments persist exact complete scopes after migration 00010.

### Workspace Example

```go
// Create a shared tenant catalog definition
err := svc.Commands().CreateRole.Execute(ctx, command.CreateRoleInput{
    Name:        "Workspace Editor",
    Permissions: []string{"content.read", "content.write"},
    Scope:       types.ScopeFilter{TenantID: tenantID},
    Actor:       actor,
    Result:      role,
})

// Assign role within workspace
err = svc.Commands().AssignRole.Execute(ctx, command.AssignRoleInput{
    UserID: userID,
    RoleID: roleID,
    Scope:  types.ScopeFilter{TenantID: tenantID}.WithLabel("workspace", workspaceID),
    Actor:  actor,
})

// After the host authorizes this canonical Group object, query its feed.
feed, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor:      actor,
    Scope:      types.ScopeFilter{TenantID: tenantID},
    ObjectType: "group",
    ObjectID:   authorizedGroupID.String(),
})
```

### Multi-Level Hierarchy

```go
// Tenant → Workspace → Team
scope := types.ScopeFilter{TenantID: tenantID}.
    WithLabel("workspace", workspaceID).
    WithLabel("team", teamID)

// Policy can check each level
policy := types.AuthorizationPolicyFunc(func(ctx context.Context, check types.PolicyCheck) error {
    claims := claimsFromContext(ctx)

    // Check tenant
    if check.Scope.TenantID != claims.TenantID {
        return types.ErrUnauthorizedScope
    }

    // Check workspace if present
    if ws := check.Scope.Label("workspace"); ws != uuid.Nil {
        if !claims.HasWorkspaceAccess(ws) {
            return types.ErrUnauthorizedScope
        }
    }

    // Check team if present
    if team := check.Scope.Label("team"); team != uuid.Nil {
        if !claims.HasTeamAccess(team) {
            return types.ErrUnauthorizedScope
        }
    }

    return nil
})
```

## Common Patterns

### Single-Tenant Application

For applications without multi-tenancy, use default implementations:

```go
svc := users.New(users.Config{
    AuthRepository: authRepo,
    // ScopeResolver and AuthorizationPolicy omitted
    // Guard will pass through scopes unchanged
})
```

### Tenant Isolation

Strict tenant isolation where users can only access their own tenant:

```go
policy := types.AuthorizationPolicyFunc(func(ctx context.Context, check types.PolicyCheck) error {
    claims := claimsFromContext(ctx)

    // Must match tenant exactly
    if check.Scope.TenantID == uuid.Nil {
        return errors.New("tenant required")
    }
    if check.Scope.TenantID != claims.TenantID {
        return types.ErrUnauthorizedScope
    }

    return nil
})
```

### Super Admin Access

Allow system admins to access any tenant:

```go
policy := types.AuthorizationPolicyFunc(func(ctx context.Context, check types.PolicyCheck) error {
    // System admins can access any scope
    if check.Actor.IsSystemAdmin() {
        return nil
    }

    // Regular users restricted to their tenant
    claims := claimsFromContext(ctx)
    if check.Scope.TenantID != claims.TenantID {
        return types.ErrUnauthorizedScope
    }

    return nil
})
```

### Support Agent Restrictions

Limit support agents to read-only operations on specific users:

```go
policy := types.AuthorizationPolicyFunc(func(ctx context.Context, check types.PolicyCheck) error {
    if check.Actor.IsSupport() {
        // Support can only read
        switch check.Action {
        case types.PolicyActionUsersRead, types.PolicyActionActivityRead:
            return nil
        default:
            return types.ErrUnauthorizedScope
        }
    }

    // Other actors follow normal rules
    return validateTenantAccess(ctx, check)
})
```

### Impersonation

Allow admins to operate on behalf of other tenants:

```go
resolver := types.ScopeResolverFunc(func(ctx context.Context, actor types.ActorRef, requested types.ScopeFilter) (types.ScopeFilter, error) {
    claims := claimsFromContext(ctx)

    // Check for impersonation header
    impersonateTenant := getImpersonationTenant(ctx)
    if impersonateTenant != uuid.Nil && actor.IsSystemAdmin() {
        // Admin is impersonating - use requested tenant
        return requested, nil
    }

    // Normal resolution
    scope := requested
    if scope.TenantID == uuid.Nil {
        scope.TenantID = claims.TenantID
    }
    return scope, nil
})
```

## Error Handling

```go
err := svc.Commands().UserLifecycleTransition.Execute(ctx, input)

if errors.Is(err, types.ErrUnauthorizedScope) {
    // Actor not authorized for the requested scope
    // Return 403 Forbidden
}

// Check for resolver errors
if err != nil && strings.Contains(err.Error(), "tenant required") {
    // Scope resolution failed
    // Return 400 Bad Request
}
```

## Testing with Mock Guard

```go
import "github.com/goliatone/go-users/scope"

func TestWithMockGuard(t *testing.T) {
    // Use NopGuard for testing without scope enforcement
    guard := scope.NopGuard()

    // Or create a custom test guard
    testResolver := types.ScopeResolverFunc(func(ctx context.Context, actor types.ActorRef, requested types.ScopeFilter) (types.ScopeFilter, error) {
        return requested, nil
    })

    testPolicy := types.AllowAllAuthorizationPolicy{}

    guard := scope.NewGuard(testResolver, testPolicy)
}
```

## Next Steps

- **[GUIDE_ROLES](GUIDE_ROLES.md)**: Scope roles by tenant/organization
- **[GUIDE_ACTIVITY](GUIDE_ACTIVITY.md)**: Tenant-scoped activity queries
- **[GUIDE_PROFILES_PREFERENCES](GUIDE_PROFILES_PREFERENCES.md)**: Scoped preferences
- **[GUIDE_CRUD_INTEGRATION](GUIDE_CRUD_INTEGRATION.md)**: HTTP middleware patterns

## Transaction ordering, audit and publication

Ordinary commands authorize before separate repository calls. A strict guard
alone does not serialize a later write with revocation. The supported host
composition uses the existing `auth.AuthorizationVersionStore` transaction
boundary and `auth.AuthorizationVersionWriter`:

1. Lock authoritative actor, target and Group state in a consistent order before
   policy reads. Every revocation, disablement and permission/catalog writer must
   follow the same protocol. Locking an unrelated version row is insufficient.
2. Bind `roleRegistry.WithTx(tx)` and policy/validator reads to that same Bun
   transaction. Execute the typed role command against the bound registry.
   Transaction-bound registries suppress `AfterRoleChange`; the original
   registry retains its existing notifications. Do not reuse bound registries
   after the transaction or concurrently.
3. Persist required audit with `activityRepo.LogTx(ctx, tx, record, policy)` and
   propagate errors. This applies `RecordPolicy` after enrichment; nil policy
   drops metadata/IP. Advance all affected authorization versions and enqueue
   required publication in a durable host outbox in the same transaction.
4. Commit, then publish. `AuthorizationVersionWriter` reports `Committed` and
   `Published` separately. A publication error after commit must not be described
   as rollback. Retry delivery from the outbox; consumers deduplicate using the
   operation ID. Bind that ID to an immutable request and deduplicate mutations
   inside the transaction. Do not retry arbitrary commands blindly.

The ordering rule permits a write that takes the shared lock first to finish;
revocation waits and then removes access. A write following committed revocation
must read the revoked state and deny. This does not cancel in-flight writes.

`service/authorization_transaction_test.go` implements a host SQL fixture using
these public APIs and the pinned go-auth version contract. Barrier tests on
SQLite and PostgreSQL reproduce the unsafe interleaving, verify both lock
orderings, audit/version rollback, idempotent mutation retries and outbox
recovery. `registry/scope_migration_test.go` checks transaction-bound catalog and
grant operations, duplicate grants and hook suppression. These establish a
composition contract; go-users does not install a host Group/version/outbox
schema. A distributed authorization store needs an equivalent atomic protocol;
a remote callback or cache invalidation hook cannot join a Bun transaction.

Legacy lifecycle, user, link and token commands retain best-effort Activity
sinks. Persistence failures call the configured Logger's Error method with event
verb/object type and the sink error; default NopLogger remains silent. Their
post-write hooks are notifications. Configuring a failing sink as “required”
does not add rollback. Required lifecycle audit needs a host service whose auth
repository mutation, audit and version writes all use the same transaction.
