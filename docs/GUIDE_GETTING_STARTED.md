# Getting Started with go-users

This guide walks you through setting up `go-users` and performing basic user management operations. By the end, you'll have a working service that can invite users, manage their lifecycle states, create roles, and log activity.

## Overview

`go-users` is a transport-agnostic user management package that provides:

- **User lifecycle management** with state machine transitions (pending, active, suspended, disabled, archived)
- **Custom role registry** with tenant/org scoping and permission assignments
- **Activity logging** for audit trails and compliance
- **Profile and preference management** with hierarchical scoping
- **Multi-tenancy support** with scope guards and authorization policies

The package follows a clean architecture with commands (write operations) and queries (read operations) exposed through a unified service facade.

## Prerequisites

- Go 1.24 or later
- Basic understanding of Go modules

## Installation

Add `go-users` to your project:

```bash
go get github.com/goliatone/go-users
```

## Minimal Setup

The simplest way to get started is using the in-memory repositories provided in the examples package. This is useful for development, testing, and understanding the API before integrating with a real database. Note that the in-memory repositories live under `examples/internal/memory`, so they are only importable within this module. For your own project, either copy those implementations or wire the Bun-backed repositories shown in Production Setup.

```go
package main

import (
    "context"
    "fmt"
    "log"

    users "github.com/goliatone/go-users"
    "github.com/goliatone/go-users/examples/internal/memory"
)

func main() {
    ctx := context.Background()

    // Create in-memory repositories
    authRepo := memory.NewAuthRepository()
    activityStore := memory.NewActivityStore()
    roleRegistry := memory.NewRoleRegistry()
    profileRepo := memory.NewProfileRepository()
    preferenceRepo := memory.NewPreferenceRepository()

    // Wire the service
    svc := users.New(users.Config{
        AuthRepository:       authRepo,
        InventoryRepository:  authRepo, // AuthRepository also implements UserInventoryRepository
        RoleRegistry:         roleRegistry,
        ActivitySink:         activityStore,
        ActivityRepository:   activityStore,
        ProfileRepository:    profileRepo,
        PreferenceRepository: preferenceRepo,
    })

    // Verify the service is ready
    if err := svc.HealthCheck(ctx); err != nil {
        log.Fatal("Service not ready:", err)
    }

    fmt.Println("go-users service is ready!")
}
```

## Core Concepts

### Service Structure

The `Service` exposes two main facades:

- **Commands**: Write operations that modify state
- **Queries**: Read operations that retrieve data

```go
svc.Commands()  // Returns command handlers
svc.Queries()   // Returns query handlers
```

### Actor Reference

Most operations require an `ActorRef` to identify who is performing the action:

```go
actor := types.ActorRef{
    ID:   uuid.New(),        // The actor's unique identifier
    Type: "system_admin",    // Actor role type
}
```

Actor types include: `system_admin`, `tenant_admin`, `org_admin`, `support`, or custom types.

### Scope Filter

Operations can be scoped to specific tenants or organizations:

```go
scope := types.ScopeFilter{
    TenantID: tenantUUID,
    OrgID:    orgUUID,
    Labels:   map[string]uuid.UUID{"workspace": workspaceUUID},
}
```

## Inviting a User

The first step in user management is typically inviting a user. This creates a user in `pending` state with an invite token:

```go
actor := types.ActorRef{ID: uuid.New(), Type: "system_admin"}

// Create a result container
inviteResult := &command.UserInviteResult{}

// Execute the invite command
err := svc.Commands().UserInvite.Execute(ctx, command.UserInviteInput{
    Email:  "newuser@example.com",
    Actor:  actor,
    Result: inviteResult,
})
if err != nil {
    log.Fatal("Failed to invite user:", err)
}

fmt.Printf("Invited user: %s\n", inviteResult.User.Email)
fmt.Printf("Invite token: %s\n", inviteResult.Token)
fmt.Printf("User ID: %s\n", inviteResult.User.ID)
```

The `UserInviteResult` contains:
- `User`: The created user record
- `Token`: An invite token for completing registration

## Feature Gates

`go-users` can optionally gate invite, self-registration, and password reset flows through `go-featuregate`. Provide a `featuregate.FeatureGate` via `users.Config.FeatureGate`. If you omit it, these flows are treated as enabled.

```go
gate := resolver.New(/* go-featuregate options */)

svc := users.New(users.Config{
    AuthRepository: authRepo,
    RoleRegistry:   roleRegistry,
    ActivitySink:   activityStore,
    FeatureGate:    gate,
})
```

Gated flows:

- `users.invite` (`command.ErrInviteDisabled`)
- `users.password_reset` (`command.ErrPasswordResetDisabled`)
- `featuregate.FeatureUsersSignup` / `users.signup` (`command.ErrSignupDisabled`)

## Transitioning User State

Users progress through lifecycle states based on the default transition policy: `pending` → `active`/`disabled`, `active` ↔ `suspended`, `active`/`suspended` → `disabled`, `disabled` → `archived`.

To activate a pending user:

