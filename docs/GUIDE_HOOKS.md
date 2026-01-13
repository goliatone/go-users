# GUIDE_HOOKS.md

This guide covers the hooks system in `go-users`, enabling event-driven integrations for lifecycle changes, role mutations, preference updates, profile changes, and activity logging.

---

## Table of Contents

1. [Overview](#overview)
2. [Hooks Architecture](#hooks-architecture)
3. [Available Hooks](#available-hooks)
4. [Hook Event Payloads](#hook-event-payloads)
5. [Configuring Hooks](#configuring-hooks)
6. [Common Integrations](#common-integrations)
7. [Error Handling](#error-handling)
8. [Testing Hooks](#testing-hooks)
9. [Best Practices](#best-practices)

---

## Overview

Hooks provide a mechanism for reacting to key events within `go-users`. When commands complete successfully, they emit events that your application can observe and respond to. This enables:

- **Email notifications** - Send welcome emails, password reset confirmations
- **Cache invalidation** - Clear cached user data when profiles change
- **Analytics/tracking** - Record user actions for business intelligence
- **WebSocket broadcasts** - Push real-time updates to connected clients
- **Webhook delivery** - Notify external systems of state changes
- **Audit logging** - Capture detailed audit trails beyond standard activity

### Design Principles

- **Non-blocking by default** - Hooks execute synchronously but should not block
- **Fire-and-forget** - Hook errors are logged but don't fail the parent operation
- **Composable** - Multiple handlers can be combined for different concerns
- **Scope-aware** - Events include tenant/org context for multi-tenant filtering

---

## Hooks Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        Command Execution                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│   ┌─────────────────┐                                          │
│   │ Lifecycle       │──┐                                        │
│   │ Transition      │  │                                        │
│   └─────────────────┘  │                                        │
│                        │   ┌─────────────────────────────────┐  │
│   ┌─────────────────┐  ├──▶│ types.Hooks                     │  │
│   │ Role            │──┤   │                                 │  │
│   │ Operations      │  │   │ AfterLifecycle(ctx, event)      │  │
│   └─────────────────┘  │   │ AfterRoleChange(ctx, event)     │  │
│                        │   │ AfterPreferenceChange(ctx, ev)  │  │
│   ┌─────────────────┐  │   │ AfterProfileChange(ctx, event)  │  │
│   │ Preference      │──┤   │ AfterActivity(ctx, record)      │  │
│   │ Mutations       │  │   │                                 │  │
│   └─────────────────┘  │   └─────────────────────────────────┘  │
│                        │                   │                    │
│   ┌─────────────────┐  │                   ▼                    │
│   │ Profile         │──┤   ┌─────────────────────────────────┐  │
│   │ Updates         │  │   │ Your Handlers                   │  │
│   └─────────────────┘  │   │                                 │  │
│                        │   │ • Email service                 │  │
│   ┌─────────────────┐  │   │ • Cache invalidation            │  │
│   │ Activity        │──┘   │ • WebSocket broadcaster         │  │
│   │ Logging         │      │ • Webhook dispatcher            │  │
│   └─────────────────┘      │ • Analytics tracker             │  │
│                            └─────────────────────────────────┘  │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### Hook Execution Flow

```
Command.Execute()
    │
    ├── Validate input
    ├── Enforce scope guard
    ├── Perform operation (repository call)
    ├── Log activity (ActivitySink)
    │
    └── Emit hook (if configured)
            │
            ├── AfterActivity (if activity was logged)
            └── Domain-specific hook (AfterLifecycle, etc.)
```

---

## Available Hooks

The `types.Hooks` struct groups all available hook callbacks:

```go
type Hooks struct {
    AfterLifecycle        func(context.Context, LifecycleEvent)
    AfterRoleChange       func(context.Context, RoleEvent)
    AfterPreferenceChange func(context.Context, PreferenceEvent)
    AfterProfileChange    func(context.Context, ProfileEvent)
    AfterActivity         func(context.Context, ActivityRecord)
}
```

### AfterLifecycle

Triggered after user lifecycle state transitions:

| Trigger | Description |
|---------|-------------|
| `UserLifecycleTransition` | Single user state change |
| `BulkUserTransition` | Each user in bulk operation |
| `UserInvite` | New user created in pending state |

### AfterRoleChange

Triggered after role-related mutations:

| Trigger | Action Value |
|---------|--------------|
| `CreateRole` | `"role.created"` |
| `UpdateRole` | `"role.updated"` |
| `DeleteRole` | `"role.deleted"` |
| `AssignRole` | `"role.assigned"` |
| `UnassignRole` | `"role.unassigned"` |

### AfterPreferenceChange

Triggered after preference mutations:

| Trigger | Action Value |
|---------|--------------|
| `PreferenceUpsert` | `"preference.upsert"` |
| `PreferenceDelete` | `"preference.delete"` |

### AfterProfileChange

Triggered after profile updates:

| Trigger | Description |
|---------|-------------|
| `ProfileUpsert` | Profile created or updated |

### AfterActivity

Triggered after any activity is logged:

| Trigger | Description |
|---------|-------------|
| `LogActivity` | Manual activity logging |
| Any command | Automatic activity logging from commands |

---

## Hook Event Payloads

### LifecycleEvent

```go
type LifecycleEvent struct {
    UserID     uuid.UUID          // Target user
    ActorID    uuid.UUID          // Who performed the action
    FromState  LifecycleState     // Previous state
    ToState    LifecycleState     // New state
    Reason     string             // Optional reason
    OccurredAt time.Time          // When it happened
    Scope      ScopeFilter        // Tenant/org context
    Metadata   map[string]any     // Additional data
}
```

**Example payload:**
```go
LifecycleEvent{
    UserID:     uuid.MustParse("..."),
    ActorID:    uuid.MustParse("..."),
    FromState:  types.LifecycleStatePending,
    ToState:    types.LifecycleStateActive,
    Reason:     "User completed onboarding",
    OccurredAt: time.Now().UTC(),
    Scope: types.ScopeFilter{
        TenantID: tenantID,
        OrgID:    orgID,
    },
    Metadata: map[string]any{
        "activation_method": "email_verification",
    },
}
```

### RoleEvent

```go
type RoleEvent struct {
    RoleID     uuid.UUID        // Target role
    UserID     uuid.UUID        // Target user (for assign/unassign)
    Action     string           // role.created, role.assigned, etc.
    ActorID    uuid.UUID        // Who performed the action
    Scope      ScopeFilter      // Tenant/org context
    OccurredAt time.Time        // When it happened
    Role       RoleDefinition   // Full role details (when available)
}
```

**Example payload:**
```go
RoleEvent{
    RoleID:     roleID,
    UserID:     userID,
    Action:     "role.assigned",
    ActorID:    adminID,
    Scope: types.ScopeFilter{
        TenantID: tenantID,
    },
    OccurredAt: time.Now().UTC(),
    Role: types.RoleDefinition{
        ID:          roleID,
        Name:        "Editor",
        Permissions: []string{"content.read", "content.write"},
    },
}
```

### PreferenceEvent

```go
type PreferenceEvent struct {
    UserID     uuid.UUID       // Target user (uuid.Nil for system/tenant/org)
    Scope      ScopeFilter     // Tenant/org context
    Key        string          // Preference key
    Action     string          // preference.upsert or preference.delete
    ActorID    uuid.UUID       // Who performed the action
    OccurredAt time.Time       // When it happened
}
```

**Example payload:**
```go
PreferenceEvent{
    UserID:     userID,
    Scope: types.ScopeFilter{
        TenantID: tenantID,
        OrgID:    orgID,
    },
    Key:        "notifications.email.enabled",
    Action:     "preference.upsert",
    ActorID:    userID,
    OccurredAt: time.Now().UTC(),
}
```

### ProfileEvent

```go
type ProfileEvent struct {
    UserID     uuid.UUID       // Target user
    Scope      ScopeFilter     // Tenant/org context
    ActorID    uuid.UUID       // Who performed the action
    OccurredAt time.Time       // When it happened
    Profile    UserProfile     // Updated profile data
}
```

**Example payload:**
```go
ProfileEvent{
    UserID:     userID,
    Scope: types.ScopeFilter{
        TenantID: tenantID,
    },
    ActorID:    userID,
    OccurredAt: time.Now().UTC(),
    Profile: types.UserProfile{
        UserID:      userID,
        DisplayName: "Jane Doe",
        Locale:      "en-US",
        Timezone:    "America/New_York",
    },
}
```

### ActivityRecord

```go
type ActivityRecord struct {
    ID         uuid.UUID       // Activity ID
    UserID     uuid.UUID       // Subject user
    ActorID    uuid.UUID       // Who performed the action
    Verb       string          // Action verb
    ObjectType string          // Target object type
    ObjectID   string          // Target object ID
    Channel    string          // Activity channel
    IP         string          // Client IP (if available)
    TenantID   uuid.UUID       // Tenant context
    OrgID      uuid.UUID       // Org context
    Data       map[string]any  // Additional metadata
    OccurredAt time.Time       // When it happened
}
```

---

## Configuring Hooks

### Basic Configuration

```go
import (
    users "github.com/goliatone/go-users"
    "github.com/goliatone/go-users/pkg/types"
)

svc := users.New(users.Config{
    AuthRepository:       authRepo,
    RoleRegistry:         roleRegistry,
    ActivitySink:         activitySink,
    // ... other config ...

    Hooks: types.Hooks{
        AfterLifecycle: func(ctx context.Context, event types.LifecycleEvent) {
            log.Printf("User %s transitioned from %s to %s",
                event.UserID, event.FromState, event.ToState)
        },
        AfterRoleChange: func(ctx context.Context, event types.RoleEvent) {
            log.Printf("Role event: %s for role %s", event.Action, event.RoleID)
        },
        AfterPreferenceChange: func(ctx context.Context, event types.PreferenceEvent) {
            log.Printf("Preference %s changed for user %s", event.Key, event.UserID)
        },
        AfterProfileChange: func(ctx context.Context, event types.ProfileEvent) {
            log.Printf("Profile updated for user %s", event.UserID)
        },
        AfterActivity: func(ctx context.Context, record types.ActivityRecord) {
            log.Printf("Activity: %s %s/%s", record.Verb, record.ObjectType, record.ObjectID)
        },
    },
})
```

### Composing Multiple Handlers

```go
// Create composable hook functions
func composeLifecycleHooks(handlers ...func(context.Context, types.LifecycleEvent)) func(context.Context, types.LifecycleEvent) {
    return func(ctx context.Context, event types.LifecycleEvent) {
        for _, handler := range handlers {
            handler(ctx, event)
        }
    }
}

// Individual handlers
func logLifecycle(ctx context.Context, event types.LifecycleEvent) {
    log.Printf("Lifecycle: %s -> %s", event.FromState, event.ToState)
}

func notifyLifecycle(ctx context.Context, event types.LifecycleEvent) {
    // Send notification
}

func metricsLifecycle(ctx context.Context, event types.LifecycleEvent) {
    // Record metrics
}

// Compose them
svc := users.New(users.Config{
    // ...
    Hooks: types.Hooks{
        AfterLifecycle: composeLifecycleHooks(
            logLifecycle,
            notifyLifecycle,
            metricsLifecycle,
        ),
    },
})
```

### Tenant-Filtered Hooks

```go
func tenantFilteredHook(targetTenantID uuid.UUID, handler func(context.Context, types.LifecycleEvent)) func(context.Context, types.LifecycleEvent) {
    return func(ctx context.Context, event types.LifecycleEvent) {
        if event.Scope.TenantID != targetTenantID {
            return // Skip events for other tenants
        }
        handler(ctx, event)
    }
}

svc := users.New(users.Config{
    Hooks: types.Hooks{
        AfterLifecycle: tenantFilteredHook(myTenantID, func(ctx context.Context, event types.LifecycleEvent) {
            // Only handles events for myTenantID
        }),
    },
})
```

---

## Common Integrations

### Email Notifications

```go
type EmailNotifier struct {
    mailer MailService
    logger types.Logger
}

func (n *EmailNotifier) OnLifecycle(ctx context.Context, event types.LifecycleEvent) {
    switch event.ToState {
    case types.LifecycleStateActive:
        // Send welcome email
        go n.sendWelcomeEmail(ctx, event.UserID)

    case types.LifecycleStateSuspended:
        // Send suspension notice
        go n.sendSuspensionNotice(ctx, event.UserID, event.Reason)

    case types.LifecycleStateArchived:
        // Send account closure confirmation
        go n.sendClosureConfirmation(ctx, event.UserID)
    }
}

func (n *EmailNotifier) OnRoleChange(ctx context.Context, event types.RoleEvent) {
    if event.Action == "role.assigned" {
        go n.sendRoleAssignedNotification(ctx, event.UserID, event.Role.Name)
    }
}

// Wire into service
emailNotifier := &EmailNotifier{mailer: mailSvc, logger: logger}

svc := users.New(users.Config{
    Hooks: types.Hooks{
        AfterLifecycle:  emailNotifier.OnLifecycle,
        AfterRoleChange: emailNotifier.OnRoleChange,
    },
})
```

### Cache Invalidation

```go
type CacheInvalidator struct {
    cache  CacheClient
    logger types.Logger
}

func (c *CacheInvalidator) OnProfileChange(ctx context.Context, event types.ProfileEvent) {
    keys := []string{
        fmt.Sprintf("user:%s:profile", event.UserID),
        fmt.Sprintf("tenant:%s:user:%s", event.Scope.TenantID, event.UserID),
    }
    for _, key := range keys {
        if err := c.cache.Delete(ctx, key); err != nil {
            c.logger.Error("cache invalidation failed", err, "key", key)
        }
    }
}

func (c *CacheInvalidator) OnPreferenceChange(ctx context.Context, event types.PreferenceEvent) {
    key := fmt.Sprintf("user:%s:preferences", event.UserID)
    _ = c.cache.Delete(ctx, key)
}

func (c *CacheInvalidator) OnRoleChange(ctx context.Context, event types.RoleEvent) {
    if event.UserID != uuid.Nil {
        key := fmt.Sprintf("user:%s:roles", event.UserID)
        _ = c.cache.Delete(ctx, key)
    }
}

// Wire into service
invalidator := &CacheInvalidator{cache: redisClient, logger: logger}

svc := users.New(users.Config{
    Hooks: types.Hooks{
        AfterProfileChange:    invalidator.OnProfileChange,
        AfterPreferenceChange: invalidator.OnPreferenceChange,
        AfterRoleChange:       invalidator.OnRoleChange,
    },
})
```

### WebSocket Broadcasts

```go
type RealtimeBroadcaster struct {
    hub    *WebSocketHub
    logger types.Logger
}

func (b *RealtimeBroadcaster) OnLifecycle(ctx context.Context, event types.LifecycleEvent) {
    msg := WebSocketMessage{
        Type: "user.lifecycle",
        Payload: map[string]any{
            "user_id":    event.UserID,
            "from_state": event.FromState,
            "to_state":   event.ToState,
            "timestamp":  event.OccurredAt,
        },
    }

    // Broadcast to tenant channel
    b.hub.BroadcastToTenant(event.Scope.TenantID, msg)
}

func (b *RealtimeBroadcaster) OnActivity(ctx context.Context, record types.ActivityRecord) {
    msg := WebSocketMessage{
        Type: "activity",
        Payload: map[string]any{
            "verb":        record.Verb,
            "object_type": record.ObjectType,
            "object_id":   record.ObjectID,
            "actor_id":    record.ActorID,
            "timestamp":   record.OccurredAt,
        },
    }

    // Broadcast to user's personal channel
    if record.UserID != uuid.Nil {
        b.hub.BroadcastToUser(record.UserID, msg)
    }
}
```

### Webhook Delivery

```go
type WebhookDispatcher struct {
    client    *http.Client
    endpoints []WebhookEndpoint
    queue     JobQueue
    logger    types.Logger
}

type WebhookEndpoint struct {
    URL      string
    Events   []string // e.g., ["lifecycle.*", "role.assigned"]
    TenantID uuid.UUID
    Secret   string
}

func (w *WebhookDispatcher) OnLifecycle(ctx context.Context, event types.LifecycleEvent) {
    payload := WebhookPayload{
        Event:     fmt.Sprintf("lifecycle.%s", event.ToState),
        Timestamp: event.OccurredAt,
        Data: map[string]any{
            "user_id":    event.UserID,
            "actor_id":   event.ActorID,
            "from_state": event.FromState,
            "to_state":   event.ToState,
            "reason":     event.Reason,
            "tenant_id":  event.Scope.TenantID,
        },
    }

    // Queue webhook deliveries (don't block the request)
    for _, endpoint := range w.matchingEndpoints(payload.Event, event.Scope.TenantID) {
        w.queue.Enqueue(ctx, WebhookJob{
            Endpoint: endpoint,
            Payload:  payload,
        })
    }
}

func (w *WebhookDispatcher) matchingEndpoints(event string, tenantID uuid.UUID) []WebhookEndpoint {
    var matches []WebhookEndpoint
    for _, ep := range w.endpoints {
        if ep.TenantID != uuid.Nil && ep.TenantID != tenantID {
            continue
        }
        if w.eventMatches(event, ep.Events) {
            matches = append(matches, ep)
        }
    }
    return matches
}
```

### Analytics/Tracking

```go
type AnalyticsTracker struct {
    client AnalyticsClient
    logger types.Logger
}

func (a *AnalyticsTracker) OnActivity(ctx context.Context, record types.ActivityRecord) {
    event := AnalyticsEvent{
        Name:      record.Verb,
        UserID:    record.UserID.String(),
        Timestamp: record.OccurredAt,
        Properties: map[string]any{
            "object_type": record.ObjectType,
            "object_id":   record.ObjectID,
            "channel":     record.Channel,
            "tenant_id":   record.TenantID.String(),
            "ip":          record.IP,
        },
    }

    // Track asynchronously
    go func() {
        if err := a.client.Track(event); err != nil {
            a.logger.Error("analytics tracking failed", err,
                "verb", record.Verb,
                "user_id", record.UserID)
        }
    }()
}

func (a *AnalyticsTracker) OnLifecycle(ctx context.Context, event types.LifecycleEvent) {
    // Track funnel events
    if event.ToState == types.LifecycleStateActive && event.FromState == types.LifecycleStatePending {
        a.client.Track(AnalyticsEvent{
            Name:      "user.activated",
            UserID:    event.UserID.String(),
            Timestamp: event.OccurredAt,
            Properties: map[string]any{
                "activation_reason": event.Reason,
            },
        })
    }
}
```

---

## Error Handling

Hooks are designed to be non-blocking. The internal helper functions wrap hook calls with panic recovery:

```go
// From registry/bun_registry.go
func (r *RoleRegistry) emitRoleEvent(ctx context.Context, event types.RoleEvent) {
    if r.hooks.AfterRoleChange == nil {
        return
    }
    defer func() {
        if rec := recover(); rec != nil {
            r.logger.Error("role hook panic", errors.New("panic in AfterRoleChange"), "panic", rec)
        }
    }()
    r.hooks.AfterRoleChange(ctx, event)
}
```

### Best Practices for Error Handling

```go
func safeHookHandler(ctx context.Context, event types.LifecycleEvent) {
    // 1. Use goroutines for I/O operations
    go func() {
        defer func() {
            if r := recover(); r != nil {
                log.Printf("hook panic recovered: %v", r)
            }
        }()

        // Perform the actual work
        if err := sendNotification(ctx, event); err != nil {
            log.Printf("notification failed: %v", err)
            // Don't propagate the error - log and continue
        }
    }()
}

// 2. Use timeouts for external calls
func sendNotificationWithTimeout(ctx context.Context, event types.LifecycleEvent) error {
    ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()

    // Make external call with timeout
    return externalService.Notify(ctx, event)
}

// 3. Use circuit breakers for unreliable services
type CircuitBreakerHook struct {
    breaker *circuitbreaker.CircuitBreaker
    handler func(context.Context, types.LifecycleEvent) error
}

func (c *CircuitBreakerHook) Handle(ctx context.Context, event types.LifecycleEvent) {
    _, err := c.breaker.Execute(func() (interface{}, error) {
        return nil, c.handler(ctx, event)
    })
    if err != nil {
        log.Printf("hook circuit breaker: %v", err)
    }
}
```

---

## Testing Hooks

### Unit Testing Hook Handlers

```go
func TestEmailNotifier_OnLifecycle(t *testing.T) {
    var sentEmails []string
    mockMailer := &MockMailer{
        SendFunc: func(to, template string, data any) error {
            sentEmails = append(sentEmails, template)
            return nil
        },
    }

    notifier := &EmailNotifier{mailer: mockMailer, logger: types.NopLogger{}}

    ctx := context.Background()
    event := types.LifecycleEvent{
        UserID:    uuid.New(),
        FromState: types.LifecycleStatePending,
        ToState:   types.LifecycleStateActive,
    }

    notifier.OnLifecycle(ctx, event)

    // Wait for goroutine
    time.Sleep(100 * time.Millisecond)

    if len(sentEmails) != 1 || sentEmails[0] != "welcome" {
        t.Errorf("expected welcome email, got %v", sentEmails)
    }
}
```

### Integration Testing with Hooks

```go
func TestService_LifecycleHook(t *testing.T) {
    var capturedEvents []types.LifecycleEvent
    var mu sync.Mutex

    repo := memory.NewAuthRepository()
    svc := users.New(users.Config{
        AuthRepository:       repo,
        InventoryRepository:  repo,
        RoleRegistry:         memory.NewRoleRegistry(),
        ActivitySink:         memory.NewActivityStore(),
        ActivityRepository:   memory.NewActivityStore(),
        ProfileRepository:    memory.NewProfileRepository(),
        PreferenceRepository: memory.NewPreferenceRepository(),
        Hooks: types.Hooks{
            AfterLifecycle: func(_ context.Context, event types.LifecycleEvent) {
                mu.Lock()
                capturedEvents = append(capturedEvents, event)
                mu.Unlock()
            },
        },
    })

    ctx := context.Background()
    actor := types.ActorRef{ID: uuid.New(), Type: "test"}

    // Create user via invite
    inviteResult := &command.UserInviteResult{}
    err := svc.Commands().UserInvite.Execute(ctx, command.UserInviteInput{
        Email:  "test@example.com",
        Actor:  actor,
        Result: inviteResult,
    })
    require.NoError(t, err)

    // Transition to active
    err = svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
        UserID: inviteResult.User.ID,
        Target: types.LifecycleStateActive,
        Actor:  actor,
    })
    require.NoError(t, err)

    // Verify hooks were called
    mu.Lock()
    defer mu.Unlock()

    require.Len(t, capturedEvents, 1)
    assert.Equal(t, types.LifecycleStatePending, capturedEvents[0].FromState)
    assert.Equal(t, types.LifecycleStateActive, capturedEvents[0].ToState)
    assert.Equal(t, inviteResult.User.ID, capturedEvents[0].UserID)
}
```

### Testing Hook Error Recovery

```go
func TestHook_PanicRecovery(t *testing.T) {
    repo := memory.NewAuthRepository()

    // Create service with panicking hook
    svc := users.New(users.Config{
        AuthRepository: repo,
        // ... other config ...
        Hooks: types.Hooks{
            AfterLifecycle: func(_ context.Context, _ types.LifecycleEvent) {
                panic("intentional test panic")
            },
        },
    })

    ctx := context.Background()
    actor := types.ActorRef{ID: uuid.New(), Type: "test"}

    // Create and transition user - should not panic
    inviteResult := &command.UserInviteResult{}
    err := svc.Commands().UserInvite.Execute(ctx, command.UserInviteInput{
        Email:  "test@example.com",
        Actor:  actor,
        Result: inviteResult,
    })

    // Command should succeed despite hook panic
    assert.NoError(t, err)
    assert.NotNil(t, inviteResult.User)
}
```

---

## Best Practices

### 1. Keep Hooks Fast

```go
// Good: Queue work for background processing
func (h *Handler) OnLifecycle(ctx context.Context, event types.LifecycleEvent) {
    h.queue.Enqueue(ctx, LifecycleJob{Event: event})
}

// Avoid: Blocking operations in hooks
func (h *Handler) OnLifecycle(ctx context.Context, event types.LifecycleEvent) {
    // This blocks the request!
    resp, err := http.Post(webhookURL, "application/json", body)
    // ...
}
```

### 2. Use Goroutines for I/O

```go
func (h *Handler) OnActivity(ctx context.Context, record types.ActivityRecord) {
    // Fire-and-forget with goroutine
    go func() {
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()

        if err := h.externalService.Track(ctx, record); err != nil {
            h.logger.Error("tracking failed", err)
        }
    }()
}
```

### 3. Filter Events Early

```go
func (h *Handler) OnLifecycle(ctx context.Context, event types.LifecycleEvent) {
    // Early return for events we don't care about
    if event.ToState != types.LifecycleStateActive {
        return
    }

    // Only process activation events
    h.processActivation(ctx, event)
}
```

### 4. Log Hook Errors

```go
func (h *Handler) OnRoleChange(ctx context.Context, event types.RoleEvent) {
    if err := h.notifyRoleChange(ctx, event); err != nil {
        h.logger.Error("role change notification failed", err,
            "role_id", event.RoleID,
            "action", event.Action,
            "actor_id", event.ActorID)
    }
}
```

### 5. Consider Idempotency

```go
func (h *Handler) OnLifecycle(ctx context.Context, event types.LifecycleEvent) {
    // Use idempotency key to prevent duplicate processing
    idempotencyKey := fmt.Sprintf("lifecycle:%s:%s:%d",
        event.UserID,
        event.ToState,
        event.OccurredAt.UnixNano())

    if h.cache.Exists(ctx, idempotencyKey) {
        return // Already processed
    }

    h.processEvent(ctx, event)
    h.cache.Set(ctx, idempotencyKey, "1", 24*time.Hour)
}
```

---

## Summary

The `go-users` hooks system provides:

- **Five hook types**: AfterLifecycle, AfterRoleChange, AfterPreferenceChange, AfterProfileChange, AfterActivity
- **Rich event payloads** with user, actor, scope, and timestamp information
- **Non-blocking execution** with panic recovery
- **Composable handlers** for multiple concerns

Common integration patterns:
- Email notifications on lifecycle changes
- Cache invalidation on data mutations
- WebSocket broadcasts for real-time updates
- Webhook delivery to external systems
- Analytics tracking for user behavior

For more details, see:
- [GUIDE_USER_LIFECYCLE.md](GUIDE_USER_LIFECYCLE.md) - Lifecycle state transitions
- [GUIDE_ACTIVITY.md](GUIDE_ACTIVITY.md) - Activity logging
- [GUIDE_TESTING.md](GUIDE_TESTING.md) - Testing strategies
