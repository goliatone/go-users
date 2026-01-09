# User Lifecycle Management Guide

This guide covers the user lifecycle state machine in `go-users`, including state transitions, transition policies, bulk operations, and related workflows like invitations and password resets.

## Overview

Every user in `go-users` has a lifecycle state that determines their access level and account status. The lifecycle system provides:

- **State machine** with defined transitions between states
- **Transition policies** that enforce valid state changes
- **Bulk operations** for managing multiple users at once
- **Activity logging** for audit trails
- **Hooks** for integrating with notifications and external systems

## Lifecycle States

Users can be in one of five states:

| State | Description | Typical Use |
|-------|-------------|-------------|
| `pending` | User invited but not yet activated | New invitations, awaiting email verification |
| `active` | User has full access | Normal operational state |
| `suspended` | User temporarily restricted | Account review, policy violation |
| `disabled` | User permanently restricted | Pre-deletion hold, compliance |
| `archived` | User soft-deleted | Historical records, GDPR retention |

```go
import "github.com/goliatone/go-users/pkg/types"

// Available states
types.LifecycleStatePending   // "pending"
types.LifecycleStateActive    // "active"
types.LifecycleStateSuspended // "suspended"
types.LifecycleStateDisabled  // "disabled"
types.LifecycleStateArchived  // "archived"
```

## State Transition Graph

The default transition policy enforces this state machine:

```
                    ┌─────────────┐
                    │   pending   │
                    └──────┬──────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
              ▼            ▼            │
        ┌──────────┐  ┌──────────┐      │
        │  active  │◄─┤suspended │      │
        └────┬─────┘  └────┬─────┘      │
             │             │            │
    ┌────────┼─────────────┼────────────┘
    │        │             │
    │        ▼             ▼
    │   ┌──────────┐  ┌──────────┐
    │   │ archived │  │ disabled │
    │   └──────────┘  └────┬─────┘
    │                      │
    └──────────────────────┘
                           │
                           ▼
                    ┌──────────┐
                    │ archived │
                    └──────────┘
```

### Valid Transitions

| From State | Allowed Targets |
|------------|-----------------|
| `pending` | `active`, `disabled` |
| `active` | `suspended`, `disabled`, `archived` |
| `suspended` | `active`, `disabled` |
| `disabled` | `archived` |
| `archived` | (terminal state) |

## Basic Lifecycle Transitions

### Activating a User

The most common transition is activating a pending user after they complete registration:

```go
err := svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
    UserID: userID,
    Target: types.LifecycleStateActive,
    Actor:  actor,
    Reason: "User completed email verification",
})
if err != nil {
    log.Printf("Activation failed: %v", err)
    return err
}
```

### Suspending a User

Temporarily restrict access while keeping the account recoverable:

```go
err := svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
    UserID: userID,
    Target: types.LifecycleStateSuspended,
    Actor:  actor,
    Reason: "Account under review for policy violation",
    Metadata: map[string]any{
        "ticket_id":    "SUPPORT-1234",
        "reviewed_by":  actor.ID.String(),
        "review_notes": "Multiple login failures detected",
    },
})
```

### Reactivating a Suspended User

Restore access after resolving the suspension reason:

```go
err := svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
    UserID: userID,
    Target: types.LifecycleStateActive,
    Actor:  actor,
    Reason: "Review completed, account cleared",
    Metadata: map[string]any{
        "ticket_id":      "SUPPORT-1234",
        "resolution":     "false_positive",
        "reactivated_by": actor.ID.String(),
    },
})
```

### Disabling a User

Permanently restrict access (but retain the record):

```go
err := svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
    UserID: userID,
    Target: types.LifecycleStateDisabled,
    Actor:  actor,
    Reason: "Account terminated per user request",
})
```

### Archiving a User

Soft-delete for compliance and historical purposes:

```go
err := svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
    UserID: userID,
    Target: types.LifecycleStateArchived,
    Actor:  actor,
    Reason: "Data retention period expired",
})
```

## Command Input Structure

