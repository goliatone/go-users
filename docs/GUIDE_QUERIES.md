# Queries Guide

This guide covers the query architecture in `go-users` and how to effectively query users, roles, activity, profiles, and preferences. Learn pagination patterns, filtering strategies, and best practices for building admin interfaces.

## Table of Contents

- [Overview](#overview)
- [Query Architecture](#query-architecture)
- [User Inventory Queries](#user-inventory-queries)
  - [Filters](#filters)
  - [Pagination](#pagination)
  - [Response Structure](#response-structure)
- [Role Queries](#role-queries)
  - [Listing Roles](#listing-roles)
  - [Role Detail Lookups](#role-detail-lookups)
  - [Assignment Queries](#assignment-queries)
- [Activity Queries](#activity-queries)
  - [Feed Queries](#feed-queries)
  - [Verb and Channel Filtering](#verb-and-channel-filtering)
  - [Stats Aggregations](#stats-aggregations)
- [Profile and Preference Queries](#profile-and-preference-queries)
- [Pagination Patterns and Best Practices](#pagination-patterns-and-best-practices)
- [Building Admin Search Interfaces](#building-admin-search-interfaces)
- [Error Handling](#error-handling)
- [Next Steps](#next-steps)

---

## Overview

`go-users` follows a Command/Query separation pattern. While commands mutate state, queries provide read-only access to data. All queries:

- Are scope-aware (respect tenant/org isolation)
- Require an authenticated actor
- Support pagination for list operations
- Return structured response types

```
┌─────────────────────────────────────────────────────────────┐
│                    Query Architecture                        │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│   ┌─────────────┐    ┌─────────────┐    ┌───────────────┐  │
│   │   Filter    │ →  │ Scope Guard │ →  │  Repository   │  │
│   │   Input     │    │ (authz)     │    │   (data)      │  │
│   └─────────────┘    └─────────────┘    └───────────────┘  │
│         │                   │                   │           │
│         ▼                   ▼                   ▼           │
│   - Actor (who)       - Enforce scope    - Execute query   │
│   - Scope filter      - Check policy     - Return page     │
│   - Pagination        - Augment filter                     │
│   - Search criteria                                        │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

---

## Query Architecture

### The Queries Facade

All queries are accessible through the service's `Queries()` method:

```go
svc := service.New(service.Config{...})

queries := svc.Queries()

// Available queries:
queries.UserInventory    // List/search users
queries.RoleList         // List roles
queries.RoleDetail       // Get single role
queries.RoleAssignments  // List role assignments
queries.ActivityFeed     // Paginated activity logs
queries.ActivityStats    // Activity aggregations
queries.ProfileDetail    // Get user profile
queries.Preferences      // Get effective preferences
```

### Common Query Pattern

All queries follow a consistent pattern:

```go
// 1. Construct filter/input with required Actor
filter := types.SomeFilter{
    Actor: types.ActorRef{
        ID:    actorID,
        Roles: []string{"admin"},
    },
    Scope: types.ScopeFilter{
        TenantID: tenantID,
    },
    // ... other criteria
}

// 2. Execute query
result, err := svc.Queries().SomeQuery.Query(ctx, filter)
if err != nil {
    return err
}

// 3. Process result
for _, item := range result.Items {
    // ...
}
```

---

## User Inventory Queries

The `UserInventory` query provides powerful user listing and search capabilities for admin dashboards.

### Filters

```go
type UserInventoryFilter struct {
    Actor      ActorRef         // Required: who is querying
    Scope      ScopeFilter      // Tenant/org isolation
    Statuses   []LifecycleState // Filter by status
    Role       string           // Filter by role key
    Keyword    string           // Search term
    Pagination Pagination       // Limit/offset
    UserIDs    []uuid.UUID      // Specific user IDs
}
```

#### Basic User Listing

```go
func listUsers(ctx context.Context, svc *service.Service, actor types.ActorRef) ([]types.AuthUser, error) {
    page, err := svc.Queries().UserInventory.Query(ctx, types.UserInventoryFilter{
        Actor: actor,
        Scope: types.ScopeFilter{
            TenantID: uuid.MustParse("tenant-uuid"),
        },
        Pagination: types.Pagination{
            Limit:  50,
            Offset: 0,
        },
    })
    if err != nil {
        return nil, err
    }
    return page.Users, nil
}
```

#### Filter by Status

```go
// Get all active users
page, err := svc.Queries().UserInventory.Query(ctx, types.UserInventoryFilter{
    Actor: actor,
    Scope: scopeFilter,
    Statuses: []types.LifecycleState{
        types.LifecycleStateActive,
    },
    Pagination: types.Pagination{Limit: 50},
})

// Get suspended and disabled users
page, err := svc.Queries().UserInventory.Query(ctx, types.UserInventoryFilter{
    Actor: actor,
    Scope: scopeFilter,
    Statuses: []types.LifecycleState{
        types.LifecycleStateSuspended,
        types.LifecycleStateDisabled,
    },
    Pagination: types.Pagination{Limit: 50},
})
```

#### Filter by Role

```go
// Get all admin users
page, err := svc.Queries().UserInventory.Query(ctx, types.UserInventoryFilter{
    Actor: actor,
    Scope: scopeFilter,
    Role:  "admin",  // Role key
    Pagination: types.Pagination{Limit: 50},
})
```

#### Keyword Search

```go
// Search users by email, name, etc.
page, err := svc.Queries().UserInventory.Query(ctx, types.UserInventoryFilter{
    Actor:   actor,
    Scope:   scopeFilter,
    Keyword: "john@example",  // Searches email, username, etc.
    Pagination: types.Pagination{Limit: 50},
})
```

#### Fetch Specific Users

```go
// Get specific users by ID
page, err := svc.Queries().UserInventory.Query(ctx, types.UserInventoryFilter{
    Actor: actor,
    Scope: scopeFilter,
    UserIDs: []uuid.UUID{
        uuid.MustParse("user-1-uuid"),
        uuid.MustParse("user-2-uuid"),
        uuid.MustParse("user-3-uuid"),
    },
})
```

### Pagination

User inventory queries support offset-based pagination:

```go
type Pagination struct {
    Limit  int  // Items per page (default: 50, max: 200)
    Offset int  // Skip N items
}
```

#### Paginating Through Results

```go
func getAllUsers(ctx context.Context, svc *service.Service, actor types.ActorRef, scopeFilter types.ScopeFilter) ([]types.AuthUser, error) {
    var allUsers []types.AuthUser
    offset := 0
    limit := 100

    for {
        page, err := svc.Queries().UserInventory.Query(ctx, types.UserInventoryFilter{
            Actor: actor,
            Scope: scopeFilter,
            Pagination: types.Pagination{
                Limit:  limit,
                Offset: offset,
            },
        })
        if err != nil {
            return nil, err
        }

        allUsers = append(allUsers, page.Users...)

        if !page.HasMore {
            break
        }
        offset = page.NextOffset
    }

    return allUsers, nil
}
```

### Response Structure

```go
type UserInventoryPage struct {
    Users      []AuthUser  // Users in this page
    Total      int         // Total matching users
    NextOffset int         // Offset for next page
    HasMore    bool        // More pages available?
}

type AuthUser struct {
    ID        uuid.UUID
    Email     string
    Username  string
    Status    LifecycleState
    Roles     []string
    TenantID  uuid.UUID
    OrgID     uuid.UUID
    CreatedAt time.Time
    UpdatedAt time.Time
    // ... other fields from go-auth
}
```

---

## Role Queries

### Listing Roles

```go
type RoleFilter struct {
    Actor         ActorRef      // Required
    Scope         ScopeFilter   // Tenant/org
    Keyword       string        // Search name/description
    RoleKey       string        // Exact role key match
    IncludeSystem bool          // Include system roles
    RoleIDs       []uuid.UUID   // Specific role IDs
    Pagination    Pagination
}
```

#### List All Custom Roles

```go
func listRoles(ctx context.Context, svc *service.Service, actor types.ActorRef, scopeFilter types.ScopeFilter) ([]types.RoleDefinition, error) {
    page, err := svc.Queries().RoleList.Query(ctx, types.RoleFilter{
        Actor: actor,
        Scope: scopeFilter,
        Pagination: types.Pagination{Limit: 100},
    })
    if err != nil {
        return nil, err
    }
    return page.Roles, nil
}
```

#### Include System Roles

```go
page, err := svc.Queries().RoleList.Query(ctx, types.RoleFilter{
    Actor:         actor,
    Scope:         scopeFilter,
    IncludeSystem: true,  // Include built-in roles
    Pagination:    types.Pagination{Limit: 100},
})
```

#### Search Roles by Keyword

```go
page, err := svc.Queries().RoleList.Query(ctx, types.RoleFilter{
    Actor:      actor,
    Scope:      scopeFilter,
    Keyword:    "manager",  // Searches name and description
    Pagination: types.Pagination{Limit: 50},
})
```

#### Filter by Role Key

```go
page, err := svc.Queries().RoleList.Query(ctx, types.RoleFilter{
    Actor:   actor,
    Scope:   scopeFilter,
    RoleKey: "project_manager",  // Exact match
})
```

### Role Detail Lookups

Fetch a single role by ID:

```go
func getRole(ctx context.Context, svc *service.Service, roleID uuid.UUID, actor types.ActorRef, scopeFilter types.ScopeFilter) (*types.RoleDefinition, error) {
    return svc.Queries().RoleDetail.Query(ctx, query.RoleDetailInput{
        RoleID: roleID,
        Actor:  actor,
        Scope:  scopeFilter,
    })
}
```

The `RoleDefinition` contains full role metadata:

```go
type RoleDefinition struct {
    ID          uuid.UUID
    Name        string             // "Project Manager"
    Order       int                // Display order
    Description string             // "Manages project resources"
    RoleKey     string             // "project_manager"
    Permissions []string           // ["projects.read", "projects.write"]
    Metadata    map[string]any     // Custom data
    IsSystem    bool               // Built-in role?
    Scope       ScopeFilter        // Tenant/org scope
    CreatedAt   time.Time
    UpdatedAt   time.Time
    CreatedBy   uuid.UUID
    UpdatedBy   uuid.UUID
}
```

### Assignment Queries

Query role assignments (who has which roles):

```go
type RoleAssignmentFilter struct {
    Actor   ActorRef
    Scope   ScopeFilter
    UserID  uuid.UUID      // Filter by user
    RoleID  uuid.UUID      // Filter by role
    UserIDs []uuid.UUID    // Multiple users
    RoleIDs []uuid.UUID    // Multiple roles
}
```

#### Get All Users with a Role

```go
func getUsersWithRole(ctx context.Context, svc *service.Service, roleID uuid.UUID, actor types.ActorRef, scopeFilter types.ScopeFilter) ([]types.RoleAssignment, error) {
    return svc.Queries().RoleAssignments.Query(ctx, types.RoleAssignmentFilter{
        Actor:  actor,
        Scope:  scopeFilter,
        RoleID: roleID,
    })
}
```

#### Get All Roles for a User

```go
func getUserRoles(ctx context.Context, svc *service.Service, userID uuid.UUID, actor types.ActorRef, scopeFilter types.ScopeFilter) ([]types.RoleAssignment, error) {
    return svc.Queries().RoleAssignments.Query(ctx, types.RoleAssignmentFilter{
        Actor:  actor,
        Scope:  scopeFilter,
        UserID: userID,
    })
}
```

#### Get Assignments for Multiple Users

```go
assignments, err := svc.Queries().RoleAssignments.Query(ctx, types.RoleAssignmentFilter{
    Actor: actor,
    Scope: scopeFilter,
    UserIDs: []uuid.UUID{
        userID1,
        userID2,
        userID3,
    },
})
// Returns all role assignments for the specified users
```

The `RoleAssignment` structure:

```go
type RoleAssignment struct {
    UserID     uuid.UUID
    RoleID     uuid.UUID
    RoleName   string        // Denormalized for display
    Scope      ScopeFilter
    AssignedAt time.Time
    AssignedBy uuid.UUID
}
```

---

## Activity Queries

### Feed Queries

Fetch paginated activity logs:

```go
type ActivityFilter struct {
    Actor      ActorRef
    Scope      ScopeFilter
    UserID     uuid.UUID      // Activities for specific user
    ActorID    uuid.UUID      // Activities by specific actor
    Verbs      []string       // Filter by verb
    ObjectType string         // Filter by object type
    ObjectID   string         // Filter by object ID
    Channel    string         // Filter by channel
    Channels   []string       // Filter by channel allowlist (IN)
    ChannelDenylist []string  // Exclude channels after allow filtering
    Since      *time.Time     // Start time
    Until      *time.Time     // End time
    Pagination Pagination
    Keyword    string         // Search
}
```

#### Basic Activity Feed

```go
func getActivityFeed(ctx context.Context, svc *service.Service, actor types.ActorRef, scopeFilter types.ScopeFilter) ([]types.ActivityRecord, error) {
    page, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
        Actor: actor,
        Scope: scopeFilter,
        Pagination: types.Pagination{
            Limit:  50,
            Offset: 0,
        },
    })
    if err != nil {
        return nil, err
    }
    return page.Records, nil
}
```

#### User-Specific Activity

```go
// Get all activity for a specific user
page, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor:  actor,
    Scope:  scopeFilter,
    UserID: targetUserID,  // Activities where this user is the subject
    Pagination: types.Pagination{Limit: 50},
})
```

#### Activity by Actor

```go
// Get all actions performed by a specific actor
page, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor:   actor,
    Scope:   scopeFilter,
    ActorID: adminUserID,  // Actions performed by this user
    Pagination: types.Pagination{Limit: 50},
})
```

### Verb and Channel Filtering

#### Filter by Verbs

```go
// Get login-related activity
page, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor: actor,
    Scope: scopeFilter,
    Verbs: []string{"user.login", "user.logout", "user.login_failed"},
    Pagination: types.Pagination{Limit: 100},
})
```

#### Filter by Channel

```go
// Get all settings-related activity
page, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor:   actor,
    Scope:   scopeFilter,
    Channel: "settings",
    Pagination: types.Pagination{Limit: 50},
})
```

#### Filter by Object Type and ID

```go
// Get all activity for a specific project
page, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor:      actor,
    Scope:      scopeFilter,
    ObjectType: "project",
    ObjectID:   "project-123",
    Pagination: types.Pagination{Limit: 50},
})
```

#### Time-Based Filtering

```go
// Get activity from the last 24 hours
since := time.Now().Add(-24 * time.Hour)
page, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor: actor,
    Scope: scopeFilter,
    Since: &since,
    Pagination: types.Pagination{Limit: 100},
})

// Get activity within a specific range
since := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
until := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
page, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor: actor,
    Scope: scopeFilter,
    Since: &since,
    Until: &until,
    Pagination: types.Pagination{Limit: 100},
})
```

### Stats Aggregations

Get activity counts grouped by verb for dashboard widgets:

```go
type ActivityStatsFilter struct {
    Actor ActorRef
    Scope ScopeFilter
    Since *time.Time
    Until *time.Time
    Verbs []string  // Optional: only count these verbs
}
```

#### Basic Stats

```go
func getActivityStats(ctx context.Context, svc *service.Service, actor types.ActorRef, scopeFilter types.ScopeFilter) (*types.ActivityStats, error) {
    stats, err := svc.Queries().ActivityStats.Query(ctx, types.ActivityStatsFilter{
        Actor: actor,
        Scope: scopeFilter,
    })
    if err != nil {
        return nil, err
    }
    return &stats, nil
}
```

#### Stats for Last 7 Days

```go
since := time.Now().Add(-7 * 24 * time.Hour)
stats, err := svc.Queries().ActivityStats.Query(ctx, types.ActivityStatsFilter{
    Actor: actor,
    Scope: scopeFilter,
    Since: &since,
})

fmt.Printf("Total activities: %d\n", stats.Total)
for verb, count := range stats.ByVerb {
    fmt.Printf("  %s: %d\n", verb, count)
}
// Output:
// Total activities: 1234
//   user.login: 500
//   user.update: 300
//   role.assign: 150
//   ...
```

#### Stats for Specific Verbs

```go
stats, err := svc.Queries().ActivityStats.Query(ctx, types.ActivityStatsFilter{
    Actor: actor,
    Scope: scopeFilter,
    Verbs: []string{"user.login", "user.logout"},  // Only count these
})
```

---

## Profile and Preference Queries

### Profile Queries

Fetch user profile data:

```go
func getProfile(ctx context.Context, svc *service.Service, userID uuid.UUID, actor types.ActorRef, scopeFilter types.ScopeFilter) (*types.UserProfile, error) {
    return svc.Queries().ProfileDetail.Query(ctx, query.ProfileQueryInput{
        UserID: userID,
        Actor:  actor,
        Scope:  scopeFilter,
    })
}
```

### Preference Queries

Get effective preferences with optional filtering:

```go
func getPreferences(ctx context.Context, svc *service.Service, userID uuid.UUID, actor types.ActorRef, scopeFilter types.ScopeFilter) (map[string]any, error) {
    snapshot, err := svc.Queries().Preferences.Query(ctx, query.PreferenceQueryInput{
        UserID: userID,
        Actor:  actor,
        Scope:  scopeFilter,
    })
    if err != nil {
        return nil, err
    }
    return snapshot.Effective, nil
}

// Get specific keys only
snapshot, err := svc.Queries().Preferences.Query(ctx, query.PreferenceQueryInput{
    UserID: userID,
    Keys:   []string{"theme", "notifications"},
    Actor:  actor,
    Scope:  scopeFilter,
})
```

See [GUIDE_PROFILES_PREFERENCES.md](GUIDE_PROFILES_PREFERENCES.md) for detailed preference query patterns including traces and inheritance.

---

## Pagination Patterns and Best Practices

### Consistent Pagination Structure

All list queries return a similar page structure:

```go
type Page struct {
    Items      []T   // The items for this page
    Total      int   // Total matching items
    NextOffset int   // Offset for next page
    HasMore    bool  // More pages available?
}
```

### Pagination Limits

Default and maximum limits are enforced:

| Query | Default Limit | Max Limit |
|-------|---------------|-----------|
| UserInventory | 50 | 200 |
| RoleList | 50 | 200 |
| ActivityFeed | 50 | 200 |

```go
// These are normalized automatically:
filter.Pagination.Limit = 0    // → 50 (default)
filter.Pagination.Limit = 500  // → 200 (max)
filter.Pagination.Offset = -1  // → 0
```

### Efficient Pagination Helper

```go
// Generic pagination helper
func paginate[T any, F any](
    ctx context.Context,
    queryFn func(context.Context, F) (Page[T], error),
    baseFilter F,
    setOffset func(F, int) F,
    limit int,
) ([]T, error) {
    var all []T
    offset := 0

    for {
        filter := setOffset(baseFilter, offset)
        page, err := queryFn(ctx, filter)
        if err != nil {
            return nil, err
        }

        all = append(all, page.Items...)

        if !page.HasMore {
            break
        }
        offset = page.NextOffset
    }

    return all, nil
}
```

### Cursor-Based Alternative

For very large datasets, consider implementing cursor-based pagination at the repository level:

```go
// Example cursor-based filter extension
type CursorFilter struct {
    Cursor string  // Opaque cursor from previous response
    Limit  int
}

// Repository returns cursor for next page
type CursorPage struct {
    Items      []T
    NextCursor string  // Empty if no more pages
}
```

---

## Building Admin Search Interfaces

### Combined Search Endpoint

```go
type AdminSearchRequest struct {
    Query     string   `json:"query"`
    Statuses  []string `json:"statuses"`
    Roles     []string `json:"roles"`
    Page      int      `json:"page"`
    PageSize  int      `json:"page_size"`
}

type AdminSearchResponse struct {
    Users      []UserDTO `json:"users"`
    Total      int       `json:"total"`
    Page       int       `json:"page"`
    PageSize   int       `json:"page_size"`
    TotalPages int       `json:"total_pages"`
}

func handleAdminSearch(w http.ResponseWriter, r *http.Request) {
    var req AdminSearchRequest
    json.NewDecoder(r.Body).Decode(&req)

    // Normalize pagination
    if req.PageSize <= 0 {
        req.PageSize = 20
    }
    if req.PageSize > 100 {
        req.PageSize = 100
    }
    if req.Page < 1 {
        req.Page = 1
    }

    // Convert statuses
    statuses := make([]types.LifecycleState, 0, len(req.Statuses))
    for _, s := range req.Statuses {
        statuses = append(statuses, types.LifecycleState(s))
    }

    // Build filter
    filter := types.UserInventoryFilter{
        Actor:    getActorFromRequest(r),
        Scope:    getScopeFromRequest(r),
        Keyword:  req.Query,
        Statuses: statuses,
        Pagination: types.Pagination{
            Limit:  req.PageSize,
            Offset: (req.Page - 1) * req.PageSize,
        },
    }

    // Handle role filter (single role supported by filter)
    if len(req.Roles) > 0 {
        filter.Role = req.Roles[0]
    }

    // Execute query
    page, err := svc.Queries().UserInventory.Query(r.Context(), filter)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    // Build response
    resp := AdminSearchResponse{
        Users:      toUserDTOs(page.Users),
        Total:      page.Total,
        Page:       req.Page,
        PageSize:   req.PageSize,
        TotalPages: (page.Total + req.PageSize - 1) / req.PageSize,
    }

    json.NewEncoder(w).Encode(resp)
}
```

### User Detail Endpoint with Related Data

```go
type UserDetailResponse struct {
    User           UserDTO            `json:"user"`
    Profile        *ProfileDTO        `json:"profile"`
    Roles          []RoleAssignmentDTO `json:"roles"`
    RecentActivity []ActivityDTO      `json:"recent_activity"`
}

func handleUserDetail(w http.ResponseWriter, r *http.Request) {
    userID := uuid.MustParse(chi.URLParam(r, "id"))
    actor := getActorFromRequest(r)
    scopeFilter := getScopeFromRequest(r)

    // Fetch user from inventory
    userPage, err := svc.Queries().UserInventory.Query(r.Context(), types.UserInventoryFilter{
        Actor:   actor,
        Scope:   scopeFilter,
        UserIDs: []uuid.UUID{userID},
    })
    if err != nil || len(userPage.Users) == 0 {
        http.Error(w, "User not found", http.StatusNotFound)
        return
    }

    // Fetch profile
    profile, _ := svc.Queries().ProfileDetail.Query(r.Context(), query.ProfileQueryInput{
        UserID: userID,
        Actor:  actor,
        Scope:  scopeFilter,
    })

    // Fetch role assignments
    assignments, _ := svc.Queries().RoleAssignments.Query(r.Context(), types.RoleAssignmentFilter{
        Actor:  actor,
        Scope:  scopeFilter,
        UserID: userID,
    })

    // Fetch recent activity
    activityPage, _ := svc.Queries().ActivityFeed.Query(r.Context(), types.ActivityFilter{
        Actor:      actor,
        Scope:      scopeFilter,
        UserID:     userID,
        Pagination: types.Pagination{Limit: 10},
    })

    resp := UserDetailResponse{
        User:           toUserDTO(userPage.Users[0]),
        Profile:        toProfileDTO(profile),
        Roles:          toRoleAssignmentDTOs(assignments),
        RecentActivity: toActivityDTOs(activityPage.Records),
    }

    json.NewEncoder(w).Encode(resp)
}
```

### Activity Dashboard Widget

```go
type DashboardStats struct {
    TotalUsers     int            `json:"total_users"`
    ActiveUsers    int            `json:"active_users"`
    ActivityStats  map[string]int `json:"activity_stats"`
    RecentActivity []ActivityDTO  `json:"recent_activity"`
}

func handleDashboardStats(w http.ResponseWriter, r *http.Request) {
    actor := getActorFromRequest(r)
    scopeFilter := getScopeFromRequest(r)
    since := time.Now().Add(-7 * 24 * time.Hour)

    // Get user counts
    allUsers, _ := svc.Queries().UserInventory.Query(r.Context(), types.UserInventoryFilter{
        Actor: actor,
        Scope: scopeFilter,
        Pagination: types.Pagination{Limit: 1},  // Just need total
    })

    activeUsers, _ := svc.Queries().UserInventory.Query(r.Context(), types.UserInventoryFilter{
        Actor:    actor,
        Scope:    scopeFilter,
        Statuses: []types.LifecycleState{types.LifecycleStateActive},
        Pagination: types.Pagination{Limit: 1},
    })

    // Get activity stats
    stats, _ := svc.Queries().ActivityStats.Query(r.Context(), types.ActivityStatsFilter{
        Actor: actor,
        Scope: scopeFilter,
        Since: &since,
    })

    // Get recent activity
    activity, _ := svc.Queries().ActivityFeed.Query(r.Context(), types.ActivityFilter{
        Actor:      actor,
        Scope:      scopeFilter,
        Pagination: types.Pagination{Limit: 5},
    })

    resp := DashboardStats{
        TotalUsers:     allUsers.Total,
        ActiveUsers:    activeUsers.Total,
        ActivityStats:  stats.ByVerb,
        RecentActivity: toActivityDTOs(activity.Records),
    }

    json.NewEncoder(w).Encode(resp)
}
```

---

## Error Handling

Common query errors:

```go
import "github.com/goliatone/go-users/pkg/types"

// Repository errors
types.ErrMissingInventoryRepository  // UserInventoryRepository not configured
types.ErrMissingActivityRepository   // ActivityRepository not configured
types.ErrMissingRoleRegistry         // RoleRegistry not configured
types.ErrMissingProfileRepository    // ProfileRepository not configured
types.ErrMissingPreferenceResolver   // PreferenceResolver not configured

// Validation errors
types.ErrActorRequired               // Actor.ID is required
query.errRoleIDRequired              // RoleID required for detail query

// Authorization errors (from scope guard)
types.ErrUnauthorized                // Actor not authorized for action
```

Error handling example:

```go
func handleQuery(ctx context.Context, svc *service.Service, filter types.UserInventoryFilter) (*types.UserInventoryPage, error) {
    page, err := svc.Queries().UserInventory.Query(ctx, filter)
    if err != nil {
        switch {
        case errors.Is(err, types.ErrActorRequired):
            return nil, fmt.Errorf("authentication required")
        case errors.Is(err, types.ErrUnauthorized):
            return nil, fmt.Errorf("access denied")
        case errors.Is(err, types.ErrMissingInventoryRepository):
            return nil, fmt.Errorf("service unavailable")
        default:
            return nil, fmt.Errorf("query failed: %w", err)
        }
    }
    return &page, nil
}
```

---

## Next Steps

- [GUIDE_CRUD_INTEGRATION.md](GUIDE_CRUD_INTEGRATION.md) - REST API patterns with go-crud
- [GUIDE_ACTIVITY.md](GUIDE_ACTIVITY.md) - Activity logging in depth
- [GUIDE_ROLES.md](GUIDE_ROLES.md) - Role management details
- [GUIDE_PROFILES_PREFERENCES.md](GUIDE_PROFILES_PREFERENCES.md) - Profile and preference queries
