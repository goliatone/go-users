# Custom Roles Guide

This guide covers the custom role system in `go-users`, including creating roles, managing permissions, assigning roles to users, and querying role data.

## Overview

The role system provides:

- **Custom role definitions** with permissions, metadata, and ordering
- **Scoped roles** that can be global, tenant-specific, or organization-specific
- **Role assignments** linking users to roles within a scope
- **System roles** vs admin-defined roles distinction
- **Hooks** for notifications and cache invalidation on role changes

## Role Architecture

### Core Components

```
┌─────────────────────────────────────────────────────────────┐
│                      RoleRegistry                           │
├─────────────────────────────────────────────────────────────┤
│  CreateRole    UpdateRole    DeleteRole                     │
│  AssignRole    UnassignRole                                 │
│  ListRoles     GetRole       ListAssignments                │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    Database Tables                          │
├─────────────────────────────────────────────────────────────┤
│  custom_roles          │  user_custom_roles                 │
│  - id                  │  - user_id                         │
│  - name                │  - role_id                         │
│  - description         │  - tenant_id                       │
│  - role_key            │  - org_id                          │
│  - permissions (JSON)  │  - assigned_at                     │
│  - metadata (JSON)     │  - assigned_by                     │
│  - is_system           │                                    │
│  - tenant_id           │                                    │
│  - org_id              │                                    │
│  - order               │                                    │
└─────────────────────────────────────────────────────────────┘
```

### Role Definition Structure

```go
type RoleDefinition struct {
    ID          uuid.UUID          // Unique identifier
    Name        string             // Display name (e.g., "Content Editor")
    Order       int                // Display ordering (lower = higher priority)
    Description string             // Admin-facing description
    RoleKey     string             // Machine key for grouping (e.g., "editor")
    Permissions []string           // Permission slugs (e.g., ["content.read", "content.write"])
    Metadata    map[string]any     // Custom attributes
    IsSystem    bool               // System-defined vs admin-defined
    Scope       ScopeFilter        // Tenant/org scope
    CreatedAt   time.Time
    UpdatedAt   time.Time
    CreatedBy   uuid.UUID
    UpdatedBy   uuid.UUID
}
```

### Built-in vs Custom Roles

`go-users` distinguishes between:

| Type | Description | Example |
|------|-------------|---------|
| **Built-in roles** | Core auth tiers from go-auth (`guest`, `member`, `admin`, `owner`) | Stored in `users.user_role` column |
| **Custom roles** | Application-specific roles managed by go-users | Stored in `custom_roles` table |

Custom roles complement built-in roles by providing fine-grained, application-specific permissions.

## Creating Roles

### Basic Role Creation

```go
role := &types.RoleDefinition{}

err := svc.Commands().CreateRole.Execute(ctx, command.CreateRoleInput{
    Name:        "Content Editor",
    Description: "Can create and edit content but not publish",
    Permissions: []string{"content.read", "content.write", "content.draft"},
    Actor:       actor,
    Result:      role,
})
if err != nil {
    return fmt.Errorf("failed to create role: %w", err)
}

fmt.Printf("Created role: %s (ID: %s)\n", role.Name, role.ID)
```

### Role with All Properties

```go
role := &types.RoleDefinition{}

err := svc.Commands().CreateRole.Execute(ctx, command.CreateRoleInput{
    Name:        "Senior Editor",
    Description: "Full content management with publishing rights",
    RoleKey:     "editor",              // Machine key for grouping
    Order:       10,                    // Display order (lower = first)
    Permissions: []string{
        "content.read",
        "content.write",
        "content.publish",
        "content.delete",
        "media.upload",
    },
    Metadata: map[string]any{
        "color":      "#3B82F6",        // UI hint
        "icon":       "edit-3",         // Icon identifier
        "max_users":  50,               // Business rule
    },
    IsSystem: false,                    // Admin-defined role
    Scope:    types.ScopeFilter{        // Tenant-scoped
        TenantID: tenantID,
    },
    Actor:  actor,
    Result: role,
})
```

### System Roles

Mark roles as system roles when they should not be edited by regular admins:

```go
err := svc.Commands().CreateRole.Execute(ctx, command.CreateRoleInput{
    Name:        "Platform Admin",
    Description: "System-level administration role",
    Permissions: []string{"*"},         // Wildcard for all permissions
    IsSystem:    true,                  // Protected role
    Actor:       systemActor,
    Result:      role,
})
```

## Updating Roles

### Full Update