The `UserLifecycleTransitionInput` accepts these fields:

```go
type UserLifecycleTransitionInput struct {
    UserID   uuid.UUID              // Required: target user
    Target   types.LifecycleState   // Required: destination state
    Actor    types.ActorRef         // Required: who is performing this action
    Reason   string                 // Optional: human-readable explanation
    Metadata map[string]any         // Optional: structured audit data
    Scope    types.ScopeFilter      // Optional: tenant/org scope
    Result   *UserLifecycleTransitionResult // Optional: receive updated user
}
```

### Getting the Result

To receive the updated user after transition:

```go
result := &command.UserLifecycleTransitionResult{}

err := svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
    UserID: userID,
    Target: types.LifecycleStateActive,
    Actor:  actor,
    Reason: "Activation",
    Result: result,
})
if err != nil {
    return err
}

fmt.Printf("User %s is now %s\n", result.User.Email, result.User.Status)
```

## Bulk Transitions

For batch operations on multiple users:

```go
results := &[]command.BulkUserTransitionResult{}

err := svc.Commands().BulkUserTransition.Execute(ctx, command.BulkUserTransitionInput{
    UserIDs:     []uuid.UUID{user1, user2, user3},
    Target:      types.LifecycleStateSuspended,
    Actor:       actor,
    Reason:      "Bulk suspension for compliance review",
    StopOnError: false, // Continue processing even if some fail
    Results:     results,
})

// Check individual results
for _, r := range *results {
    if r.Err != nil {
        log.Printf("Failed to transition user %s: %v", r.UserID, r.Err)
    } else {
        log.Printf("Successfully transitioned user %s", r.UserID)
    }
}
```

### Stop on Error Mode

Set `StopOnError: true` to halt processing on the first failure:

```go
err := svc.Commands().BulkUserTransition.Execute(ctx, command.BulkUserTransitionInput{
    UserIDs:     userIDs,
    Target:      types.LifecycleStateActive,
    Actor:       actor,
    StopOnError: true, // Stop at first failure
    Results:     results,
})
```

## Transition Policies

### Default Policy

The default policy is created automatically when you don't specify one:

```go
svc := users.New(users.Config{
    // TransitionPolicy not specified - uses DefaultTransitionPolicy()
    AuthRepository: repo,
    // ...
})
```

The default policy matches the state graph shown above.

### Custom Transition Policy

Create a custom policy for different business rules:

```go
// Allow direct transition from pending to archived (e.g., for spam invites)
customPolicy := types.NewStaticTransitionPolicy(map[types.LifecycleState][]types.LifecycleState{
    types.LifecycleStatePending:   {types.LifecycleStateActive, types.LifecycleStateDisabled, types.LifecycleStateArchived},
    types.LifecycleStateActive:    {types.LifecycleStateSuspended, types.LifecycleStateDisabled, types.LifecycleStateArchived},
    types.LifecycleStateSuspended: {types.LifecycleStateActive, types.LifecycleStateDisabled},
    types.LifecycleStateDisabled:  {types.LifecycleStateArchived},
})

svc := users.New(users.Config{
    AuthRepository:   repo,
    TransitionPolicy: customPolicy,
    // ...
})
```

### Implementing TransitionPolicy Interface

For complex validation logic, implement the interface directly:

```go
type CustomTransitionPolicy struct {
    allowedHours []int // Only allow transitions during business hours
}

func (p *CustomTransitionPolicy) Validate(current, target types.LifecycleState) error {
    // Check business hours
    hour := time.Now().Hour()
    allowed := false
    for _, h := range p.allowedHours {
        if hour == h {
            allowed = true
            break
        }
    }
    if !allowed {
        return fmt.Errorf("transitions only allowed during business hours")
    }

    // Delegate to default policy for state validation
    return types.DefaultTransitionPolicy().Validate(current, target)
}

func (p *CustomTransitionPolicy) AllowedTargets(current types.LifecycleState) []types.LifecycleState {
    return types.DefaultTransitionPolicy().AllowedTargets(current)
}
```