```go
err := svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
    UserID: inviteResult.User.ID,
    Target: types.LifecycleStateActive,
    Actor:  actor,
    Reason: "User completed registration",
})
if err != nil {
    log.Fatal("Failed to activate user:", err)
}

fmt.Println("User activated successfully")
```

To suspend an active user:

```go
err := svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
    UserID: userID,
    Target: types.LifecycleStateSuspended,
    Actor:  actor,
    Reason: "Account review required",
})
```

## Querying Users

The inventory query retrieves users with filtering and pagination:

```go
page, err := svc.Queries().UserInventory.Query(ctx, types.UserInventoryFilter{
    Actor:      actor,
    Statuses:   []types.LifecycleState{types.LifecycleStateActive},
    Keyword:    "example.com",
    Pagination: types.Pagination{Limit: 10, Offset: 0},
})
if err != nil {
    log.Fatal("Query failed:", err)
}

fmt.Printf("Found %d users (total: %d)\n", len(page.Users), page.Total)
for _, user := range page.Users {
    fmt.Printf("  - %s (%s)\n", user.Email, user.Status)
}
```

## Creating and Assigning Roles

Custom roles allow fine-grained permission management:

```go
// Create a role
role := &types.RoleDefinition{}
err := svc.Commands().CreateRole.Execute(ctx, command.CreateRoleInput{
    Name:        "Editor",
    Description: "Can edit content",
    Permissions: []string{"content.read", "content.write"},
    Actor:       actor,
    Result:      role,
})
if err != nil {
    log.Fatal("Failed to create role:", err)
}

fmt.Printf("Created role: %s (ID: %s)\n", role.Name, role.ID)

// Assign role to a user
userID := inviteResult.User.ID
err = svc.Commands().AssignRole.Execute(ctx, command.AssignRoleInput{
    UserID: userID,
    RoleID: role.ID,
    Actor:  actor,
})
if err != nil {
    log.Fatal("Failed to assign role:", err)
}

fmt.Println("Role assigned successfully")
```

Query role assignments:

```go
assignments, err := svc.Queries().RoleAssignments.Query(ctx, types.RoleAssignmentFilter{
    Actor:  actor,
    UserID: userID,
})
if err != nil {
    log.Fatal("Query failed:", err)
}

fmt.Printf("User has %d role assignments\n", len(assignments))
```

## Viewing Activity

Activity records provide an audit trail of all operations:

```go
feed, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor:      actor,
    Pagination: types.Pagination{Limit: 10},
})
if err != nil {
    log.Fatal("Query failed:", err)
}

fmt.Printf("Recent activity (%d entries):\n", len(feed.Records))
for _, record := range feed.Records {
    fmt.Printf("  [%s] %s %s/%s\n",
        record.OccurredAt.Format("15:04:05"),
        record.Verb,
        record.ObjectType,
        record.ObjectID,
    )
}
```

## Adding Hooks

Hooks allow you to react to events for integrations like notifications, cache invalidation, or analytics:

```go
svc := users.New(users.Config{
    // ... repositories ...
    Hooks: types.Hooks{
        AfterLifecycle: func(ctx context.Context, event types.LifecycleEvent) {
            log.Printf("User %s transitioned from %s to %s",
                event.UserID, event.FromState, event.ToState)
            // Send notification, update cache, etc.
        },
        AfterRoleChange: func(ctx context.Context, event types.RoleEvent) {
            log.Printf("Role change: %s for user %s", event.Action, event.UserID)
        },
        AfterActivity: func(ctx context.Context, record types.ActivityRecord) {
            log.Printf("Activity: %s", record.Verb)
        },
    },
})
```

## Configuration Options

The `Config` struct accepts these options. `HealthCheck` expects all repositories/resolvers to be present; if you only use a subset of features, either skip `HealthCheck` or supply no-op implementations for the features you do not use.

| Option | Required | Description |
|--------|----------|-------------|
| `AuthRepository` | Yes | User CRUD and lifecycle operations |
| `RoleRegistry` | Yes | Role definition and assignment storage |
| `ActivitySink` | Yes | Audit log destination |
| `InventoryRepository` | Conditional | User listing (falls back to AuthRepository if it implements `UserInventoryRepository`) |
| `ActivityRepository` | Conditional | Activity queries (falls back to ActivitySink if it implements `ActivityRepository`) |
| `ProfileRepository` | Conditional | User profile storage and profile commands/queries |
| `PreferenceRepository` | Conditional | Scoped preference storage and preference commands |
| `PreferenceResolver` | Conditional | Required for preference queries; auto-built when PreferenceRepository is provided |
| `Hooks` | No | Event callbacks |
| `Clock` | No | Time source (defaults to system clock) |
| `IDGenerator` | No | UUID generator (defaults to google/uuid) |
| `Logger` | No | Logging interface (defaults to no-op) |
| `TransitionPolicy` | No | Lifecycle transition rules (defaults to standard policy) |
| `InviteTokenTTL` | No | Token expiration (defaults to 72 hours) |
| `ScopeResolver` | No | Multi-tenancy scope resolution |
| `AuthorizationPolicy` | No | Permission checks |

