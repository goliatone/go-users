# CRUD Integration Guide

This guide covers integrating `go-users` with `go-crud` to build REST APIs for user management. Learn how to use the crudguard adapter for scope enforcement and the pre-built service implementations.

## Table of Contents

- [Overview](#overview)
- [go-crud Adapter Overview](#go-crud-adapter-overview)
- [crudguard.Adapter Setup](#crudguardadapter-setup)
  - [Configuration](#configuration)
  - [Policy Mapping](#policy-mapping)
  - [Scope Extraction](#scope-extraction)
- [Scope Enforcement in CRUD Operations](#scope-enforcement-in-crud-operations)
- [Field and Row Policies](#field-and-row-policies)
- [Service Implementations](#service-implementations)
  - [UserService](#userservice)
  - [RoleService](#roleservice)
  - [ActivityService](#activityservice)
  - [PreferenceService](#preferenceservice)
- [HTTP Endpoint Patterns](#http-endpoint-patterns)
- [Request/Response Transformations](#requestresponse-transformations)
- [Authorization Middleware Integration](#authorization-middleware-integration)
- [Error Handling and Responses](#error-handling-and-responses)
- [Next Steps](#next-steps)

---

## Overview

`go-users` provides a seamless integration layer with `go-crud` to expose user management functionality via REST APIs. The integration consists of two main components:

```
┌─────────────────────────────────────────────────────────────┐
│                 go-crud Integration Stack                    │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│   ┌─────────────────┐    ┌─────────────────┐               │
│   │   go-crud       │    │   crudguard     │               │
│   │   Controller    │ →  │   Adapter       │               │
│   └─────────────────┘    └─────────────────┘               │
│          │                      │                           │
│          ▼                      ▼                           │
│   ┌─────────────────┐    ┌─────────────────┐               │
│   │   crudsvc       │ →  │   Scope Guard   │               │
│   │   Services      │    │   (AuthZ)       │               │
│   └─────────────────┘    └─────────────────┘               │
│          │                      │                           │
│          ▼                      ▼                           │
│   ┌─────────────────────────────────────────┐              │
│   │          go-users Service               │              │
│   │   (Commands, Queries, Repositories)     │              │
│   └─────────────────────────────────────────┘              │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

**Components:**

- **crudguard.Adapter**: Bridges go-crud operations to the scope guard for authorization
- **crudsvc Services**: Pre-built go-crud service implementations for users, roles, activity, and preferences
- **Field/Row Policies**: Fine-grained access control based on actor roles

---

## go-crud Adapter Overview

The `crudguard` package provides an adapter that maps go-crud operations to `go-users` policy actions, enabling consistent scope enforcement across all CRUD endpoints.

### Key Features

- Maps CRUD operations to policy actions
- Extracts scope from actor context or custom extractors
- Supports bypass configuration for whitelisted routes
- Returns resolved scope for downstream use

---

## crudguard.Adapter Setup

### Configuration

```go
package main

import (
    "github.com/goliatone/go-crud"
    "github.com/goliatone/go-users/crudguard"
    "github.com/goliatone/go-users/pkg/types"
    "github.com/goliatone/go-users/scope"
)

func setupAdapter(scopeGuard scope.Guard) (*crudguard.Adapter, error) {
    return crudguard.NewAdapter(crudguard.Config{
        Guard: scopeGuard,  // Required: the scope guard from go-users

        // Map CRUD operations to policy actions
        PolicyMap: crudguard.DefaultPolicyMap(
            types.PolicyActionUsersRead,   // Read action
            types.PolicyActionUsersWrite,  // Write action
        ),

        // Optional: custom scope extractor
        ScopeExtractor: crudguard.DefaultScopeExtractor,

        // Optional: fallback action if operation not in map
        FallbackAction: types.PolicyActionUsersRead,

        // Optional: logger
        Logger: myLogger,
    })
}
```

### Policy Mapping

The `DefaultPolicyMap` helper creates a standard mapping:

```go
func DefaultPolicyMap(readAction, writeAction types.PolicyAction) map[crud.CrudOperation]types.PolicyAction {
    return map[crud.CrudOperation]types.PolicyAction{
        crud.OpRead:        readAction,   // GET /resource/:id
        crud.OpList:        readAction,   // GET /resource
        crud.OpCreate:      writeAction,  // POST /resource
        crud.OpCreateBatch: writeAction,  // POST /resource/batch
        crud.OpUpdate:      writeAction,  // PUT /resource/:id
        crud.OpUpdateBatch: writeAction,  // PUT /resource/batch
        crud.OpDelete:      writeAction,  // DELETE /resource/:id
        crud.OpDeleteBatch: writeAction,  // DELETE /resource/batch
    }
}
```

Custom mappings for fine-grained control:

```go
policyMap := map[crud.CrudOperation]types.PolicyAction{
    crud.OpRead:   types.PolicyActionUsersRead,
    crud.OpList:   types.PolicyActionUsersRead,
    crud.OpCreate: types.PolicyActionUsersWrite,
    crud.OpUpdate: types.PolicyActionUsersWrite,
    crud.OpDelete: types.PolicyActionUsersDelete,  // Separate delete permission
}
```

### Scope Extraction

The default scope extractor derives scope from the actor context:

```go
// DefaultScopeExtractor builds the requested scope from the actor context
func DefaultScopeExtractor(_ crud.Context, actor *auth.ActorContext) (types.ScopeFilter, error) {
    return authctx.ScopeFromActorContext(actor), nil
}
```

Custom extractors for additional scope sources:

```go
// Extract scope from query parameters
func QueryParamScopeExtractor(ctx crud.Context, actor *auth.ActorContext) (types.ScopeFilter, error) {
    scope := authctx.ScopeFromActorContext(actor)

    // Override with query params if admin
    if tenantID := ctx.Query("tenant_id"); tenantID != "" {
        if id, err := uuid.Parse(tenantID); err == nil {
            scope.TenantID = id
        }
    }

    return scope, nil
}

// Extract scope from request body
func BodyScopeExtractor(ctx crud.Context, actor *auth.ActorContext) (types.ScopeFilter, error) {
    scope := authctx.ScopeFromActorContext(actor)

    // Parse body for scope fields
    var body struct {
        TenantID uuid.UUID `json:"tenant_id"`
        OrgID    uuid.UUID `json:"org_id"`
    }
    if err := ctx.BodyParser(&body); err == nil {
        if body.TenantID != uuid.Nil {
            scope.TenantID = body.TenantID
        }
        if body.OrgID != uuid.Nil {
            scope.OrgID = body.OrgID
        }
    }

    return scope, nil
}
```

---

## Scope Enforcement in CRUD Operations

The adapter enforces scope on every CRUD operation:

```go
// GuardInput captures per-request parameters
type GuardInput struct {
    Context   crud.Context       // The CRUD context
    Operation crud.CrudOperation // The operation being performed
    TargetID  uuid.UUID          // Target resource ID (for read/update/delete)
    Scope     types.ScopeFilter  // Additional scope constraints
    Bypass    *BypassConfig      // Optional bypass configuration
}

// GuardResult reports the resolved scope and actor
type GuardResult struct {
    Actor        types.ActorRef
    Scope        types.ScopeFilter
    Operation    crud.CrudOperation
    Bypassed     bool
    BypassReason string
}
```

Usage in service implementations:

```go
func (s *MyService) Show(ctx crud.Context, id string) (*MyRecord, error) {
    recordID, err := uuid.Parse(id)
    if err != nil {
        return nil, err
    }

    // Enforce scope guard
    res, err := s.guard.Enforce(crudguard.GuardInput{
        Context:   ctx,
        Operation: crud.OpRead,
        TargetID:  recordID,
    })
    if err != nil {
        return nil, err  // Authorization denied
    }

    // Use resolved scope for query
    return s.repo.GetByID(ctx.UserContext(), recordID, res.Scope)
}
```

### Bypass Configuration

For whitelisted routes (schema exports, health checks):

```go
res, err := s.guard.Enforce(crudguard.GuardInput{
    Context:   ctx,
    Operation: crud.OpList,
    Bypass: &crudguard.BypassConfig{
        Enabled: true,
        Reason:  "schema export endpoint",
    },
})
// Guard enforcement is skipped, but scope is still resolved from actor
```

---

## Field and Row Policies

The crudsvc services implement field-level and row-level access control:

### Row Policies

Row policies restrict which records an actor can access:

```go
// Support actors can only see their own records
func applyUserInventoryRowPolicy(filter *types.UserInventoryFilter, actor types.ActorRef) {
    if actor.IsSupport() {
        filter.UserIDs = []uuid.UUID{actor.ID}  // Limit to own record
    }
}

// Filter results after query
func filterUserInventoryResults(users []types.AuthUser, actor types.ActorRef) []types.AuthUser {
    if !actor.IsSupport() {
        return users
    }
    // Support actors only see themselves
    filtered := make([]types.AuthUser, 0, 1)
    for _, user := range users {
        if user.ID == actor.ID {
            filtered = append(filtered, user)
        }
    }
    return filtered
}
```

### Field Policies

Field policies redact sensitive data based on actor roles:

```go
// Redact fields for support actors viewing other users
func applyUserFieldPolicy(user *auth.User, actor types.ActorRef) *auth.User {
    if user == nil {
        return nil
    }

    // Support actors viewing their own record see everything
    if !actor.IsSupport() || user.ID == actor.ID {
        return user
    }

    // Redact sensitive fields for other users
    user.Email = obfuscateEmail(user.Email)  // j***@example.com
    user.Username = ""
    user.FirstName = ""
    user.LastName = ""
    user.Metadata = nil
    user.Phone = ""
    user.ProfilePicture = ""
    user.LoginAttempts = 0
    user.LoginAttemptAt = nil
    user.LoggedInAt = nil
    user.SuspendedAt = nil
    user.ResetedAt = nil

    return user
}

// Activity field policy - redact IP for non-admins
func applyActivityFieldPolicy(entry *activity.LogEntry, actor types.ActorRef) *activity.LogEntry {
    if entry == nil {
        return nil
    }

    // Only system admins see IP addresses
    if !actor.IsSystemAdmin() {
        entry.IP = ""
    }

    // Support actors don't see metadata
    if actor.IsSupport() && len(entry.Data) > 0 {
        entry.Data = nil
    }

    return entry
}
```

---

## Service Implementations

### UserService

Read-only service for user inventory (admin panels):

```go
package main

import (
    "github.com/goliatone/go-users/crudsvc"
)

func setupUserService(svc *service.Service, guard *crudguard.Adapter) *crudsvc.UserService {
    return crudsvc.NewUserService(crudsvc.UserServiceConfig{
        Guard:     guard,
        Inventory: svc.Queries().UserInventory,
        AuthRepo:  authRepo,  // For Show() lookups
    })
}
```

**Supported Operations:**

| Operation | Supported | Notes |
|-----------|-----------|-------|
| Index (List) | Yes | Paginated user listing with filters |
| Show (Read) | Yes | Single user lookup by ID |
| Create | No | Use go-auth directly |
| Update | No | Use go-auth directly |
| Delete | No | Use lifecycle transitions instead |

**Query Parameters:**

- `q` - Keyword search
- `status` - Filter by lifecycle status (comma-separated)
- `limit` - Page size (default: 50, max: 200)
- `offset` - Skip N records

### RoleService

Full CRUD for custom roles:

```go
func setupRoleService(svc *service.Service, guard *crudguard.Adapter) *crudsvc.RoleService {
    return crudsvc.NewRoleService(crudsvc.RoleServiceConfig{
        Guard:  guard,
        Create: svc.Commands().CreateRole,
        Update: svc.Commands().UpdateRole,
        Delete: svc.Commands().DeleteRole,
        List:   svc.Queries().RoleList,
        Detail: svc.Queries().RoleDetail,
    },
        crudsvc.WithActivityEmitter(activityEmitter),
        crudsvc.WithLogger(logger),
    )
}
```

**Supported Operations:**

| Operation | Supported | Notes |
|-----------|-----------|-------|
| Index (List) | Yes | List roles with filters |
| Show (Read) | Yes | Role detail by ID |
| Create | Yes | Create new custom role |
| Update | Yes | Update existing role |
| Delete | Yes | Delete custom role |
| Batch ops | No | Not supported |

**Query Parameters:**

- `q` - Keyword search
- `include_system` - Include system roles (default: false)
- `limit`, `offset` - Pagination

### ActivityService

Activity logging and feed queries:

```go
func setupActivityService(svc *service.Service, guard *crudguard.Adapter) *crudsvc.ActivityService {
    return crudsvc.NewActivityService(crudsvc.ActivityServiceConfig{
        Guard:      guard,
        LogCommand: svc.Commands().LogActivity,
        FeedQuery:  svc.Queries().ActivityFeed,
    },
        crudsvc.WithActivityEmitter(activityEmitter),
        crudsvc.WithLogger(logger),
    )
}
```

**Supported Operations:**

| Operation | Supported | Notes |
|-----------|-----------|-------|
| Index (List) | Yes | Paginated activity feed |
| Create | Yes | Log new activity |
| CreateBatch | Yes | Log multiple activities |
| Show/Update/Delete | No | Activity is append-only |

**Query Parameters:**

- `user_id` - Filter by subject user
- `actor_id` - Filter by actor
- `verb` - Filter by verbs (comma-separated)
- `object_type` - Filter by object type
- `object_id` - Filter by object ID
- `channel` - Filter by channel
- `since`, `until` - Time range (RFC3339)
- `q` - Keyword search
- `limit`, `offset` - Pagination

### PreferenceService

User preference management:

```go
func setupPreferenceService(svc *service.Service, guard *crudguard.Adapter, prefRepo *preferences.Repository) *crudsvc.PreferenceService {
    return crudsvc.NewPreferenceService(crudsvc.PreferenceServiceConfig{
        Guard:  guard,
        Repo:   prefRepo,
        Store:  prefRepo,  // For GetByID
        Upsert: svc.Commands().PreferenceUpsert,
        Delete: svc.Commands().PreferenceDelete,
    },
        crudsvc.WithActivityEmitter(activityEmitter),
        crudsvc.WithLogger(logger),
    )
}
```

**Supported Operations:**

| Operation | Supported | Notes |
|-----------|-----------|-------|
| Index (List) | Yes | List preferences with filters |
| Show (Read) | Yes | Get preference by ID |
| Create | Yes | Create/upsert preference |
| Update | Yes | Update preference |
| Delete | Yes | Delete preference |
| Batch ops | Yes | Batch create/update/delete |

**Query Parameters:**

- `user_id` - Filter by user
- `level` - Filter by scope level
- `key` - Filter by keys (comma-separated)
- `limit`, `offset` - Pagination

---

## HTTP Endpoint Patterns

### Router Setup with go-crud

```go
package main

import (
    "github.com/gofiber/fiber/v2"
    "github.com/goliatone/go-crud"
    fibercrud "github.com/goliatone/go-crud/adapters/fiber"
)

func setupRoutes(app *fiber.App, services Services) {
    api := app.Group("/api/v1")

    // Users (read-only)
    userCtrl := fibercrud.NewController(services.UserService, fibercrud.Config{
        ResourceName: "users",
    })
    api.Get("/users", userCtrl.Index)
    api.Get("/users/:id", userCtrl.Show)

    // Roles (full CRUD)
    roleCtrl := fibercrud.NewController(services.RoleService, fibercrud.Config{
        ResourceName: "roles",
    })
    api.Get("/roles", roleCtrl.Index)
    api.Get("/roles/:id", roleCtrl.Show)
    api.Post("/roles", roleCtrl.Create)
    api.Put("/roles/:id", roleCtrl.Update)
    api.Delete("/roles/:id", roleCtrl.Delete)

    // Activity
    activityCtrl := fibercrud.NewController(services.ActivityService, fibercrud.Config{
        ResourceName: "activities",
    })
    api.Get("/activities", activityCtrl.Index)
    api.Post("/activities", activityCtrl.Create)

    // Preferences
    prefCtrl := fibercrud.NewController(services.PreferenceService, fibercrud.Config{
        ResourceName: "preferences",
    })
    api.Get("/preferences", prefCtrl.Index)
    api.Get("/preferences/:id", prefCtrl.Show)
    api.Post("/preferences", prefCtrl.Create)
    api.Put("/preferences/:id", prefCtrl.Update)
    api.Delete("/preferences/:id", prefCtrl.Delete)
}
```

### Custom Endpoints

For operations not covered by CRUD:

```go
// Lifecycle transitions (not CRUD)
api.Post("/users/:id/activate", handleActivateUser)
api.Post("/users/:id/suspend", handleSuspendUser)

// Role assignments
api.Post("/users/:id/roles", handleAssignRole)
api.Delete("/users/:id/roles/:roleId", handleUnassignRole)

// Bulk operations
api.Post("/users/bulk/transition", handleBulkTransition)
```

---

## Request/Response Transformations

### Input Transformation

The services handle transformation from HTTP to domain models:

```go
// go-crud provides the parsed body
func (s *RoleService) Create(ctx crud.Context, record *registry.CustomRole) (*registry.CustomRole, error) {
    // record is already parsed from JSON body
    // Transform to command input
    input := command.CreateRoleInput{
        Name:        record.Name,
        Description: record.Description,
        RoleKey:     record.RoleKey,
        Permissions: record.Permissions,
        Metadata:    record.Metadata,
        Scope:       res.Scope,
        Actor:       res.Actor,
    }
    // ...
}
```

### Output Transformation

Services transform domain models to HTTP responses:

```go
// Transform domain to HTTP response
func (s *UserService) Index(ctx crud.Context, ...) ([]*auth.User, int, error) {
    // Query returns domain objects
    page, err := s.inventory.Query(ctx.UserContext(), filter)

    // Transform to HTTP model
    records := make([]*auth.User, 0, len(page.Users))
    for _, user := range page.Users {
        record := sanitizeUser(goauth.UserFromDomain(&user))
        records = append(records, applyUserFieldPolicy(record, res.Actor))
    }

    return records, len(users), nil
}

// Sanitize sensitive fields
func sanitizeUser(user *auth.User) *auth.User {
    if user == nil {
        return nil
    }
    clone := *user
    clone.PasswordHash = ""  // Never expose password hash
    return &clone
}
```

---

## Authorization Middleware Integration

### Actor Context Middleware

Ensure actor context is set before CRUD operations:

```go
func AuthMiddleware(authService auth.Service) fiber.Handler {
    return func(c *fiber.Ctx) error {
        // Extract token from header
        token := c.Get("Authorization")
        if token == "" {
            return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
        }

        // Validate and get actor context
        actorCtx, err := authService.ValidateToken(c.Context(), token)
        if err != nil {
            return c.Status(401).JSON(fiber.Map{"error": "invalid token"})
        }

        // Store in context for crudguard
        ctx := authctx.WithActorContext(c.Context(), actorCtx)
        c.SetUserContext(ctx)

        return c.Next()
    }
}

// Apply to protected routes
api := app.Group("/api/v1", AuthMiddleware(authService))
```

### Scope Injection

For multi-tenant apps, inject scope from JWT claims:

```go
func ScopeMiddleware() fiber.Handler {
    return func(c *fiber.Ctx) error {
        actorCtx := authctx.FromContext(c.UserContext())
        if actorCtx == nil {
            return c.Next()
        }

        // Extract scope from claims
        claims := actorCtx.Claims()
        scope := types.ScopeFilter{
            TenantID: claims.TenantID,
            OrgID:    claims.OrgID,
        }

        // Store resolved scope
        ctx := authctx.WithScope(c.UserContext(), scope)
        c.SetUserContext(ctx)

        return c.Next()
    }
}
```

---

## Error Handling and Responses

### Error Types

The crudsvc services use `go-errors` for structured errors:

```go
import goerrors "github.com/goliatone/go-errors"

// Common error patterns
goerrors.New("message", goerrors.CategoryValidation).
    WithCode(goerrors.CodeBadRequest)

goerrors.New("message", goerrors.CategoryNotFound).
    WithCode(goerrors.CodeNotFound)

goerrors.New("message", goerrors.CategoryAuthz).
    WithCode(goerrors.CodeForbidden)

goerrors.New("message", goerrors.CategoryInternal).
    WithCode(goerrors.CodeInternal)
```

### Error Response Middleware

```go
func ErrorHandler(c *fiber.Ctx, err error) error {
    var goErr *goerrors.Error
    if errors.As(err, &goErr) {
        status := httpStatusFromCode(goErr.Code())
        return c.Status(status).JSON(fiber.Map{
            "error":     goErr.Message(),
            "code":      goErr.TextCode(),
            "category":  goErr.Category(),
        })
    }

    // Generic error
    return c.Status(500).JSON(fiber.Map{
        "error": "internal server error",
    })
}

func httpStatusFromCode(code goerrors.Code) int {
    switch code {
    case goerrors.CodeBadRequest:
        return 400
    case goerrors.CodeUnauthorized:
        return 401
    case goerrors.CodeForbidden:
        return 403
    case goerrors.CodeNotFound:
        return 404
    default:
        return 500
    }
}
```

### Scope Guard Errors

```go
// Scope-related error codes
const (
    textCodeScopeDenied          = "SCOPE_DENIED"
    textCodeScopeEnforcementFail = "SCOPE_ENFORCEMENT_FAILED"
    textCodeMissingPolicy        = "SCOPE_POLICY_MISSING"
    textCodeMissingContext       = "CONTEXT_MISSING"
)

// Handle in error middleware
switch goErr.TextCode() {
case "SCOPE_DENIED":
    // User not authorized for this scope
    return c.Status(403).JSON(...)
case "SCOPE_ENFORCEMENT_FAILED":
    // Guard configuration error
    return c.Status(500).JSON(...)
}
```

---

## Complete Example

```go
package main

import (
    "github.com/gofiber/fiber/v2"
    "github.com/goliatone/go-crud"
    fibercrud "github.com/goliatone/go-crud/adapters/fiber"
    "github.com/goliatone/go-users/crudguard"
    "github.com/goliatone/go-users/crudsvc"
    "github.com/goliatone/go-users/pkg/types"
    "github.com/goliatone/go-users/service"
)

func main() {
    // Setup go-users service
    svc := service.New(service.Config{
        AuthRepository:       authRepo,
        RoleRegistry:         roleRegistry,
        ActivitySink:         activitySink,
        PreferenceRepository: prefRepo,
        ScopeResolver:        scopeResolver,
        AuthorizationPolicy:  authzPolicy,
        // ... other config
    })

    // Create guard adapters for each resource
    userGuard, _ := crudguard.NewAdapter(crudguard.Config{
        Guard:     svc.ScopeGuard(),
        PolicyMap: crudguard.DefaultPolicyMap(
            types.PolicyActionUsersRead,
            types.PolicyActionUsersWrite,
        ),
    })

    roleGuard, _ := crudguard.NewAdapter(crudguard.Config{
        Guard:     svc.ScopeGuard(),
        PolicyMap: crudguard.DefaultPolicyMap(
            types.PolicyActionRolesRead,
            types.PolicyActionRolesWrite,
        ),
    })

    activityGuard, _ := crudguard.NewAdapter(crudguard.Config{
        Guard:     svc.ScopeGuard(),
        PolicyMap: crudguard.DefaultPolicyMap(
            types.PolicyActionActivityRead,
            types.PolicyActionActivityWrite,
        ),
    })

    prefGuard, _ := crudguard.NewAdapter(crudguard.Config{
        Guard:     svc.ScopeGuard(),
        PolicyMap: crudguard.DefaultPolicyMap(
            types.PolicyActionPreferencesRead,
            types.PolicyActionPreferencesWrite,
        ),
    })

    // Create CRUD services
    userService := crudsvc.NewUserService(crudsvc.UserServiceConfig{
        Guard:     userGuard,
        Inventory: svc.Queries().UserInventory,
        AuthRepo:  authRepo,
    })

    roleService := crudsvc.NewRoleService(crudsvc.RoleServiceConfig{
        Guard:  roleGuard,
        Create: svc.Commands().CreateRole,
        Update: svc.Commands().UpdateRole,
        Delete: svc.Commands().DeleteRole,
        List:   svc.Queries().RoleList,
        Detail: svc.Queries().RoleDetail,
    })

    activityService := crudsvc.NewActivityService(crudsvc.ActivityServiceConfig{
        Guard:      activityGuard,
        LogCommand: svc.Commands().LogActivity,
        FeedQuery:  svc.Queries().ActivityFeed,
    })

    prefService := crudsvc.NewPreferenceService(crudsvc.PreferenceServiceConfig{
        Guard:  prefGuard,
        Repo:   prefRepo,
        Store:  prefRepo,
        Upsert: svc.Commands().PreferenceUpsert,
        Delete: svc.Commands().PreferenceDelete,
    })

    // Setup Fiber app
    app := fiber.New(fiber.Config{
        ErrorHandler: ErrorHandler,
    })

    // Protected routes
    api := app.Group("/api/v1", AuthMiddleware(authService))

    // Register controllers
    userCtrl := fibercrud.NewController(userService, fibercrud.Config{ResourceName: "users"})
    api.Get("/users", userCtrl.Index)
    api.Get("/users/:id", userCtrl.Show)

    roleCtrl := fibercrud.NewController(roleService, fibercrud.Config{ResourceName: "roles"})
    api.Get("/roles", roleCtrl.Index)
    api.Get("/roles/:id", roleCtrl.Show)
    api.Post("/roles", roleCtrl.Create)
    api.Put("/roles/:id", roleCtrl.Update)
    api.Delete("/roles/:id", roleCtrl.Delete)

    activityCtrl := fibercrud.NewController(activityService, fibercrud.Config{ResourceName: "activities"})
    api.Get("/activities", activityCtrl.Index)
    api.Post("/activities", activityCtrl.Create)

    prefCtrl := fibercrud.NewController(prefService, fibercrud.Config{ResourceName: "preferences"})
    api.Get("/preferences", prefCtrl.Index)
    api.Get("/preferences/:id", prefCtrl.Show)
    api.Post("/preferences", prefCtrl.Create)
    api.Put("/preferences/:id", prefCtrl.Update)
    api.Delete("/preferences/:id", prefCtrl.Delete)

    app.Listen(":8080")
}
```

---

## Next Steps

- [GUIDE_MULTITENANCY.md](GUIDE_MULTITENANCY.md) - Scope resolution and authorization policies
- [GUIDE_QUERIES.md](GUIDE_QUERIES.md) - Query patterns for admin interfaces
- [GUIDE_REPOSITORIES.md](GUIDE_REPOSITORIES.md) - Custom repository implementations
- [GUIDE_HOOKS.md](GUIDE_HOOKS.md) - Event-driven integrations
