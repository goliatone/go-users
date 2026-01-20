# GUIDE_TESTING.md

This guide covers testing strategies for applications using `go-users`, including unit testing, integration testing with SQLite, mocking dependencies, and testing patterns for key features.

---

## Table of Contents

1. [Overview](#overview)
2. [Testing Architecture](#testing-architecture)
3. [In-Memory Repository Fixtures](#in-memory-repository-fixtures)
4. [Unit Testing Commands and Queries](#unit-testing-commands-and-queries)
5. [Integration Testing with SQLite](#integration-testing-with-sqlite)
6. [Mocking Dependencies](#mocking-dependencies)
7. [Testing Scope Resolution and Authorization](#testing-scope-resolution-and-authorization)
8. [Testing Lifecycle Transitions](#testing-lifecycle-transitions)
9. [Testing Activity Logging](#testing-activity-logging)
10. [Example Test Patterns](#example-test-patterns)

---

## Overview

`go-users` is designed with testability in mind. The package uses interface-based dependencies that allow you to:

- **Swap repositories** with in-memory implementations for fast unit tests
- **Use SQLite** for integration tests that verify real database behavior
- **Mock individual components** for isolated testing
- **Test scope guards** with custom resolvers and policies

### Testing Principles

1. **Prefer in-memory repositories** for unit tests - fast and deterministic
2. **Use SQLite for integration tests** - validates SQL queries and migrations
3. **Test commands and queries independently** - verify business logic in isolation
4. **Test scope enforcement** - ensure multi-tenancy works correctly
5. **Verify hooks are called** - confirm events are emitted properly

---

## Testing Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        Test Layers                              │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│   ┌─────────────────┐                                          │
│   │ Unit Tests      │  • In-memory repositories                │
│   │                 │  • Fake/mock dependencies                 │
│   │                 │  • Fast execution (~ms)                   │
│   │                 │  • Test business logic                    │
│   └─────────────────┘                                          │
│           │                                                     │
│           ▼                                                     │
│   ┌─────────────────┐                                          │
│   │ Integration     │  • SQLite :memory: database              │
│   │ Tests           │  • Real repository implementations        │
│   │                 │  • Medium execution (~100ms)              │
│   │                 │  • Test SQL queries and migrations        │
│   └─────────────────┘                                          │
│           │                                                     │
│           ▼                                                     │
│   ┌─────────────────┐                                          │
│   │ E2E Tests       │  • PostgreSQL database                   │
│   │ (optional)      │  • Full service wiring                   │
│   │                 │  • Slower execution (~1s+)               │
│   │                 │  • Test production scenarios              │
│   └─────────────────┘                                          │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

---

## In-Memory Repository Fixtures

The `examples/internal/memory` package provides production-ready in-memory implementations for all repository interfaces.

### Available Fixtures

```go
import "github.com/goliatone/go-users/examples/internal/memory"

// Auth and inventory repository
authRepo := memory.NewAuthRepository()

// Role registry
roleRegistry := memory.NewRoleRegistry()

// Activity sink and repository
activityStore := memory.NewActivityStore()

// Profile repository
profileRepo := memory.NewProfileRepository()

// Preference repository
preferenceRepo := memory.NewPreferenceRepository()
```

### AuthRepository

```go
// memory.AuthRepository implements:
// - types.AuthRepository
// - types.UserInventoryRepository

type AuthRepository struct {
    mu    sync.RWMutex
    users map[uuid.UUID]*types.AuthUser
}

func NewAuthRepository() *AuthRepository {
    return &AuthRepository{
        users: make(map[uuid.UUID]*types.AuthUser),
    }
}
```

**Features:**
- Thread-safe with read/write mutex
- Implements `GetByID`, `GetByIdentifier`, `Create`, `Update`
- Supports lifecycle state transitions via `UpdateStatus`
- Pagination support in `ListUsers`
- Keyword filtering in `ListUsers`

### Using In-Memory Repositories in Tests

```go
func TestUserWorkflow(t *testing.T) {
    ctx := context.Background()

    // Create all in-memory repositories
    authRepo := memory.NewAuthRepository()
    roleRegistry := memory.NewRoleRegistry()
    activityStore := memory.NewActivityStore()
    profileRepo := memory.NewProfileRepository()
    preferenceRepo := memory.NewPreferenceRepository()

    // Wire the service
    svc := users.New(users.Config{
        AuthRepository:       authRepo,
        InventoryRepository:  authRepo, // Same instance implements both
        RoleRegistry:         roleRegistry,
        ActivitySink:         activityStore,
        ActivityRepository:   activityStore, // Same instance implements both
        ProfileRepository:    profileRepo,
        PreferenceRepository: preferenceRepo,
        Logger:               types.NopLogger{},
    })

    // Verify service is ready
    require.NoError(t, svc.HealthCheck(ctx))

    // Run your tests...
}
```

---

## Unit Testing Commands and Queries

### Testing Commands

Commands are tested by providing fake repositories and verifying side effects.

```go
func TestUserLifecycleTransitionCommand_PolicyViolation(t *testing.T) {
    userID := uuid.New()
    repo := newFakeAuthRepo()
    repo.users[userID] = &types.AuthUser{
        ID:     userID,
        Status: types.LifecycleStateActive,
    }

    cmd := command.NewUserLifecycleTransitionCommand(command.LifecycleCommandConfig{
        Repository: repo,
        Policy:     types.DefaultTransitionPolicy(),
    })

    err := cmd.Execute(context.Background(), command.UserLifecycleTransitionInput{
        UserID: userID,
        Target: types.LifecycleStatePending, // Invalid: active -> pending not allowed
        Actor:  types.ActorRef{ID: uuid.New(), Type: "admin"},
    })

    require.ErrorIs(t, err, types.ErrTransitionNotAllowed)
    require.False(t, repo.transitionCalled, "repo should not receive UpdateStatus when policy rejects")
}
```

### Testing Commands with Hooks

```go
func TestUserLifecycleTransitionCommand_EmitsHook(t *testing.T) {
    userID := uuid.New()
    repo := newFakeAuthRepo()
    repo.users[userID] = &types.AuthUser{
        ID:     userID,
        Status: types.LifecycleStateActive,
    }

    var capturedEvent types.LifecycleEvent
    hooks := types.Hooks{
        AfterLifecycle: func(_ context.Context, event types.LifecycleEvent) {
            capturedEvent = event
        },
    }

    cmd := command.NewUserLifecycleTransitionCommand(command.LifecycleCommandConfig{
        Repository: repo,
        Policy:     types.DefaultTransitionPolicy(),
        Hooks:      hooks,
    })

    err := cmd.Execute(context.Background(), command.UserLifecycleTransitionInput{
        UserID: userID,
        Target: types.LifecycleStateSuspended,
        Actor:  types.ActorRef{ID: uuid.New()},
        Reason: "test cleanup",
    })

    require.NoError(t, err)
    require.Equal(t, types.LifecycleStateActive, capturedEvent.FromState)
    require.Equal(t, types.LifecycleStateSuspended, capturedEvent.ToState)
    require.Equal(t, "test cleanup", capturedEvent.Reason)
}
```

### Testing Commands with Result Capture

```go
func TestUserInviteCommand_ReturnsResult(t *testing.T) {
    repo := newFakeAuthRepo()
    fixedToken := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
    tokenRepo := newMemoryTokenRepo()
    secureLinks := &stubSecureLinkManager{
        token:      "secure-link",
        expiration: time.Hour,
    }

    cmd := command.NewUserInviteCommand(command.InviteCommandConfig{
        Repository:      repo,
        TokenRepository: tokenRepo,
        SecureLinks:     secureLinks,
        IDGen:           fixedIDGenerator{id: fixedToken},
        TokenTTL:        time.Hour,
    })

    result := &command.UserInviteResult{}
    err := cmd.Execute(context.Background(), command.UserInviteInput{
        Email:    "new@example.com",
        Username: "new-user",
        Actor:    types.ActorRef{ID: uuid.New()},
        Result:   result,
    })

    require.NoError(t, err)
    require.Equal(t, "secure-link", result.Token)
    require.Equal(t, "new@example.com", result.User.Email)
    require.Equal(t, types.LifecycleStatePending, result.User.Status)
}
```

### Testing Queries

```go
func TestUserInventoryQuery_NormalizesFilters(t *testing.T) {
    repo := &recordingInventoryRepo{
        page: types.UserInventoryPage{
            Users: []types.AuthUser{{Email: "one@example.com"}},
            Total: 1,
        },
    }
    q := query.NewUserInventoryQuery(repo, types.NopLogger{}, nil)

    filter := types.UserInventoryFilter{
        Actor: types.ActorRef{ID: uuid.New()},
        Scope: types.ScopeFilter{TenantID: uuid.New()},
        Pagination: types.Pagination{
            Limit:  0,   // Should be normalized to default
            Offset: -10, // Should be normalized to 0
        },
    }

    page, err := q.Query(context.Background(), filter)

    require.NoError(t, err)
    require.Greater(t, repo.lastFilter.Pagination.Limit, 0)
    require.Equal(t, 0, repo.lastFilter.Pagination.Offset)
    require.Equal(t, 1, len(page.Users))
}
```

---

## Integration Testing with SQLite

Integration tests use SQLite in-memory databases to verify real database behavior.

### Setup Pattern

```go
import (
    "database/sql"
    "testing"

    _ "github.com/mattn/go-sqlite3"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/sqlitedialect"
)

func newTestDB(t *testing.T) *bun.DB {
    sqlDB, err := sql.Open("sqlite3", ":memory:?cache=shared")
    require.NoError(t, err)
    sqlDB.SetMaxOpenConns(1) // SQLite requires single connection for :memory:

    db := bun.NewDB(sqlDB, sqlitedialect.New())
    t.Cleanup(func() {
        _ = db.Close()
        _ = sqlDB.Close()
    })

    return db
}
```

### Applying Migrations

```go
import (
    "os"
    "strings"
)

func applyMigration(t *testing.T, db *bun.DB, migrationPath string) {
    content, err := os.ReadFile(migrationPath)
    require.NoError(t, err)

    for _, stmt := range splitStatements(string(content)) {
        if strings.TrimSpace(stmt) == "" {
            continue
        }
        _, err := db.Exec(stmt)
        require.NoError(t, err)
    }
}

func splitStatements(sql string) []string {
    lines := strings.Split(sql, "\n")
    var builder strings.Builder
    var statements []string

    for _, line := range lines {
        line = strings.TrimSpace(line)
        if line == "" || strings.HasPrefix(line, "--") {
            continue
        }
        builder.WriteString(line)
        if strings.HasSuffix(line, ";") {
            stmt := strings.TrimSuffix(builder.String(), ";")
            statements = append(statements, strings.TrimSpace(stmt))
            builder.Reset()
        } else {
            builder.WriteString(" ")
        }
    }
    if builder.Len() > 0 {
        statements = append(statements, strings.TrimSpace(builder.String()))
    }
    return statements
}
```

### Full Integration Test Example

```go
func TestActivityRepository_Integration(t *testing.T) {
    ctx := context.Background()
    db := newTestDB(t)

    // Apply the activity migration
    applyMigration(t, db, "../data/sql/migrations/sqlite/00004_user_activity.up.sql")

    // Create the repository
    repo, err := activity.NewRepository(activity.RepositoryConfig{DB: db})
    require.NoError(t, err)

    // Log an activity record
    record := types.ActivityRecord{
        UserID:     uuid.New(),
        ActorID:    uuid.New(),
        Verb:       "user.lifecycle.transition",
        ObjectType: "user",
        ObjectID:   uuid.New().String(),
        Channel:    "lifecycle",
        Data: map[string]any{
            "from": "pending",
            "to":   "active",
        },
    }
    err = repo.Log(ctx, record)
    require.NoError(t, err)

    // Query the activity feed
    page, err := repo.ListActivity(ctx, types.ActivityFilter{
        Verbs:      []string{"user.lifecycle.transition"},
        Pagination: types.Pagination{Limit: 10},
    })
    require.NoError(t, err)
    require.Len(t, page.Records, 1)
    require.Equal(t, "user.lifecycle.transition", page.Records[0].Verb)
    require.Equal(t, "active", page.Records[0].Data["to"])
}
```

### Testing with Multiple Migrations

```go
func TestFullSchema_Integration(t *testing.T) {
    ctx := context.Background()
    db := newTestDB(t)

    // Apply all migrations in order
    migrations := []string{
        "../data/sql/migrations/auth/sqlite/00001_users.up.sql",
        "../data/sql/migrations/auth/sqlite/00002_user_status.up.sql",
        "../data/sql/migrations/sqlite/00003_custom_roles.up.sql",
        "../data/sql/migrations/sqlite/00004_user_activity.up.sql",
        "../data/sql/migrations/sqlite/00005_profiles_preferences.up.sql",
        "../data/sql/migrations/sqlite/00006_custom_roles_metadata.up.sql",
        "../data/sql/migrations/sqlite/00007_custom_roles_order.up.sql",
        "../data/sql/migrations/sqlite/00008_user_tokens.up.sql",
        "../data/sql/migrations/auth/sqlite/00009_user_external_ids.up.sql",
        "../data/sql/migrations/auth_extras/sqlite/00010_social_accounts.up.sql",
        "../data/sql/migrations/auth_extras/sqlite/00011_user_identifiers.up.sql",
    }

    for _, path := range migrations {
        applyMigration(t, db, path)
    }

    // Verify tables exist
    tables := []string{"users", "password_reset", "user_tokens", "custom_roles", "user_custom_roles",
        "user_activity", "user_profiles", "user_preferences", "social_accounts", "user_identifiers"}

    for _, table := range tables {
        var name string
        err := db.QueryRowContext(ctx,
            "SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
        ).Scan(&name)
        require.NoError(t, err, "table %s should exist", table)
    }
}
```

---

## Mocking Dependencies

### Fake Repository Pattern

```go
type fakeAuthRepo struct {
    users                  map[uuid.UUID]*types.AuthUser
    transitionCalled       bool
    lastTransitionReason   string
    lastTransitionMetadata map[string]any
    lastResetUserID        uuid.UUID
    lastResetHash          string
}

func newFakeAuthRepo() *fakeAuthRepo {
    return &fakeAuthRepo{
        users: make(map[uuid.UUID]*types.AuthUser),
    }
}

func (f *fakeAuthRepo) GetByID(_ context.Context, id uuid.UUID) (*types.AuthUser, error) {
    user, ok := f.users[id]
    if !ok {
        return nil, errors.New("not found")
    }
    return user, nil
}

func (f *fakeAuthRepo) Create(_ context.Context, input *types.AuthUser) (*types.AuthUser, error) {
    if input.ID == uuid.Nil {
        input.ID = uuid.New()
    }
    f.users[input.ID] = input
    return input, nil
}

func (f *fakeAuthRepo) UpdateStatus(_ context.Context, actor types.ActorRef, id uuid.UUID, next types.LifecycleState, opts ...types.TransitionOption) (*types.AuthUser, error) {
    f.transitionCalled = true
    // Extract options for verification
    for _, opt := range opts {
        cfg := types.TransitionConfig{}
        opt(&cfg)
        f.lastTransitionReason = cfg.Reason
        f.lastTransitionMetadata = cfg.Metadata
    }

    user, ok := f.users[id]
    if !ok {
        return nil, errors.New("not found")
    }
    user.Status = next
    return user, nil
}

// Implement remaining interface methods...
```

### Recording Activity Sink

```go
type recordingActivitySink struct {
    onLog   func(types.ActivityRecord)
    records []types.ActivityRecord
}

func (r *recordingActivitySink) Log(_ context.Context, record types.ActivityRecord) error {
    r.records = append(r.records, record)
    if r.onLog != nil {
        r.onLog(record)
    }
    return nil
}

// Usage in tests:
func TestCommand_LogsActivity(t *testing.T) {
    var recorded types.ActivityRecord
    sink := &recordingActivitySink{
        onLog: func(r types.ActivityRecord) {
            recorded = r
        },
    }

    cmd := command.NewUserPasswordResetCommand(command.PasswordResetCommandConfig{
        Repository: repo,
        Activity:   sink,
    })

    // Execute command...

    require.Equal(t, "user.password.reset", recorded.Verb)
}
```

### SecureLink Stubs

```go
type stubSecureLinkManager struct {
    token      string
    expiration time.Duration
}

func (s *stubSecureLinkManager) Generate(string, ...types.SecureLinkPayload) (string, error) {
    if s.token == "" {
        return "token", nil
    }
    return s.token, nil
}

func (s *stubSecureLinkManager) Validate(string) (map[string]any, error) {
    return map[string]any{}, nil
}

func (s *stubSecureLinkManager) GetAndValidate(fn func(string) string) (types.SecureLinkPayload, error) {
    if fn != nil {
        _ = fn("")
    }
    return types.SecureLinkPayload{}, nil
}

func (s *stubSecureLinkManager) GetExpiration() time.Duration {
    return s.expiration
}

type memoryTokenRepo struct {
    tokens map[string]*types.UserToken
}

func newMemoryTokenRepo() *memoryTokenRepo {
    return &memoryTokenRepo{tokens: map[string]*types.UserToken{}}
}

func (m *memoryTokenRepo) CreateToken(_ context.Context, token types.UserToken) (*types.UserToken, error) {
    copy := token
    if copy.ID == uuid.Nil {
        copy.ID = uuid.New()
    }
    m.tokens[copy.JTI] = &copy
    return &copy, nil
}

func (m *memoryTokenRepo) GetTokenByJTI(_ context.Context, _ types.UserTokenType, jti string) (*types.UserToken, error) {
    if token, ok := m.tokens[jti]; ok {
        return token, nil
    }
    return nil, errors.New("not found")
}

func (m *memoryTokenRepo) UpdateTokenStatus(_ context.Context, _ types.UserTokenType, jti string, status types.UserTokenStatus, usedAt time.Time) error {
    token, ok := m.tokens[jti]
    if !ok {
        return errors.New("not found")
    }
    token.Status = status
    if !usedAt.IsZero() {
        token.UsedAt = usedAt
    }
    return nil
}
```

### Fixed Time and ID Generators

```go
// Fixed clock for deterministic timestamps
type fixedClock struct {
    t time.Time
}

func (f fixedClock) Now() time.Time {
    return f.t
}

// Fixed ID generator for predictable UUIDs
type fixedIDGenerator struct {
    id uuid.UUID
}

func (f fixedIDGenerator) UUID() uuid.UUID {
    return f.id
}

// Usage:
tokenRepo := newMemoryTokenRepo()
secureLinks := &stubSecureLinkManager{
    token:      "secure-link",
    expiration: time.Hour,
}
cmd := command.NewUserInviteCommand(command.InviteCommandConfig{
    Repository:      repo,
    TokenRepository: tokenRepo,
    SecureLinks:     secureLinks,
    Clock:           fixedClock{t: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)},
    IDGen:           fixedIDGenerator{id: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")},
    TokenTTL:        time.Hour,
})
```

---

## Testing Scope Resolution and Authorization

### Static Scope Resolver

```go
type staticScopeResolver struct {
    scopes map[uuid.UUID]types.ScopeFilter
}

func (r staticScopeResolver) ResolveScope(_ context.Context, actor types.ActorRef, requested types.ScopeFilter) (types.ScopeFilter, error) {
    // If explicit scope requested, use it
    if requested.TenantID != uuid.Nil || requested.OrgID != uuid.Nil {
        return requested, nil
    }
    // Otherwise, resolve from actor
    if resolved, ok := r.scopes[actor.ID]; ok {
        return resolved, nil
    }
    return requested, nil
}
```

### Tenant-Isolating Policy

```go
type tenantPolicy struct {
    allowed map[uuid.UUID]uuid.UUID // actorID -> tenantID
}

func (p tenantPolicy) Authorize(_ context.Context, check types.PolicyCheck) error {
    tenant := p.allowed[check.Actor.ID]
    if tenant == uuid.Nil || check.Scope.TenantID == uuid.Nil {
        return nil // Allow if no tenant restriction
    }
    if tenant != check.Scope.TenantID {
        return types.ErrUnauthorizedScope
    }
    return nil
}
```

### Multi-Tenant Isolation Test

```go
func TestService_MultiTenantIsolation(t *testing.T) {
    ctx := context.Background()
    tenantA := uuid.New()
    tenantB := uuid.New()

    authRepo := newMTAuthRepo()
    userTenantA := authRepo.seedUser(tenantA)

    actorA := types.ActorRef{ID: uuid.New(), Type: "tenant-admin"}
    actorB := types.ActorRef{ID: uuid.New(), Type: "tenant-admin"}

    resolver := staticScopeResolver{
        scopes: map[uuid.UUID]types.ScopeFilter{
            actorA.ID: {TenantID: tenantA},
            actorB.ID: {TenantID: tenantB},
        },
    }
    policy := tenantPolicy{
        allowed: map[uuid.UUID]uuid.UUID{
            actorA.ID: tenantA,
            actorB.ID: tenantB,
        },
    }

    svc := service.New(service.Config{
        AuthRepository:       authRepo,
        InventoryRepository:  authRepo,
        // ... other repositories ...
        ScopeResolver:        resolver,
        AuthorizationPolicy:  policy,
    })

    // Tenant A can access their user
    err := svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
        UserID: userTenantA,
        Target: types.LifecycleStateSuspended,
        Actor:  actorA,
        Scope:  types.ScopeFilter{TenantID: tenantA},
    })
    require.NoError(t, err)

    // Tenant B cannot access Tenant A's user
    err = svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
        UserID: userTenantA,
        Target: types.LifecycleStateActive,
        Actor:  actorB,
        Scope:  types.ScopeFilter{TenantID: tenantA},
    })
    require.ErrorIs(t, err, types.ErrUnauthorizedScope)
}
```

---

## Testing Lifecycle Transitions

### Testing Valid Transitions

```go
func TestLifecycleTransitions_ValidPaths(t *testing.T) {
    policy := types.DefaultTransitionPolicy()

    validPaths := []struct {
        from types.LifecycleState
        to   types.LifecycleState
    }{
        {types.LifecycleStatePending, types.LifecycleStateActive},
        {types.LifecycleStateActive, types.LifecycleStateSuspended},
        {types.LifecycleStateSuspended, types.LifecycleStateActive},
        {types.LifecycleStateActive, types.LifecycleStateDisabled},
        {types.LifecycleStateDisabled, types.LifecycleStateActive},
        {types.LifecycleStateActive, types.LifecycleStateArchived},
    }

    for _, path := range validPaths {
        t.Run(fmt.Sprintf("%s->%s", path.from, path.to), func(t *testing.T) {
            err := policy.Validate(path.from, path.to)
            require.NoError(t, err)
        })
    }
}
```

### Testing Invalid Transitions

```go
func TestLifecycleTransitions_InvalidPaths(t *testing.T) {
    policy := types.DefaultTransitionPolicy()

    invalidPaths := []struct {
        from types.LifecycleState
        to   types.LifecycleState
    }{
        {types.LifecycleStateActive, types.LifecycleStatePending},
        {types.LifecycleStateArchived, types.LifecycleStateActive},
        {types.LifecycleStatePending, types.LifecycleStateSuspended},
    }

    for _, path := range invalidPaths {
        t.Run(fmt.Sprintf("%s->%s", path.from, path.to), func(t *testing.T) {
            err := policy.Validate(path.from, path.to)
            require.ErrorIs(t, err, types.ErrTransitionNotAllowed)
        })
    }
}
```

### Testing Full Lifecycle Flow

```go
func TestLifecycle_FullOnboardingFlow(t *testing.T) {
    ctx := context.Background()
    repo := memory.NewAuthRepository()
    activityStore := memory.NewActivityStore()

    svc := users.New(users.Config{
        AuthRepository:       repo,
        InventoryRepository:  repo,
        RoleRegistry:         memory.NewRoleRegistry(),
        ActivitySink:         activityStore,
        ActivityRepository:   activityStore,
        ProfileRepository:    memory.NewProfileRepository(),
        PreferenceRepository: memory.NewPreferenceRepository(),
    })

    actor := types.ActorRef{ID: uuid.New(), Type: "system"}

    // 1. Invite user (creates in pending state)
    inviteResult := &command.UserInviteResult{}
    err := svc.Commands().UserInvite.Execute(ctx, command.UserInviteInput{
        Email:  "new@example.com",
        Actor:  actor,
        Result: inviteResult,
    })
    require.NoError(t, err)
    require.Equal(t, types.LifecycleStatePending, inviteResult.User.Status)

    // 2. Activate user
    err = svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
        UserID: inviteResult.User.ID,
        Target: types.LifecycleStateActive,
        Actor:  actor,
        Reason: "email verified",
    })
    require.NoError(t, err)

    // 3. Verify user is active
    page, err := svc.Queries().UserInventory.Query(ctx, types.UserInventoryFilter{
        Actor:    actor,
        Statuses: []types.LifecycleState{types.LifecycleStateActive},
    })
    require.NoError(t, err)
    require.Equal(t, 1, len(page.Users))
    require.Equal(t, types.LifecycleStateActive, page.Users[0].Status)

    // 4. Verify activity was logged
    feed, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
        Actor:      actor,
        Verbs:      []string{"user.lifecycle.transition"},
        Pagination: types.Pagination{Limit: 10},
    })
    require.NoError(t, err)
    require.GreaterOrEqual(t, len(feed.Records), 1)
}
```

---

## Testing Activity Logging

### Verifying Activity Records

```go
func TestActivityLogging_RecordsDetails(t *testing.T) {
    ctx := context.Background()

    var capturedRecord types.ActivityRecord
    sink := &recordingActivitySink{
        onLog: func(r types.ActivityRecord) {
            capturedRecord = r
        },
    }

    repo := newFakeAuthRepo()
    userID := uuid.New()
    repo.users[userID] = &types.AuthUser{
        ID:     userID,
        Status: types.LifecycleStateActive,
    }

    cmd := command.NewUserLifecycleTransitionCommand(command.LifecycleCommandConfig{
        Repository: repo,
        Activity:   sink,
    })

    actorID := uuid.New()
    err := cmd.Execute(ctx, command.UserLifecycleTransitionInput{
        UserID: userID,
        Target: types.LifecycleStateSuspended,
        Actor:  types.ActorRef{ID: actorID},
        Reason: "policy violation",
        Scope:  types.ScopeFilter{TenantID: uuid.New()},
    })

    require.NoError(t, err)
    require.Equal(t, "user.lifecycle.transition", capturedRecord.Verb)
    require.Equal(t, userID, capturedRecord.UserID)
    require.Equal(t, actorID, capturedRecord.ActorID)
    require.Equal(t, "user", capturedRecord.ObjectType)
    require.Equal(t, "lifecycle", capturedRecord.Channel)
    require.Equal(t, "active", capturedRecord.Data["from_state"])
    require.Equal(t, types.LifecycleStateSuspended, capturedRecord.Data["to_state"])
    require.Equal(t, "policy violation", capturedRecord.Data["reason"])
}
```

### Testing Activity Query Filtering

```go
func TestActivityQuery_Filtering(t *testing.T) {
    ctx := context.Background()
    store := memory.NewActivityStore()

    // Seed various activity records
    tenantA := uuid.New()
    tenantB := uuid.New()

    records := []types.ActivityRecord{
        {Verb: "user.created", TenantID: tenantA},
        {Verb: "user.lifecycle.transition", TenantID: tenantA},
        {Verb: "role.assigned", TenantID: tenantA},
        {Verb: "user.created", TenantID: tenantB},
    }

    for _, r := range records {
        _ = store.Log(ctx, r)
    }

    // Filter by verb
    page, err := store.ListActivity(ctx, types.ActivityFilter{
        Verbs:      []string{"user.created"},
        Pagination: types.Pagination{Limit: 10},
    })
    require.NoError(t, err)
    require.Equal(t, 2, len(page.Records))

    // Filter by tenant
    page, err = store.ListActivity(ctx, types.ActivityFilter{
        Scope:      types.ScopeFilter{TenantID: tenantA},
        Pagination: types.Pagination{Limit: 10},
    })
    require.NoError(t, err)
    require.Equal(t, 3, len(page.Records))
}
```

---

## Example Test Patterns

### Table-Driven Tests

```go
func TestUserCreate_Validation(t *testing.T) {
    tests := []struct {
        name    string
        input   command.UserCreateInput
        wantErr error
    }{
        {
            name: "missing actor",
            input: command.UserCreateInput{
                User: &types.AuthUser{Email: "test@example.com"},
            },
            wantErr: command.ErrActorRequired,
        },
        {
            name: "missing user",
            input: command.UserCreateInput{
                Actor: types.ActorRef{ID: uuid.New()},
            },
            wantErr: command.ErrUserRequired,
        },
        {
            name: "valid input",
            input: command.UserCreateInput{
                User:  &types.AuthUser{Email: "test@example.com"},
                Actor: types.ActorRef{ID: uuid.New()},
            },
            wantErr: nil,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            repo := newFakeAuthRepo()
            cmd := command.NewUserCreateCommand(command.UserCreateCommandConfig{
                Repository: repo,
            })

            err := cmd.Execute(context.Background(), tt.input)

            if tt.wantErr != nil {
                require.ErrorIs(t, err, tt.wantErr)
            } else {
                require.NoError(t, err)
            }
        })
    }
}
```

### Parallel Tests

```go
func TestRepository_ConcurrentAccess(t *testing.T) {
    t.Parallel()

    repo := memory.NewAuthRepository()
    ctx := context.Background()

    var wg sync.WaitGroup
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()

            user := &types.AuthUser{
                Email:    fmt.Sprintf("user%d@example.com", i),
                Username: fmt.Sprintf("user%d", i),
            }

            created, err := repo.Create(ctx, user)
            require.NoError(t, err)

            fetched, err := repo.GetByID(ctx, created.ID)
            require.NoError(t, err)
            require.Equal(t, created.ID, fetched.ID)
        }(i)
    }
    wg.Wait()
}
```

### Cleanup Pattern

```go
func TestWithCleanup(t *testing.T) {
    db := newTestDB(t) // t.Cleanup registered inside

    // Create repository
    repo, err := activity.NewRepository(activity.RepositoryConfig{DB: db})
    require.NoError(t, err)

    // Test code...
    // db.Close() called automatically via t.Cleanup
}
```

### Subtests for Related Scenarios

```go
func TestRoleOperations(t *testing.T) {
    ctx := context.Background()
    registry := memory.NewRoleRegistry()
    actor := types.ActorRef{ID: uuid.New()}

    var roleID uuid.UUID

    t.Run("create role", func(t *testing.T) {
        role, err := registry.CreateRole(ctx, types.RoleMutation{
            Name:        "Editors",
            Permissions: []string{"content.read", "content.write"},
            ActorID:     actor.ID,
        })
        require.NoError(t, err)
        require.Equal(t, "Editors", role.Name)
        roleID = role.ID
    })

    t.Run("update role", func(t *testing.T) {
        role, err := registry.UpdateRole(ctx, roleID, types.RoleMutation{
            Name:        "Senior Editors",
            Permissions: []string{"content.read", "content.write", "content.delete"},
            ActorID:     actor.ID,
        })
        require.NoError(t, err)
        require.Equal(t, "Senior Editors", role.Name)
        require.Len(t, role.Permissions, 3)
    })

    t.Run("delete role", func(t *testing.T) {
        err := registry.DeleteRole(ctx, roleID, types.ScopeFilter{}, actor.ID)
        require.NoError(t, err)

        _, err = registry.GetRole(ctx, roleID, types.ScopeFilter{})
        require.Error(t, err)
    })
}
```

---

## Summary

Testing `go-users` applications effectively requires:

1. **In-memory repositories** from `examples/internal/memory` for fast unit tests
2. **SQLite integration tests** for validating SQL queries and migrations
3. **Fake/mock repositories** for verifying specific behaviors
4. **Fixed clocks and ID generators** for deterministic tests
5. **Scope resolver/policy mocks** for testing multi-tenancy

Key testing areas:
- Command execution and validation
- Query filtering and pagination
- Hook invocation
- Activity logging
- Scope enforcement
- Lifecycle transitions

For more details, see:
- [GUIDE_HOOKS.md](GUIDE_HOOKS.md) - Testing hooks
- [GUIDE_MIGRATIONS.md](GUIDE_MIGRATIONS.md) - Migration smoke tests
- [GUIDE_MULTITENANCY.md](GUIDE_MULTITENANCY.md) - Scope testing patterns