### Querying Allowed Transitions

Check what transitions are valid from a given state:

```go
policy := types.DefaultTransitionPolicy()

// Get allowed targets from "active" state
targets := policy.AllowedTargets(types.LifecycleStateActive)
// Returns: [suspended, disabled, archived]

// Validate a specific transition
err := policy.Validate(types.LifecycleStateActive, types.LifecycleStatePending)
if err != nil {
    // err == types.ErrTransitionNotAllowed
    fmt.Println("Cannot transition from active back to pending")
}
```

## User Invite Workflow

The invite command creates users in the `pending` state with an invite token:

```go
result := &command.UserInviteResult{}

err := svc.Commands().UserInvite.Execute(ctx, command.UserInviteInput{
    Email:     "newuser@example.com",
    FirstName: "Jane",
    LastName:  "Doe",
    Role:      "member",
    Metadata: map[string]any{
        "department": "Engineering",
        "manager_id": managerID.String(),
    },
    Actor:  actor,
    Scope:  scope,
    Result: result,
})
if err != nil {
    return err
}

fmt.Printf("Invited user: %s\n", result.User.Email)
fmt.Printf("Invite token: %s\n", result.Token)
fmt.Printf("Expires at: %s\n", result.ExpiresAt)
```

### Invite Token Configuration

The default token TTL is 72 hours. Customize it in the service config:

```go
svc := users.New(users.Config{
    AuthRepository: repo,
    InviteTokenTTL: 24 * time.Hour, // 24-hour expiration
    // ...
})
```

### Complete Registration Flow

A typical invite-to-active flow:

```go
// 1. Admin invites user
inviteResult := &command.UserInviteResult{}
err := svc.Commands().UserInvite.Execute(ctx, command.UserInviteInput{
    Email:  "newuser@example.com",
    Actor:  adminActor,
    Result: inviteResult,
})

// 2. Send invite email (using your email service)
sendInviteEmail(inviteResult.User.Email, inviteResult.Token)

// 3. User clicks link, verifies token, sets password (your transport layer)
// ...

// 4. Activate the user after verification
err = svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
    UserID: inviteResult.User.ID,
    Target: types.LifecycleStateActive,
    Actor:  systemActor,
    Reason: "Email verified and password set",
})
```

## Password Reset Workflow

Reset a user's password hash:

```go
// Hash the new password (using your preferred hashing library)
newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
if err != nil {
    return err
}

result := &command.UserPasswordResetResult{}

err = svc.Commands().UserPasswordReset.Execute(ctx, command.UserPasswordResetInput{
    UserID:          userID,
    NewPasswordHash: string(newHash),
    Actor:           actor,
    Scope:           scope,
    Result:          result,
})
if err != nil {
    return err
}

fmt.Printf("Password reset for user: %s\n", result.User.Email)
```

### Self-Service Password Reset Flow

A typical password reset flow:

```go
// 1. User requests reset (your transport layer generates token)
// ...

// 2. User clicks link, provides new password (your transport layer validates token)
// ...

// 3. Update the password
err := svc.Commands().UserPasswordReset.Execute(ctx, command.UserPasswordResetInput{
    UserID:          userID,
    NewPasswordHash: hashedPassword,
    Actor:           types.ActorRef{ID: userID, Type: "user"}, // Self-service
    Reason:          "User-initiated password reset",
})
```

## Lifecycle Hooks

React to lifecycle changes for notifications, cache invalidation, or analytics:

```go
svc := users.New(users.Config{
    AuthRepository: repo,
    Hooks: types.Hooks{
        AfterLifecycle: func(ctx context.Context, event types.LifecycleEvent) {
            log.Printf("User %s: %s -> %s (reason: %s)",
                event.UserID,
                event.FromState,
                event.ToState,
                event.Reason,
            )

            // Send notifications based on state change
            switch event.ToState {
            case types.LifecycleStateActive:
                sendWelcomeEmail(ctx, event.UserID)
            case types.LifecycleStateSuspended:
                sendSuspensionNotice(ctx, event.UserID, event.Reason)
            case types.LifecycleStateArchived:
                triggerDataExport(ctx, event.UserID)
            }
        },
    },
    // ...
})
```