```go
role := &types.RoleDefinition{}

err := svc.Commands().UpdateRole.Execute(ctx, command.UpdateRoleInput{
    RoleID:      roleID,
    Name:        "Content Editor",       // Required
    Description: "Updated description",
    RoleKey:     "editor",
    Order:       20,
    Permissions: []string{
        "content.read",
        "content.write",
        "content.draft",
        "content.publish",              // Added permission
    },
    Metadata: map[string]any{
        "color": "#10B981",             // Changed color
        "icon":  "edit-3",
    },
    Actor:  actor,
    Result: role,
})
if err != nil {
    return fmt.Errorf("failed to update role: %w", err)
}

fmt.Printf("Updated role: %s\n", role.Name)
```

### Partial Updates

The update command requires a name, but you can preserve other fields by first fetching the role:

```go
// Fetch current role
current, err := svc.Queries().RoleDetail.Query(ctx, query.RoleDetailInput{
    RoleID: roleID,
    Actor:  actor,
})
if err != nil {
    return err
}

// Update only permissions
err = svc.Commands().UpdateRole.Execute(ctx, command.UpdateRoleInput{
    RoleID:      roleID,
    Name:        current.Name,           // Preserve name
    Description: current.Description,    // Preserve description
    RoleKey:     current.RoleKey,
    Order:       current.Order,
    Permissions: append(current.Permissions, "new.permission"),
    Metadata:    current.Metadata,
    Actor:       actor,
})
```

## Deleting Roles

```go
err := svc.Commands().DeleteRole.Execute(ctx, command.DeleteRoleInput{
    RoleID: roleID,
    Scope:  scope,
    Actor:  actor,
})
if err != nil {
    return fmt.Errorf("failed to delete role: %w", err)
}

fmt.Println("Role deleted")
```

Deleting a role also removes all associated assignments.

## Role Assignments

### Assigning a Role to a User

```go
err := svc.Commands().AssignRole.Execute(ctx, command.AssignRoleInput{
    UserID: userID,
    RoleID: roleID,
    Scope:  scope,
    Actor:  actor,
})
if err != nil {
    return fmt.Errorf("failed to assign role: %w", err)
}

fmt.Printf("Assigned role %s to user %s\n", roleID, userID)
```

### Assigning Multiple Roles

```go
roles := []uuid.UUID{editorRoleID, reviewerRoleID, publisherRoleID}

for _, roleID := range roles {
    err := svc.Commands().AssignRole.Execute(ctx, command.AssignRoleInput{
        UserID: userID,
        RoleID: roleID,
        Scope:  scope,
        Actor:  actor,
    })
    if err != nil {
        log.Printf("Failed to assign role %s: %v", roleID, err)
        continue
    }
}
```

### Unassigning a Role

```go
err := svc.Commands().UnassignRole.Execute(ctx, command.UnassignRoleInput{
    UserID: userID,
    RoleID: roleID,
    Scope:  scope,
    Actor:  actor,
})
if err != nil {
    return fmt.Errorf("failed to unassign role: %w", err)
}

fmt.Printf("Unassigned role %s from user %s\n", roleID, userID)
```

### Replacing User Roles

To replace all roles for a user:

```go
func replaceUserRoles(ctx context.Context, svc *users.Service, userID uuid.UUID, newRoles []uuid.UUID, scope types.ScopeFilter, actor types.ActorRef) error {
    // 1. Get current assignments
    current, err := svc.Queries().RoleAssignments.Query(ctx, types.RoleAssignmentFilter{
        Actor:  actor,
        Scope:  scope,
        UserID: userID,
    })
    if err != nil {
        return err
    }

    // 2. Unassign old roles not in new set
    newSet := make(map[uuid.UUID]bool)
    for _, r := range newRoles {
        newSet[r] = true
    }

    for _, assignment := range current {
        if !newSet[assignment.RoleID] {
            if err := svc.Commands().UnassignRole.Execute(ctx, command.UnassignRoleInput{
                UserID: userID,
                RoleID: assignment.RoleID,
                Scope:  scope,
                Actor:  actor,
            }); err != nil {
                return err
            }
        }
    }

    // 3. Assign new roles not already assigned
    currentSet := make(map[uuid.UUID]bool)
    for _, a := range current {
        currentSet[a.RoleID] = true
    }

    for _, roleID := range newRoles {
        if !currentSet[roleID] {
            if err := svc.Commands().AssignRole.Execute(ctx, command.AssignRoleInput{
                UserID: userID,
                RoleID: roleID,
                Scope:  scope,
                Actor:  actor,
            }); err != nil {
                return err
            }
        }
    }

    return nil
}
```

## Querying Roles

### List All Roles