## Complete Example

Here's a complete example combining all the concepts:

```go
package main

import (
    "context"
    "fmt"
    "log"

    users "github.com/goliatone/go-users"
    "github.com/goliatone/go-users/command"
    "github.com/goliatone/go-users/examples/internal/memory"
    "github.com/goliatone/go-users/pkg/types"
    "github.com/google/uuid"
)

func main() {
    ctx := context.Background()

    // Setup repositories
    authRepo := memory.NewAuthRepository()
    activityStore := memory.NewActivityStore()
    roleRegistry := memory.NewRoleRegistry()
    profileRepo := memory.NewProfileRepository()
    preferenceRepo := memory.NewPreferenceRepository()

    // Create service with hooks
    svc := users.New(users.Config{
        AuthRepository:       authRepo,
        InventoryRepository:  authRepo,
        RoleRegistry:         roleRegistry,
        ActivitySink:         activityStore,
        ActivityRepository:   activityStore,
        ProfileRepository:    profileRepo,
        PreferenceRepository: preferenceRepo,
        Hooks: types.Hooks{
            AfterLifecycle: func(_ context.Context, e types.LifecycleEvent) {
                log.Printf("[hook] %s -> %s", e.FromState, e.ToState)
            },
        },
    })

    if err := svc.HealthCheck(ctx); err != nil {
        log.Fatal(err)
    }

    actor := types.ActorRef{ID: uuid.New(), Type: "system_admin"}

    // 1. Invite a user
    inviteResult := &command.UserInviteResult{}
    if err := svc.Commands().UserInvite.Execute(ctx, command.UserInviteInput{
        Email:  "demo@example.com",
        Actor:  actor,
        Result: inviteResult,
    }); err != nil {
        log.Fatal(err)
    }
    fmt.Printf("1. Invited: %s\n", inviteResult.User.Email)

    // 2. Activate the user
    if err := svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
        UserID: inviteResult.User.ID,
        Target: types.LifecycleStateActive,
        Actor:  actor,
        Reason: "demo activation",
    }); err != nil {
        log.Fatal(err)
    }
    fmt.Println("2. User activated")

    // 3. Create a role
    role := &types.RoleDefinition{}
    if err := svc.Commands().CreateRole.Execute(ctx, command.CreateRoleInput{
        Name:        "Editors",
        Permissions: []string{"content.read", "content.write"},
        Actor:       actor,
        Result:      role,
    }); err != nil {
        log.Fatal(err)
    }
    fmt.Printf("3. Created role: %s\n", role.Name)

    // 4. Assign role to user
    if err := svc.Commands().AssignRole.Execute(ctx, command.AssignRoleInput{
        UserID: inviteResult.User.ID,
        RoleID: role.ID,
        Actor:  actor,
    }); err != nil {
        log.Fatal(err)
    }
    fmt.Println("4. Role assigned")

    // 5. Query users
    page, _ := svc.Queries().UserInventory.Query(ctx, types.UserInventoryFilter{
        Actor:      actor,
        Pagination: types.Pagination{Limit: 10},
    })
    fmt.Printf("5. Found %d user(s)\n", page.Total)

    // 6. Query activity
    feed, _ := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
        Actor:      actor,
        Pagination: types.Pagination{Limit: 10},
    })
    fmt.Printf("6. Activity entries: %d\n", len(feed.Records))
}
```

Run the example:

```bash
go run main.go
```

Expected output:

```
[hook] pending -> active
1. Invited: demo@example.com
2. User activated
3. Created role: Editors
4. Role assigned
5. Found 1 user(s)
6. Activity entries: 4
```

## Next Steps

Now that you have a working service, explore these guides for deeper functionality:

- **[GUIDE_USER_LIFECYCLE](GUIDE_USER_LIFECYCLE.md)**: State machine details and transition policies
- **[GUIDE_ROLES](GUIDE_ROLES.md)**: Advanced role management and permissions
- **[ACTIVITY](ACTIVITY.md)**: Activity logging patterns and queries
- **[MULTITENANCY](MULTITENANCY.md)**: Scope guards and authorization policies
- **[PROFILES_PREFERENCES](PROFILES_PREFERENCES.md)**: User profiles and scoped settings

## Production Setup

For production deployments, replace in-memory repositories with persistent storage:

1. Use the Bun repositories under `activity`, `preferences`, `profile`, and `registry` packages
2. Run the SQL migrations from `data/sql/migrations`
3. Implement a `ScopeResolver` for multi-tenancy
4. Implement an `AuthorizationPolicy` for permission checks

See [AUTH_PROVIDER](AUTH_PROVIDER.md), [ROLE_REGISTRY](ROLE_REGISTRY.md), and [MIGRATIONS](../MIGRATIONS.md) for details.

## Resources

- [README](../README.md): Package overview and quick reference
- [SERVICE_REFERENCE](SERVICE_REFERENCE.md): Complete command and query documentation
- [examples/commands](../examples/commands): Runnable example application
- [examples/web](../examples/web): HTTP transport example with Fiber
