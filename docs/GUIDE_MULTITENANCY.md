# Multi-Tenancy Guide

This guide covers implementing multi-tenant user management in `go-users`, including scope filters, scope resolvers, authorization policies, and the scope guard system.

## Overview

The multi-tenancy system provides:

- **Scope filters** with tenant, organization, and custom label support
- **Scope resolvers** that normalize and augment scope from actor context
- **Authorization policies** that enforce access control per action
- **Scope guard** that combines resolution and authorization for all operations
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

Labels extend the basic tenant/org hierarchy for custom dimensions.

### Workspace Example

```go
// Create workspace-scoped role
err := svc.Commands().CreateRole.Execute(ctx, command.CreateRoleInput{
    Name:        "Workspace Editor",
    Permissions: []string{"content.read", "content.write"},
    Scope:       types.ScopeFilter{TenantID: tenantID}.WithLabel("workspace", workspaceID),
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

// Query workspace activity
feed, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor: actor,
    Scope: types.ScopeFilter{TenantID: tenantID}.WithLabel("workspace", workspaceID),
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