### Lifecycle Event Structure

```go
type LifecycleEvent struct {
    UserID     uuid.UUID          // The affected user
    ActorID    uuid.UUID          // Who performed the action
    FromState  LifecycleState     // Previous state
    ToState    LifecycleState     // New state
    Reason     string             // Human-readable reason
    OccurredAt time.Time          // When it happened
    Scope      ScopeFilter        // Tenant/org context
    Metadata   map[string]any     // Additional structured data
}
```

## Activity Records

All lifecycle transitions are automatically logged to the activity sink:

```go
// Activity record created for each transition
{
    Verb:       "user.lifecycle.transition",
    ObjectType: "user",
    ObjectID:   userID,
    Channel:    "lifecycle",
    Data: {
        "from_state": "pending",
        "to_state":   "active",
        "reason":     "User completed registration",
        "metadata":   { ... },
    },
}
```

Query lifecycle activity:

```go
feed, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor:   actor,
    Verbs:   []string{"user.lifecycle.transition"},
    Channel: "lifecycle",
    Pagination: types.Pagination{Limit: 50},
})
```

## Common Patterns

### Onboarding Flow

```go
func onboardUser(ctx context.Context, svc *users.Service, email string, actor types.ActorRef) error {
    // 1. Create pending user
    result := &command.UserInviteResult{}
    if err := svc.Commands().UserInvite.Execute(ctx, command.UserInviteInput{
        Email:  email,
        Actor:  actor,
        Result: result,
    }); err != nil {
        return fmt.Errorf("invite failed: %w", err)
    }

    // 2. Send welcome email with invite token
    if err := sendOnboardingEmail(result.User.Email, result.Token); err != nil {
        return fmt.Errorf("email failed: %w", err)
    }

    return nil
}

func completeOnboarding(ctx context.Context, svc *users.Service, userID uuid.UUID, passwordHash string) error {
    // 1. Set password
    if err := svc.Commands().UserPasswordReset.Execute(ctx, command.UserPasswordResetInput{
        UserID:          userID,
        NewPasswordHash: passwordHash,
        Actor:           types.ActorRef{ID: userID, Type: "user"},
    }); err != nil {
        return fmt.Errorf("password set failed: %w", err)
    }

    // 2. Activate user
    if err := svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
        UserID: userID,
        Target: types.LifecycleStateActive,
        Actor:  types.ActorRef{ID: userID, Type: "user"},
        Reason: "Completed onboarding",
    }); err != nil {
        return fmt.Errorf("activation failed: %w", err)
    }

    return nil
}
```

### Account Suspension with Review

```go
func suspendForReview(ctx context.Context, svc *users.Service, userID uuid.UUID, actor types.ActorRef, ticketID string) error {
    return svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
        UserID: userID,
        Target: types.LifecycleStateSuspended,
        Actor:  actor,
        Reason: fmt.Sprintf("Suspended for review - ticket %s", ticketID),
        Metadata: map[string]any{
            "ticket_id":    ticketID,
            "suspended_at": time.Now().UTC(),
            "reviewed_by":  actor.ID.String(),
        },
    })
}

func resolveReview(ctx context.Context, svc *users.Service, userID uuid.UUID, actor types.ActorRef, ticketID string, approve bool) error {
    var target types.LifecycleState
    var reason string

    if approve {
        target = types.LifecycleStateActive
        reason = fmt.Sprintf("Review approved - ticket %s", ticketID)
    } else {
        target = types.LifecycleStateDisabled
        reason = fmt.Sprintf("Review rejected - ticket %s", ticketID)
    }

    return svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
        UserID: userID,
        Target: target,
        Actor:  actor,
        Reason: reason,
        Metadata: map[string]any{
            "ticket_id":   ticketID,
            "resolution":  map[bool]string{true: "approved", false: "rejected"}[approve],
            "resolved_at": time.Now().UTC(),
            "resolved_by": actor.ID.String(),
        },
    })
}
```