```go
page, err := svc.Queries().RoleList.Query(ctx, types.RoleFilter{
    Actor:      actor,
    Scope:      scope,
    Pagination: types.Pagination{Limit: 50},
})
if err != nil {
    return err
}

fmt.Printf("Found %d roles (total: %d)\n", len(page.Roles), page.Total)
for _, role := range page.Roles {
    fmt.Printf("  - %s: %s (%d permissions)\n",
        role.Name, role.Description, len(role.Permissions))
}
```

### Filter by Keyword

```go
page, err := svc.Queries().RoleList.Query(ctx, types.RoleFilter{
    Actor:   actor,
    Scope:   scope,
    Keyword: "editor",  // Search in name
})
```

### Filter by Role Key

```go
page, err := svc.Queries().RoleList.Query(ctx, types.RoleFilter{
    Actor:   actor,
    Scope:   scope,
    RoleKey: "editor",  // Exact match on role_key
})
```

### Include System Roles

```go
page, err := svc.Queries().RoleList.Query(ctx, types.RoleFilter{
    Actor:         actor,
    Scope:         scope,
    IncludeSystem: true,  // Include is_system=true roles
})
```

### Filter by Specific IDs

```go
page, err := svc.Queries().RoleList.Query(ctx, types.RoleFilter{
    Actor:   actor,
    Scope:   scope,
    RoleIDs: []uuid.UUID{roleID1, roleID2, roleID3},
})
```

### Get Role Details

```go
role, err := svc.Queries().RoleDetail.Query(ctx, query.RoleDetailInput{
    RoleID: roleID,
    Scope:  scope,
    Actor:  actor,
})
if err != nil {
    return err
}

fmt.Printf("Role: %s\n", role.Name)
fmt.Printf("Description: %s\n", role.Description)
fmt.Printf("Permissions: %v\n", role.Permissions)
fmt.Printf("Created: %s by %s\n", role.CreatedAt, role.CreatedBy)
```

### Query Role Assignments

#### Get All Assignments for a User

```go
assignments, err := svc.Queries().RoleAssignments.Query(ctx, types.RoleAssignmentFilter{
    Actor:  actor,
    Scope:  scope,
    UserID: userID,
})
if err != nil {
    return err
}

fmt.Printf("User %s has %d roles:\n", userID, len(assignments))
for _, a := range assignments {
    fmt.Printf("  - %s (assigned by %s on %s)\n",
        a.RoleName, a.AssignedBy, a.AssignedAt.Format("2006-01-02"))
}
```

#### Get All Users with a Role

```go
assignments, err := svc.Queries().RoleAssignments.Query(ctx, types.RoleAssignmentFilter{
    Actor:  actor,
    Scope:  scope,
    RoleID: roleID,
})
if err != nil {
    return err
}

fmt.Printf("Role has %d assigned users:\n", len(assignments))
for _, a := range assignments {
    fmt.Printf("  - User %s\n", a.UserID)
}
```

#### Batch Query Assignments

```go
assignments, err := svc.Queries().RoleAssignments.Query(ctx, types.RoleAssignmentFilter{
    Actor:   actor,
    Scope:   scope,
    UserIDs: []uuid.UUID{user1, user2, user3},
})

// Group by user
byUser := make(map[uuid.UUID][]types.RoleAssignment)
for _, a := range assignments {
    byUser[a.UserID] = append(byUser[a.UserID], a)
}
```

## Scoped Roles

### Global Roles

Roles with zero UUIDs for tenant/org are global:

```go
err := svc.Commands().CreateRole.Execute(ctx, command.CreateRoleInput{
    Name:        "Super Admin",
    Permissions: []string{"*"},
    Scope:       types.ScopeFilter{},  // Global (uuid.Nil)
    Actor:       actor,
    Result:      role,
})
```

### Tenant-Scoped Roles

```go
err := svc.Commands().CreateRole.Execute(ctx, command.CreateRoleInput{
    Name:        "Tenant Editor",
    Permissions: []string{"content.read", "content.write"},
    Scope: types.ScopeFilter{
        TenantID: tenantID,
    },
    Actor:  actor,
    Result: role,
})
```

### Organization-Scoped Roles

```go
err := svc.Commands().CreateRole.Execute(ctx, command.CreateRoleInput{
    Name:        "Org Manager",
    Permissions: []string{"org.read", "org.write", "users.manage"},
    Scope: types.ScopeFilter{
        TenantID: tenantID,
        OrgID:    orgID,
    },
    Actor:  actor,
    Result: role,
})
```

### Scope Hierarchy

When querying roles, the scope filter determines visibility:

