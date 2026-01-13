# Repositories Guide

This guide covers the repository architecture in `go-users` and how to implement, customize, and extend repository interfaces. Learn about the built-in Bun implementations, the go-auth adapter, and patterns for testing with in-memory repositories.

## Table of Contents

- [Overview](#overview)
- [Repository Architecture](#repository-architecture)
- [Core Interfaces](#core-interfaces)
  - [AuthRepository](#authrepository)
  - [RoleRegistry](#roleregistry)
  - [ActivityRepository and ActivitySink](#activityrepository-and-activitysink)
  - [UserInventoryRepository](#userinventoryrepository)
  - [ProfileRepository](#profilerepository)
  - [PreferenceRepository](#preferencerepository)
- [Built-in Bun Implementations](#built-in-bun-implementations)
  - [Activity Repository](#activity-repository)
  - [Role Registry](#role-registry)
  - [Preference Repository](#preference-repository)
- [go-auth Adapter for AuthRepository](#go-auth-adapter-for-authrepository)
- [Implementing Custom Repositories](#implementing-custom-repositories)
- [Repository Decorators](#repository-decorators)
- [Testing with In-Memory Repositories](#testing-with-in-memory-repositories)
- [Next Steps](#next-steps)

---

## Overview

`go-users` follows a clean architecture pattern where repository interfaces define the data access contract, and implementations are plugged in at service construction time. This separation allows:

- **Flexibility**: Swap databases without changing business logic
- **Testability**: Use in-memory implementations for unit tests
- **Composition**: Add decorators for caching, logging, or metrics

```
┌─────────────────────────────────────────────────────────────┐
│                 Repository Architecture                      │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│   ┌──────────────────────────────────────────┐              │
│   │           go-users Service               │              │
│   │                                          │              │
│   │   Commands ─────────┬───────── Queries   │              │
│   └──────────────────────┼───────────────────┘              │
│                          │                                   │
│                          ▼                                   │
│   ┌──────────────────────────────────────────┐              │
│   │         Repository Interfaces            │              │
│   │                                          │              │
│   │  AuthRepository    RoleRegistry          │              │
│   │  ActivitySink      ProfileRepository     │              │
│   │  ActivityRepository PreferenceRepository │              │
│   │  UserInventoryRepository                 │              │
│   └──────────────────────┬───────────────────┘              │
│                          │                                   │
│          ┌───────────────┼───────────────┐                  │
│          ▼               ▼               ▼                  │
│   ┌────────────┐  ┌────────────┐  ┌────────────┐           │
│   │    Bun     │  │  go-auth   │  │  In-Memory │           │
│   │   (SQL)    │  │  Adapter   │  │  (Testing) │           │
│   └────────────┘  └────────────┘  └────────────┘           │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

---

## Repository Architecture

### Interface-Based Design

Each repository is defined as a Go interface in `pkg/types`:

```go
// Repository interfaces are defined in pkg/types/types.go
type AuthRepository interface { ... }
type RoleRegistry interface { ... }
type ActivitySink interface { ... }
type ActivityRepository interface { ... }
type UserInventoryRepository interface { ... }
type ProfileRepository interface { ... }
type PreferenceRepository interface { ... }
```

### Wiring Repositories

Repositories are injected into the service at construction time:

```go
svc := service.New(service.Config{
    AuthRepository:       authRepo,       // Required
    RoleRegistry:         roleRegistry,   // Required
    ActivitySink:         activitySink,   // Required
    InventoryRepository:  inventoryRepo,  // Optional (can derive from AuthRepository)
    ActivityRepository:   activityRepo,   // Optional (can derive from ActivitySink)
    ProfileRepository:    profileRepo,    // Required for profiles
    PreferenceRepository: prefRepo,       // Required for preferences
})
```

---

## Core Interfaces

### AuthRepository

Manages user authentication data and lifecycle state transitions:

```go
type AuthRepository interface {
    // Core CRUD
    GetByID(ctx context.Context, id uuid.UUID) (*AuthUser, error)
    GetByIdentifier(ctx context.Context, identifier string) (*AuthUser, error)
    Create(ctx context.Context, input *AuthUser) (*AuthUser, error)
    Update(ctx context.Context, input *AuthUser) (*AuthUser, error)

    // Lifecycle management
    UpdateStatus(ctx context.Context, actor ActorRef, id uuid.UUID, next LifecycleState, opts ...TransitionOption) (*AuthUser, error)
    AllowedTransitions(ctx context.Context, id uuid.UUID) ([]LifecycleTransition, error)

    // Password management
    ResetPassword(ctx context.Context, id uuid.UUID, passwordHash string) error
}
```

**AuthUser Structure:**

```go
type AuthUser struct {
    ID        uuid.UUID
    Role      string
    Status    LifecycleState
    Email     string
    Username  string
    FirstName string
    LastName  string
    Metadata  map[string]any
    CreatedAt *time.Time
    UpdatedAt *time.Time
    Raw       any  // Original auth provider record
}
```

### RoleRegistry

Manages custom roles and user-role assignments:

```go
type RoleRegistry interface {
    // Role CRUD
    CreateRole(ctx context.Context, input RoleMutation) (*RoleDefinition, error)
    UpdateRole(ctx context.Context, id uuid.UUID, input RoleMutation) (*RoleDefinition, error)
    DeleteRole(ctx context.Context, id uuid.UUID, scope ScopeFilter, actor uuid.UUID) error

    // Role assignments
    AssignRole(ctx context.Context, userID, roleID uuid.UUID, scope ScopeFilter, actor uuid.UUID) error
    UnassignRole(ctx context.Context, userID, roleID uuid.UUID, scope ScopeFilter, actor uuid.UUID) error

    // Queries
    ListRoles(ctx context.Context, filter RoleFilter) (RolePage, error)
    GetRole(ctx context.Context, id uuid.UUID, scope ScopeFilter) (*RoleDefinition, error)
    ListAssignments(ctx context.Context, filter RoleAssignmentFilter) ([]RoleAssignment, error)
}
```

### ActivityRepository and ActivitySink

Activity logging is split into write (sink) and read (repository) interfaces:

```go
// Write interface - minimal for logging
type ActivitySink interface {
    Log(ctx context.Context, record ActivityRecord) error
}

// Read interface - for queries
type ActivityRepository interface {
    ListActivity(ctx context.Context, filter ActivityFilter) (ActivityPage, error)
    ActivityStats(ctx context.Context, filter ActivityStatsFilter) (ActivityStats, error)
}
```

Most implementations (like the Bun repository) implement both interfaces:

```go
var (
    _ types.ActivitySink       = (*Repository)(nil)
    _ types.ActivityRepository = (*Repository)(nil)
)
```

### UserInventoryRepository

Provides search and listing capabilities for admin panels:

```go
type UserInventoryRepository interface {
    ListUsers(ctx context.Context, filter UserInventoryFilter) (UserInventoryPage, error)
}
```

This interface can often be satisfied by the `AuthRepository` implementation:

```go
// In service.New():
invRepo := cfg.InventoryRepository
if invRepo == nil {
    // Try to use AuthRepository if it implements the interface
    if cast, ok := cfg.AuthRepository.(types.UserInventoryRepository); ok {
        invRepo = cast
    }
}
```

### ProfileRepository

Manages user profile data:

```go
type ProfileRepository interface {
    GetProfile(ctx context.Context, userID uuid.UUID, scope ScopeFilter) (*UserProfile, error)
    UpsertProfile(ctx context.Context, profile UserProfile) (*UserProfile, error)
}
```

### PreferenceRepository

Manages scoped user preferences:

```go
type PreferenceRepository interface {
    ListPreferences(ctx context.Context, filter PreferenceFilter) ([]PreferenceRecord, error)
    UpsertPreference(ctx context.Context, record PreferenceRecord) (*PreferenceRecord, error)
    DeletePreference(ctx context.Context, userID uuid.UUID, scope ScopeFilter, level PreferenceLevel, key string) error
}
```

---

## Built-in Bun Implementations

`go-users` provides Bun ORM implementations for most repositories.

### Activity Repository

Located in `activity/bun_repository.go`:

```go
import (
    "github.com/goliatone/go-users/activity"
    "github.com/uptrace/bun"
)

func setupActivityRepo(db *bun.DB) (*activity.Repository, error) {
    return activity.NewRepository(activity.RepositoryConfig{
        DB:    db,
        Clock: types.SystemClock{},  // Optional
        IDGen: types.UUIDGenerator{}, // Optional
    })
}
```

**Features:**

- Implements both `ActivitySink` and `ActivityRepository`
- Auto-generates UUIDs and timestamps
- Supports all filter options (verbs, channels, time ranges)
- Provides aggregation stats by verb

**Database Model:**

```go
type LogEntry struct {
    bun.BaseModel `bun:"table:user_activity"`

    ID         uuid.UUID      `bun:"id,pk,type:uuid"`
    UserID     uuid.UUID      `bun:"user_id,type:uuid"`
    ActorID    uuid.UUID      `bun:"actor_id,type:uuid"`
    TenantID   uuid.UUID      `bun:"tenant_id,type:uuid"`
    OrgID      uuid.UUID      `bun:"org_id,type:uuid"`
    Verb       string         `bun:"verb"`
    ObjectType string         `bun:"object_type"`
    ObjectID   string         `bun:"object_id"`
    Channel    string         `bun:"channel"`
    IP         string         `bun:"ip"`
    Data       map[string]any `bun:"data,type:jsonb"`
    CreatedAt  time.Time      `bun:"created_at"`
}
```

### Role Registry

Located in `registry/bun_registry.go`:

```go
import (
    "github.com/goliatone/go-users/registry"
    "github.com/uptrace/bun"
)

func setupRoleRegistry(db *bun.DB, hooks types.Hooks) (*registry.RoleRegistry, error) {
    return registry.NewRoleRegistry(registry.RoleRegistryConfig{
        DB:    db,
        Clock: types.SystemClock{},
        Hooks: hooks,  // Optional: for AfterRoleChange callbacks
        Logger: logger,
        IDGenerator: types.UUIDGenerator{},
    })
}
```

**Features:**

- Full CRUD for custom roles
- Role assignment management
- Scope-aware queries (tenant/org isolation)
- Emits role events via hooks
- Prevents deletion of system roles

**Database Models:**

```go
// Custom role model
type CustomRole struct {
    bun.BaseModel `bun:"table:custom_roles"`

    ID          uuid.UUID      `bun:"id,pk,type:uuid"`
    Name        string         `bun:"name"`
    Order       int            `bun:"order"`
    Description string         `bun:"description"`
    RoleKey     string         `bun:"role_key"`
    Permissions []string       `bun:"permissions,array"`
    Metadata    map[string]any `bun:"metadata,type:jsonb"`
    IsSystem    bool           `bun:"is_system"`
    TenantID    uuid.UUID      `bun:"tenant_id,type:uuid"`
    OrgID       uuid.UUID      `bun:"org_id,type:uuid"`
    CreatedAt   time.Time      `bun:"created_at"`
    UpdatedAt   time.Time      `bun:"updated_at"`
    CreatedBy   uuid.UUID      `bun:"created_by,type:uuid"`
    UpdatedBy   uuid.UUID      `bun:"updated_by,type:uuid"`
}

// Role assignment model
type RoleAssignment struct {
    bun.BaseModel `bun:"table:custom_role_assignments"`

    UserID     uuid.UUID `bun:"user_id,pk,type:uuid"`
    RoleID     uuid.UUID `bun:"role_id,pk,type:uuid"`
    TenantID   uuid.UUID `bun:"tenant_id,pk,type:uuid"`
    OrgID      uuid.UUID `bun:"org_id,pk,type:uuid"`
    AssignedAt time.Time `bun:"assigned_at"`
    AssignedBy uuid.UUID `bun:"assigned_by,type:uuid"`
}
```

### Preference Repository

Located in `preferences/bun_repository.go`:

```go
import (
    "github.com/goliatone/go-users/preferences"
    "github.com/uptrace/bun"
)

func setupPreferenceRepo(db *bun.DB) (*preferences.Repository, error) {
    return preferences.NewRepository(preferences.RepositoryConfig{
        DB:    db,
        Clock: types.SystemClock{},
        IDGen: types.UUIDGenerator{},
    })
}

// Enable caching (optional)
func setupCachedPreferenceRepo(db *bun.DB) (*preferences.Repository, error) {
    return preferences.NewRepository(preferences.RepositoryConfig{
        DB:    db,
        Clock: types.SystemClock{},
        IDGen: types.UUIDGenerator{},
    }, preferences.WithCache(true))
}
```

**Features:**

- Scope-level storage (system, tenant, org, user)
- Upsert semantics (insert or update)
- Version tracking for optimistic concurrency
- Case-insensitive key matching

**Database Model:**

```go
type Record struct {
    bun.BaseModel `bun:"table:user_preferences"`

    ID         uuid.UUID      `bun:"id,pk,type:uuid"`
    UserID     uuid.UUID      `bun:"user_id,type:uuid"`
    TenantID   uuid.UUID      `bun:"tenant_id,type:uuid"`
    OrgID      uuid.UUID      `bun:"org_id,type:uuid"`
    ScopeLevel string         `bun:"scope_level"`
    Key        string         `bun:"key"`
    Value      map[string]any `bun:"value,type:jsonb"`
    Version    int            `bun:"version"`
    CreatedAt  time.Time      `bun:"created_at"`
    CreatedBy  uuid.UUID      `bun:"created_by,type:uuid"`
    UpdatedAt  time.Time      `bun:"updated_at"`
    UpdatedBy  uuid.UUID      `bun:"updated_by,type:uuid"`
}
```

---

## go-auth Adapter for AuthRepository

The `adapter/goauth` package provides an adapter to use `go-auth` repositories with `go-users`:

```go
import (
    auth "github.com/goliatone/go-auth"
    "github.com/goliatone/go-users/adapter/goauth"
    "github.com/goliatone/go-users/pkg/types"
)

func setupAuthAdapter(authUsers auth.Users) types.AuthRepository {
    return goauth.NewUsersAdapter(authUsers,
        // Optional: custom transition policy
        goauth.WithPolicy(types.DefaultTransitionPolicy()),
    )
}
```

**Features:**

- Wraps `go-auth` Users repository
- Converts between `go-auth` and `go-users` types
- Integrates with `go-auth` state machine for transitions
- Supports custom transition policies

**Usage Example:**

```go
import (
    auth "github.com/goliatone/go-auth"
    bunauth "github.com/goliatone/go-auth/bun"
    "github.com/goliatone/go-users/adapter/goauth"
    "github.com/goliatone/go-users/service"
)

func setupService(db *bun.DB) *service.Service {
    // Create go-auth repository
    authUsers := bunauth.NewUsers(db)

    // Wrap with go-users adapter
    authRepo := goauth.NewUsersAdapter(authUsers)

    return service.New(service.Config{
        AuthRepository: authRepo,
        // ... other config
    })
}
```

**Type Conversion:**

The adapter handles conversion between types:

```go
// go-auth User → go-users AuthUser
func toAuthUser(user *auth.User) *types.AuthUser {
    return &types.AuthUser{
        ID:        user.ID,
        Role:      string(user.Role),
        Status:    types.LifecycleState(user.Status),
        Email:     user.Email,
        Username:  user.Username,
        FirstName: user.FirstName,
        LastName:  user.LastName,
        Metadata:  user.Metadata,
        CreatedAt: user.CreatedAt,
        UpdatedAt: user.UpdatedAt,
        Raw:       user,  // Preserve original for roundtripping
    }
}
```

---

## Implementing Custom Repositories

### Custom AuthRepository

```go
package myrepo

import (
    "context"
    "github.com/goliatone/go-users/pkg/types"
    "github.com/google/uuid"
)

type MyAuthRepository struct {
    // Your dependencies (Redis, MongoDB, external API, etc.)
    client *MyClient
    policy types.TransitionPolicy
}

func NewMyAuthRepository(client *MyClient) *MyAuthRepository {
    return &MyAuthRepository{
        client: client,
        policy: types.DefaultTransitionPolicy(),
    }
}

// Implement types.AuthRepository interface

func (r *MyAuthRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.AuthUser, error) {
    // Fetch from your data store
    record, err := r.client.GetUser(ctx, id.String())
    if err != nil {
        return nil, err
    }
    return r.toAuthUser(record), nil
}

func (r *MyAuthRepository) GetByIdentifier(ctx context.Context, identifier string) (*types.AuthUser, error) {
    // Search by email/username
    record, err := r.client.FindUser(ctx, identifier)
    if err != nil {
        return nil, err
    }
    return r.toAuthUser(record), nil
}

func (r *MyAuthRepository) Create(ctx context.Context, input *types.AuthUser) (*types.AuthUser, error) {
    record := r.fromAuthUser(input)
    created, err := r.client.CreateUser(ctx, record)
    if err != nil {
        return nil, err
    }
    return r.toAuthUser(created), nil
}

func (r *MyAuthRepository) Update(ctx context.Context, input *types.AuthUser) (*types.AuthUser, error) {
    record := r.fromAuthUser(input)
    updated, err := r.client.UpdateUser(ctx, record)
    if err != nil {
        return nil, err
    }
    return r.toAuthUser(updated), nil
}

func (r *MyAuthRepository) UpdateStatus(ctx context.Context, actor types.ActorRef, id uuid.UUID, next types.LifecycleState, opts ...types.TransitionOption) (*types.AuthUser, error) {
    // Fetch current user
    user, err := r.GetByID(ctx, id)
    if err != nil {
        return nil, err
    }

    // Validate transition
    config := r.configFromOptions(opts...)
    if r.policy != nil && !config.Force {
        if err := r.policy.Validate(user.Status, next); err != nil {
            return nil, err
        }
    }

    // Update status
    user.Status = next
    return r.Update(ctx, user)
}

func (r *MyAuthRepository) AllowedTransitions(ctx context.Context, id uuid.UUID) ([]types.LifecycleTransition, error) {
    user, err := r.GetByID(ctx, id)
    if err != nil {
        return nil, err
    }

    if r.policy == nil {
        return nil, nil
    }

    targets := r.policy.AllowedTargets(user.Status)
    transitions := make([]types.LifecycleTransition, 0, len(targets))
    for _, target := range targets {
        transitions = append(transitions, types.LifecycleTransition{
            From: user.Status,
            To:   target,
        })
    }
    return transitions, nil
}

func (r *MyAuthRepository) ResetPassword(ctx context.Context, id uuid.UUID, passwordHash string) error {
    return r.client.UpdatePassword(ctx, id.String(), passwordHash)
}

// Helper methods for type conversion
func (r *MyAuthRepository) toAuthUser(record *MyRecord) *types.AuthUser {
    // Convert your record type to AuthUser
}

func (r *MyAuthRepository) fromAuthUser(user *types.AuthUser) *MyRecord {
    // Convert AuthUser to your record type
}
```

### Custom ActivitySink

```go
package myrepo

import (
    "context"
    "encoding/json"
    "github.com/goliatone/go-users/pkg/types"
)

// ElasticsearchActivitySink sends activity to Elasticsearch
type ElasticsearchActivitySink struct {
    client *elasticsearch.Client
    index  string
}

func NewElasticsearchActivitySink(client *elasticsearch.Client, index string) *ElasticsearchActivitySink {
    return &ElasticsearchActivitySink{
        client: client,
        index:  index,
    }
}

func (s *ElasticsearchActivitySink) Log(ctx context.Context, record types.ActivityRecord) error {
    doc := map[string]any{
        "id":          record.ID.String(),
        "user_id":     record.UserID.String(),
        "actor_id":    record.ActorID.String(),
        "tenant_id":   record.TenantID.String(),
        "org_id":      record.OrgID.String(),
        "verb":        record.Verb,
        "object_type": record.ObjectType,
        "object_id":   record.ObjectID,
        "channel":     record.Channel,
        "ip":          record.IP,
        "data":        record.Data,
        "occurred_at": record.OccurredAt,
    }

    body, _ := json.Marshal(doc)
    _, err := s.client.Index(s.index, bytes.NewReader(body))
    return err
}
```

---

## Repository Decorators

### Caching Decorator

```go
package decorators

import (
    "context"
    "time"
    "github.com/goliatone/go-users/pkg/types"
    "github.com/google/uuid"
)

type CachingAuthRepository struct {
    inner types.AuthRepository
    cache Cache
    ttl   time.Duration
}

func NewCachingAuthRepository(inner types.AuthRepository, cache Cache, ttl time.Duration) *CachingAuthRepository {
    return &CachingAuthRepository{
        inner: inner,
        cache: cache,
        ttl:   ttl,
    }
}

func (r *CachingAuthRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.AuthUser, error) {
    // Try cache first
    key := "user:" + id.String()
    if cached, ok := r.cache.Get(key); ok {
        return cached.(*types.AuthUser), nil
    }

    // Fetch from inner repository
    user, err := r.inner.GetByID(ctx, id)
    if err != nil {
        return nil, err
    }

    // Cache the result
    r.cache.Set(key, user, r.ttl)
    return user, nil
}

func (r *CachingAuthRepository) Update(ctx context.Context, input *types.AuthUser) (*types.AuthUser, error) {
    // Update in inner repository
    updated, err := r.inner.Update(ctx, input)
    if err != nil {
        return nil, err
    }

    // Invalidate cache
    key := "user:" + input.ID.String()
    r.cache.Delete(key)

    return updated, nil
}

// Implement remaining methods by delegating to inner...
```

### Logging Decorator

```go
package decorators

import (
    "context"
    "log/slog"
    "time"
    "github.com/goliatone/go-users/pkg/types"
    "github.com/google/uuid"
)

type LoggingAuthRepository struct {
    inner  types.AuthRepository
    logger *slog.Logger
}

func NewLoggingAuthRepository(inner types.AuthRepository, logger *slog.Logger) *LoggingAuthRepository {
    return &LoggingAuthRepository{
        inner:  inner,
        logger: logger,
    }
}

func (r *LoggingAuthRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.AuthUser, error) {
    start := time.Now()
    user, err := r.inner.GetByID(ctx, id)
    duration := time.Since(start)

    if err != nil {
        r.logger.Error("GetByID failed",
            "user_id", id,
            "duration", duration,
            "error", err,
        )
    } else {
        r.logger.Debug("GetByID",
            "user_id", id,
            "duration", duration,
        )
    }

    return user, err
}

// Implement remaining methods with similar logging...
```

### Metrics Decorator

```go
package decorators

import (
    "context"
    "time"
    "github.com/goliatone/go-users/pkg/types"
    "github.com/prometheus/client_golang/prometheus"
)

type MetricsAuthRepository struct {
    inner    types.AuthRepository
    duration *prometheus.HistogramVec
    errors   *prometheus.CounterVec
}

func NewMetricsAuthRepository(inner types.AuthRepository) *MetricsAuthRepository {
    return &MetricsAuthRepository{
        inner: inner,
        duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
            Name:    "auth_repository_duration_seconds",
            Help:    "Duration of auth repository operations",
            Buckets: prometheus.DefBuckets,
        }, []string{"operation"}),
        errors: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: "auth_repository_errors_total",
            Help: "Total number of auth repository errors",
        }, []string{"operation"}),
    }
}

func (r *MetricsAuthRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.AuthUser, error) {
    start := time.Now()
    user, err := r.inner.GetByID(ctx, id)
    r.duration.WithLabelValues("GetByID").Observe(time.Since(start).Seconds())

    if err != nil {
        r.errors.WithLabelValues("GetByID").Inc()
    }

    return user, err
}
```

---

## Testing with In-Memory Repositories

`go-users` provides in-memory implementations in `examples/internal/memory` for testing:

### Using In-Memory Repositories

```go
package mytest

import (
    "testing"
    "github.com/goliatone/go-users/examples/internal/memory"
    "github.com/goliatone/go-users/service"
)

func TestMyFeature(t *testing.T) {
    // Create in-memory repositories
    authRepo := memory.NewAuthRepository()
    roleRegistry := memory.NewRoleRegistry()
    activityStore := memory.NewActivityStore()
    profileRepo := memory.NewProfileRepository()
    prefRepo := memory.NewPreferenceRepository()

    // Wire up the service
    svc := service.New(service.Config{
        AuthRepository:       authRepo,
        RoleRegistry:         roleRegistry,
        ActivitySink:         activityStore,
        ActivityRepository:   activityStore,
        ProfileRepository:    profileRepo,
        PreferenceRepository: prefRepo,
    })

    // Run your tests
    // ...
}
```

### In-Memory AuthRepository

```go
// Implements both AuthRepository and UserInventoryRepository
type AuthRepository struct {
    mu    sync.RWMutex
    users map[uuid.UUID]*types.AuthUser
}

func NewAuthRepository() *AuthRepository {
    return &AuthRepository{
        users: make(map[uuid.UUID]*types.AuthUser),
    }
}

// Key features:
// - Thread-safe with RWMutex
// - Auto-generates IDs for new users
// - Supports keyword search in ListUsers
// - Simple lifecycle state transitions
```

### In-Memory RoleRegistry

```go
type RoleRegistry struct {
    mu          sync.RWMutex
    roles       map[uuid.UUID]types.RoleDefinition
    assignments map[uuid.UUID][]types.RoleAssignment
}

func NewRoleRegistry() *RoleRegistry {
    return &RoleRegistry{
        roles:       make(map[uuid.UUID]types.RoleDefinition),
        assignments: make(map[uuid.UUID][]types.RoleAssignment),
    }
}

// Key features:
// - Full CRUD for roles
// - Role assignment tracking
// - Keyword filtering
// - Scope-aware queries
```

### In-Memory ActivityStore

```go
type ActivityStore struct {
    mu      sync.RWMutex
    records []types.ActivityRecord
}

func NewActivityStore() *ActivityStore {
    return &ActivityStore{}
}

// Implements both ActivitySink and ActivityRepository
// Key features:
// - Auto-generates IDs and timestamps
// - LIFO ordering (newest first)
// - Verb and scope filtering
// - Stats aggregation
```

### Test Helper Patterns

```go
package testutil

import (
    "context"
    "testing"
    "github.com/goliatone/go-users/examples/internal/memory"
    "github.com/goliatone/go-users/pkg/types"
    "github.com/google/uuid"
)

// TestFixtures provides pre-configured test data
type TestFixtures struct {
    AuthRepo     *memory.AuthRepository
    RoleRegistry *memory.RoleRegistry
    Activity     *memory.ActivityStore
    Profiles     *memory.ProfileRepository
    Preferences  *memory.PreferenceRepository

    // Pre-created test data
    AdminUser  *types.AuthUser
    NormalUser *types.AuthUser
    AdminRole  *types.RoleDefinition
}

func SetupFixtures(t *testing.T) *TestFixtures {
    t.Helper()

    ctx := context.Background()

    f := &TestFixtures{
        AuthRepo:     memory.NewAuthRepository(),
        RoleRegistry: memory.NewRoleRegistry(),
        Activity:     memory.NewActivityStore(),
        Profiles:     memory.NewProfileRepository(),
        Preferences:  memory.NewPreferenceRepository(),
    }

    // Create admin user
    admin, _ := f.AuthRepo.Create(ctx, &types.AuthUser{
        Email:    "admin@example.com",
        Username: "admin",
        Role:     "admin",
    })
    f.AdminUser = admin

    // Create normal user
    user, _ := f.AuthRepo.Create(ctx, &types.AuthUser{
        Email:    "user@example.com",
        Username: "user",
        Role:     "user",
    })
    f.NormalUser = user

    // Create admin role
    role, _ := f.RoleRegistry.CreateRole(ctx, types.RoleMutation{
        Name:        "Administrator",
        RoleKey:     "admin",
        Permissions: []string{"users.read", "users.write"},
        ActorID:     admin.ID,
    })
    f.AdminRole = role

    return f
}

// Usage in tests:
func TestSomething(t *testing.T) {
    fixtures := SetupFixtures(t)

    svc := service.New(service.Config{
        AuthRepository: fixtures.AuthRepo,
        RoleRegistry:   fixtures.RoleRegistry,
        // ...
    })

    // Test with pre-created fixtures
    user, _ := fixtures.AuthRepo.GetByID(ctx, fixtures.NormalUser.ID)
    // ...
}
```

---

## Next Steps

- [GUIDE_MIGRATIONS.md](GUIDE_MIGRATIONS.md) - Database schema and migrations
- [GUIDE_TESTING.md](GUIDE_TESTING.md) - Comprehensive testing strategies
- [GUIDE_CRUD_INTEGRATION.md](GUIDE_CRUD_INTEGRATION.md) - REST API integration
- [GUIDE_HOOKS.md](GUIDE_HOOKS.md) - Event-driven integrations