### Bulk Deactivation for Compliance

```go
func deactivateInactiveUsers(ctx context.Context, svc *users.Service, actor types.ActorRef, inactiveSince time.Time) error {
    // 1. Query inactive users
    page, err := svc.Queries().UserInventory.Query(ctx, types.UserInventoryFilter{
        Actor:    actor,
        Statuses: []types.LifecycleState{types.LifecycleStateActive},
        // Additional filtering would be done at the repository level
    })
    if err != nil {
        return err
    }

    // 2. Filter by last login (example logic)
    var toDeactivate []uuid.UUID
    for _, user := range page.Users {
        if user.LoggedinAt != nil && user.LoggedinAt.Before(inactiveSince) {
            toDeactivate = append(toDeactivate, user.ID)
        }
    }

    if len(toDeactivate) == 0 {
        return nil
    }

    // 3. Bulk disable
    results := &[]command.BulkUserTransitionResult{}
    err = svc.Commands().BulkUserTransition.Execute(ctx, command.BulkUserTransitionInput{
        UserIDs:     toDeactivate,
        Target:      types.LifecycleStateDisabled,
        Actor:       actor,
        Reason:      fmt.Sprintf("Inactive since %s", inactiveSince.Format("2006-01-02")),
        StopOnError: false,
        Results:     results,
    })

    // 4. Log results
    var failed int
    for _, r := range *results {
        if r.Err != nil {
            failed++
        }
    }
    log.Printf("Deactivated %d users (%d failed)", len(toDeactivate)-failed, failed)

    return err
}
```

### GDPR Data Retention

```go
func archiveForRetention(ctx context.Context, svc *users.Service, actor types.ActorRef, userID uuid.UUID) error {
    // 1. Export user data (your implementation)
    exportPath, err := exportUserData(ctx, userID)
    if err != nil {
        return fmt.Errorf("export failed: %w", err)
    }

    // 2. Archive the user
    return svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
        UserID: userID,
        Target: types.LifecycleStateArchived,
        Actor:  actor,
        Reason: "GDPR retention period completed",
        Metadata: map[string]any{
            "export_path":   exportPath,
            "archived_at":   time.Now().UTC(),
            "retention_end": time.Now().UTC(),
        },
    })
}
```

## Error Handling

### Common Errors

```go
import "github.com/goliatone/go-users/command"

err := svc.Commands().UserLifecycleTransition.Execute(ctx, input)

switch {
case errors.Is(err, command.ErrLifecycleUserIDRequired):
    // Missing user ID
case errors.Is(err, command.ErrLifecycleTargetRequired):
    // Missing target state
case errors.Is(err, command.ErrActorRequired):
    // Missing actor reference
case errors.Is(err, types.ErrTransitionNotAllowed):
    // Invalid state transition per policy
default:
    // Repository or other error
}
```

### Handling Bulk Errors

```go
results := &[]command.BulkUserTransitionResult{}
err := svc.Commands().BulkUserTransition.Execute(ctx, input)

// err is a joined error of all failures
if err != nil {
    // Check individual results for details
    for _, r := range *results {
        if r.Err != nil {
            if errors.Is(r.Err, types.ErrTransitionNotAllowed) {
                log.Printf("User %s: invalid transition", r.UserID)
            } else {
                log.Printf("User %s: %v", r.UserID, r.Err)
            }
        }
    }
}
```

## Next Steps

- **[GUIDE_ROLES](GUIDE_ROLES.md)**: Assign roles to users based on lifecycle state
- **[GUIDE_ACTIVITY](GUIDE_ACTIVITY.md)**: Query and analyze lifecycle activity
- **[GUIDE_HOOKS](GUIDE_HOOKS.md)**: Advanced hook patterns for notifications
- **[GUIDE_MULTITENANCY](GUIDE_MULTITENANCY.md)**: Scope lifecycle operations by tenant