```go
// Query tenant-scoped roles only
tenantRoles, _ := svc.Queries().RoleList.Query(ctx, types.RoleFilter{
    Actor: actor,
    Scope: types.ScopeFilter{TenantID: tenantID},
})

// Query org-scoped roles (more restrictive)
orgRoles, _ := svc.Queries().RoleList.Query(ctx, types.RoleFilter{
    Actor: actor,
    Scope: types.ScopeFilter{TenantID: tenantID, OrgID: orgID},
})
```

## Permission Patterns

### Permission Naming Convention

Use a consistent naming pattern for permissions:

```go
// Resource.Action pattern
permissions := []string{
    "users.read",
    "users.write",
    "users.delete",
    "content.read",
    "content.write",
    "content.publish",
    "settings.read",
    "settings.write",
}
```

### Hierarchical Permissions

```go
// Use wildcards for broad access
adminPermissions := []string{"*"}                    // All permissions
contentAdmin := []string{"content.*"}               // All content permissions
readonly := []string{"*.read"}                      // All read permissions
```

### Permission Checking (Application Layer)

```go
func hasPermission(role types.RoleDefinition, required string) bool {
    for _, p := range role.Permissions {
        if p == "*" || p == required {
            return true
        }
        // Handle wildcards like "content.*"
        if strings.HasSuffix(p, ".*") {
            prefix := strings.TrimSuffix(p, ".*")
            if strings.HasPrefix(required, prefix+".") {
                return true
            }
        }
    }
    return false
}

func userHasPermission(ctx context.Context, svc *users.Service, userID uuid.UUID, permission string, actor types.ActorRef, scope types.ScopeFilter) (bool, error) {
    assignments, err := svc.Queries().RoleAssignments.Query(ctx, types.RoleAssignmentFilter{
        Actor:  actor,
        Scope:  scope,
        UserID: userID,
    })
    if err != nil {
        return false, err
    }

    for _, a := range assignments {
        role, err := svc.Queries().RoleDetail.Query(ctx, query.RoleDetailInput{
            RoleID: a.RoleID,
            Scope:  scope,
            Actor:  actor,
        })
        if err != nil {
            continue
        }
        if hasPermission(*role, permission) {
            return true, nil
        }
    }
    return false, nil
}
```

## Role Ordering

Use the `Order` field for consistent display ordering:

```go
// Create roles with explicit ordering
roles := []struct {
    Name  string
    Order int
}{
    {"Owner", 0},
    {"Admin", 10},
    {"Manager", 20},
    {"Editor", 30},
    {"Viewer", 40},
}

for _, r := range roles {
    err := svc.Commands().CreateRole.Execute(ctx, command.CreateRoleInput{
        Name:  r.Name,
        Order: r.Order,
        Actor: actor,
    })
}
```

Roles are returned sorted by order (ascending), then by name alphabetically.

## Role Hooks

React to role changes for cache invalidation or notifications:

```go
svc := users.New(users.Config{
    AuthRepository: repo,
    RoleRegistry:   roleRegistry,
    Hooks: types.Hooks{
        AfterRoleChange: func(ctx context.Context, event types.RoleEvent) {
            log.Printf("Role event: %s", event.Action)

            switch event.Action {
            case "role.created":
                log.Printf("Created role: %s", event.Role.Name)
            case "role.updated":
                log.Printf("Updated role: %s", event.Role.Name)
                invalidateRoleCache(event.RoleID)
            case "role.deleted":
                log.Printf("Deleted role: %s", event.RoleID)
                invalidateRoleCache(event.RoleID)
            case "role.assigned":
                log.Printf("Assigned role %s to user %s", event.RoleID, event.UserID)
                invalidateUserPermissionCache(event.UserID)
            case "role.unassigned":
                log.Printf("Unassigned role %s from user %s", event.RoleID, event.UserID)
                invalidateUserPermissionCache(event.UserID)
            }
        },
    },
    // ...
})
```

### Role Event Structure

```go
type RoleEvent struct {
    RoleID     uuid.UUID       // Affected role
    UserID     uuid.UUID       // Affected user (for assignments)
    Action     string          // Event type
    ActorID    uuid.UUID       // Who performed the action
    Scope      ScopeFilter     // Tenant/org context
    OccurredAt time.Time       // When it happened
    Role       RoleDefinition  // Current role state
}
```

### Event Actions

| Action | Description |
|--------|-------------|
| `role.created` | New role definition created |
| `role.updated` | Role definition modified |
| `role.deleted` | Role definition removed |
| `role.assigned` | User assigned to role |
| `role.unassigned` | User removed from role |

## Common Patterns

### Role Templates

Create roles from predefined templates:

```go
type RoleTemplate struct {
    Name        string
    Description string
    RoleKey     string
    Permissions []string
    Order       int
}

var templates = []RoleTemplate{
    {
        Name:        "Viewer",
        Description: "Read-only access to content",
        RoleKey:     "viewer",
        Permissions: []string{"content.read", "media.read"},
        Order:       40,
    },
    {
        Name:        "Editor",
        Description: "Create and edit content",
        RoleKey:     "editor",
        Permissions: []string{"content.read", "content.write", "media.read", "media.upload"},
        Order:       30,
    },
    {
        Name:        "Publisher",
        Description: "Full content management",
        RoleKey:     "publisher",
        Permissions: []string{"content.*", "media.*"},
        Order:       20,
    },
}

func seedRoles(ctx context.Context, svc *users.Service, tenantID uuid.UUID, actor types.ActorRef) error {
    for _, t := range templates {
        role := &types.RoleDefinition{}
        err := svc.Commands().CreateRole.Execute(ctx, command.CreateRoleInput{
            Name:        t.Name,
            Description: t.Description,
            RoleKey:     t.RoleKey,
            Permissions: t.Permissions,
            Order:       t.Order,
            Scope:       types.ScopeFilter{TenantID: tenantID},
            Actor:       actor,
            Result:      role,
        })
        if err != nil {
            return fmt.Errorf("failed to create role %s: %w", t.Name, err)
        }
    }
    return nil
}
```

### Role-Based Access Control (RBAC)

```go
func requirePermission(permission string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            userID := getUserFromContext(r.Context())
            actor := getActorFromContext(r.Context())
            scope := getScopeFromContext(r.Context())

            hasAccess, err := userHasPermission(r.Context(), svc, userID, permission, actor, scope)
            if err != nil {
                http.Error(w, "Internal error", http.StatusInternalServerError)
                return
            }
            if !hasAccess {
                http.Error(w, "Forbidden", http.StatusForbidden)
                return
            }

            next.ServeHTTP(w, r)
        })
    }
}

// Usage
mux.Handle("/content/publish", requirePermission("content.publish")(publishHandler))
```

### Bulk Role Operations

```go
func assignRolesToUsers(ctx context.Context, svc *users.Service, roleID uuid.UUID, userIDs []uuid.UUID, scope types.ScopeFilter, actor types.ActorRef) error {
    var errs []error

    for _, userID := range userIDs {
        if err := svc.Commands().AssignRole.Execute(ctx, command.AssignRoleInput{
            UserID: userID,
            RoleID: roleID,
            Scope:  scope,
            Actor:  actor,
        }); err != nil {
            errs = append(errs, fmt.Errorf("user %s: %w", userID, err))
        }
    }

    if len(errs) > 0 {
        return errors.Join(errs...)
    }
    return nil
}
```

## Error Handling

### Common Errors

```go
import "github.com/goliatone/go-users/command"

err := svc.Commands().CreateRole.Execute(ctx, input)

switch {
case errors.Is(err, command.ErrRoleNameRequired):
    // Missing role name
case errors.Is(err, command.ErrRoleIDRequired):
    // Missing role ID for update/delete
case errors.Is(err, command.ErrActorRequired):
    // Missing actor reference
case errors.Is(err, command.ErrUserIDRequired):
    // Missing user ID for assignment
default:
    // Repository or other error
}
```

### Handling Not Found

```go
role, err := svc.Queries().RoleDetail.Query(ctx, input)
if err != nil {
    if strings.Contains(err.Error(), "not found") {
        return nil, ErrRoleNotFound
    }
    return nil, err
}
```

## Bun Registry Setup

For production, use the Bun-backed registry:

```go
import (
    "github.com/goliatone/go-users/registry"
    "github.com/uptrace/bun"
)

func setupRoleRegistry(db *bun.DB, hooks types.Hooks) (*registry.RoleRegistry, error) {
    return registry.NewRoleRegistry(registry.RoleRegistryConfig{
        DB:     db,
        Hooks:  hooks,
        Logger: logger,
    })
}

// Wire into service
roleRegistry, _ := setupRoleRegistry(bunDB, hooks)
svc := users.New(users.Config{
    AuthRepository: authRepo,
    RoleRegistry:   roleRegistry,
    // ...
})
```

## Next Steps

- **[GUIDE_ACTIVITY](GUIDE_ACTIVITY.md)**: Track role changes in activity feeds
- **[GUIDE_MULTITENANCY](GUIDE_MULTITENANCY.md)**: Scope roles by tenant/organization
- **[GUIDE_HOOKS](GUIDE_HOOKS.md)**: Advanced hook patterns for role events
- **[GUIDE_QUERIES](GUIDE_QUERIES.md)**: Query patterns for role dashboards
