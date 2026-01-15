# Activity Logging Guide

This guide covers the activity logging system in `go-users`, including building activity records, logging to sinks, querying activity feeds, and best practices for audit trails.

## Overview

The activity system provides:

- **Audit logging** for compliance and accountability
- **Activity feeds** for dashboards and user timelines
- **Flexible filtering** by scope, verb, channel, and time range
- **Statistics aggregation** for metrics and reporting
- **Hooks** for real-time notifications and integrations

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Activity Sources                         │
├─────────────────────────────────────────────────────────────┤
│  Lifecycle Commands  │  Role Commands  │  Custom Events    │
└──────────┬───────────┴────────┬────────┴────────┬──────────┘
           │                    │                 │
           ▼                    ▼                 ▼
┌─────────────────────────────────────────────────────────────┐
│                      ActivitySink                           │
│              Log(ctx, ActivityRecord) error                 │
└──────────────────────────┬──────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────┐
│                   ActivityRepository                        │
├─────────────────────────────────────────────────────────────┤
│  ListActivity(ctx, filter) → ActivityPage                   │
│  ActivityStats(ctx, filter) → ActivityStats                 │
└─────────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────┐
│                    user_activity Table                      │
├─────────────────────────────────────────────────────────────┤
│  id, user_id, actor_id, verb, object_type, object_id,      │
│  channel, ip, data (JSON), tenant_id, org_id, created_at   │
└─────────────────────────────────────────────────────────────┘
```

## Activity Record Structure

```go
type ActivityRecord struct {
    ID         uuid.UUID      // Auto-generated if empty
    UserID     uuid.UUID      // Target user (optional)
    ActorID    uuid.UUID      // Who performed the action
    Verb       string         // Action name (e.g., "user.lifecycle.transition")
    ObjectType string         // Subject type (e.g., "user", "role")
    ObjectID   string         // Subject identifier
    Channel    string         // Module tag for filtering
    IP         string         // Actor's IP address (optional)
    TenantID   uuid.UUID      // Scope
    OrgID      uuid.UUID      // Scope
    Data       map[string]any // Action-specific metadata
    OccurredAt time.Time      // Auto-set if empty
}
```

## Building Activity Records

### From HTTP Requests (with go-auth)

Use `BuildRecordFromActor` when you have a go-auth `ActorContext` from middleware:

```go
import (
    "github.com/goliatone/go-auth"
    "github.com/goliatone/go-users/activity"
)

// In your HTTP handler
func handleSettingsUpdate(ctx context.Context, actor *auth.ActorContext) error {
    record, err := activity.BuildRecordFromActor(
        actor,
        "settings.updated",           // verb
        "settings",                   // objectType
        "global",                     // objectID
        map[string]any{               // metadata
            "path": "ui.theme",
            "from": "light",
            "to":   "dark",
        },
        activity.WithChannel("settings"),
    )
    if err != nil {
        return err
    }

    return svc.ActivitySink().Log(ctx, record)
}
```

### From Background Jobs (UUID only)

Use `BuildRecordFromUUID` when you only have an actor UUID:

```go
import "github.com/goliatone/go-users/activity"

func processExportJob(ctx context.Context, actorID uuid.UUID, jobID string) error {
    record, err := activity.BuildRecordFromUUID(
        actorID,
        "export.completed",           // verb
        "export.job",                 // objectType
        jobID,                        // objectID
        map[string]any{               // metadata
            "format": "csv",
            "count":  120,
            "status": "success",
        },
        activity.WithChannel("export"),
        activity.WithTenant(tenantID),
        activity.WithOrg(orgID),
        activity.WithOccurredAt(startedAt),
    )
    if err != nil {
        return err
    }

    return svc.ActivitySink().Log(ctx, record)
}
```

## Record Options

### WithChannel

Tag the activity with a module name for filtering:

```go
activity.WithChannel("settings")
activity.WithChannel("media")
activity.WithChannel("export")
activity.WithChannel("bulk")
```

### WithTenant / WithOrg

Set scope when not available from the actor context:

```go
activity.WithTenant(tenantID)
activity.WithOrg(orgID)
```

### WithOccurredAt

Override the timestamp (useful for batch imports or delayed processing):

```go
activity.WithOccurredAt(time.Now().Add(-1 * time.Hour))
```

## Logging via ActivitySink

### Direct Logging

```go
record := types.ActivityRecord{
    ActorID:    actorID,
    Verb:       "document.viewed",
    ObjectType: "document",
    ObjectID:   documentID.String(),
    TenantID:   tenantID,
    Data: map[string]any{
        "title":    "Q4 Report",
        "duration": "45s",
    },
}

err := svc.ActivitySink().Log(ctx, record)
```

### Using the LogActivity Command

For command-pattern consistency with hooks:

```go
err := svc.Commands().LogActivity.Execute(ctx, command.ActivityLogInput{
    Record: types.ActivityRecord{
        ActorID:    actorID,
        Verb:       "report.generated",
        ObjectType: "report",
        ObjectID:   reportID.String(),
        Channel:    "reports",
        Data: map[string]any{
            "type":   "monthly",
            "format": "pdf",
        },
    },
})
```

The command triggers `Hooks.AfterActivity` after logging.

## Querying Activity Feeds

### Basic Feed Query

```go
feed, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor:      actor,
    Scope:      types.ScopeFilter{TenantID: tenantID},
    Pagination: types.Pagination{Limit: 20},
})
if err != nil {
    return err
}

fmt.Printf("Found %d records (total: %d)\n", len(feed.Records), feed.Total)
for _, record := range feed.Records {
    fmt.Printf("[%s] %s %s/%s\n",
        record.OccurredAt.Format("15:04:05"),
        record.Verb,
        record.ObjectType,
        record.ObjectID,
    )
}
```

### Filter by Verbs

```go
feed, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor: actor,
    Verbs: []string{
        "user.lifecycle.transition",
        "user.invite",
        "user.password.reset",
    },
    Pagination: types.Pagination{Limit: 50},
})
```

### Filter by Channel

```go
// Get all settings-related activity
feed, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor:   actor,
    Channel: "settings",
})

// Get all export jobs
feed, err = svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor:   actor,
    Channel: "export",
})
```

For multi-channel feeds, use `Channels` for an allowlist and `ChannelDenylist`
to exclude channels after allow filtering.

```go
feed, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor:           actor,
    Channels:        []string{"settings", "roles", "export"},
    ChannelDenylist: []string{"export"},
})
```

If both `Channels` and `Channel` are set, `Channels` wins. `ChannelDenylist` is
always applied after allow filtering.

### Filter by Object

```go
// Activity for a specific user
feed, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor:      actor,
    ObjectType: "user",
    ObjectID:   userID.String(),
})

// Activity for a specific role
feed, err = svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor:      actor,
    ObjectType: "role",
    ObjectID:   roleID.String(),
})
```

### Filter by Actor or User

```go
// Activity performed by a specific actor
feed, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor:   actor,
    ActorID: adminUserID,
})

// Activity affecting a specific user
feed, err = svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor:  actor,
    UserID: targetUserID,
})
```

### Filter by Time Range

```go
since := time.Now().Add(-24 * time.Hour)
until := time.Now()

feed, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor: actor,
    Since: &since,
    Until: &until,
})
```

### Keyword Search

```go
feed, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor:   actor,
    Keyword: "password",  // Searches verb, object_type, object_id
})
```

### Pagination

```go
// First page
page1, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor:      actor,
    Pagination: types.Pagination{Limit: 20, Offset: 0},
})

// Next page
if page1.HasMore {
    page2, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
        Actor:      actor,
        Pagination: types.Pagination{Limit: 20, Offset: page1.NextOffset},
    })
}
```

### Role-aware access policy

For admin-facing feeds, attach the default access policy to enforce role-aware
visibility and sanitize payloads:

```go
policy := activity.NewDefaultAccessPolicy(
    activity.WithPolicyFilterOptions(
        activity.WithMachineActivityEnabled(false),
        activity.WithChannelDenylist("system"),
        activity.WithSuperadminScope(true),
    ),
)

feedQuery := query.NewActivityFeedQuery(store, scopeGuard, query.WithActivityAccessPolicy(policy))
feed, err := feedQuery.Query(ctx, types.ActivityFilter{
    Actor:      actor,
    Pagination: types.Pagination{Limit: 25},
})
```

If you are using the `users.Service` helpers, build a policy-aware query where
you wire your handlers (the default service queries do not attach a policy).

Defaults treat `system_admin`/`superadmin` as superadmins and `tenant_admin`/`admin`/`org_admin`
as admins (override with `WithRoleAliases`). The policy masks sensitive data via go-masker
and redacts IPs for non-superadmins; customize with `WithPolicyMasker` or `WithIPRedaction`.

### Cursor pagination (optional)

For high-volume feeds, use the cursor helper (orders by `created_at DESC, id DESC`):

```go
cursor := &activity.ActivityCursor{
    OccurredAt: page1.Records[len(page1.Records)-1].OccurredAt,
    ID:         page1.Records[len(page1.Records)-1].ID,
}

var rows []activity.LogEntry
err := activity.ApplyCursorPagination(db.NewSelect().Model(&rows), cursor, 50).Scan(ctx)
```

## Activity Statistics

### Basic Stats

```go
stats, err := svc.Queries().ActivityStats.Query(ctx, types.ActivityStatsFilter{
    Actor: actor,
    Scope: types.ScopeFilter{TenantID: tenantID},
})
if err != nil {
    return err
}

fmt.Printf("Total activities: %d\n", stats.Total)
fmt.Println("By verb:")
for verb, count := range stats.ByVerb {
    fmt.Printf("  %s: %d\n", verb, count)
}
```

### Stats with Time Range

```go
since := time.Now().Add(-7 * 24 * time.Hour) // Last 7 days

stats, err := svc.Queries().ActivityStats.Query(ctx, types.ActivityStatsFilter{
    Actor: actor,
    Scope: types.ScopeFilter{TenantID: tenantID},
    Since: &since,
})
```

### Stats for Specific Verbs

```go
stats, err := svc.Queries().ActivityStats.Query(ctx, types.ActivityStatsFilter{
    Actor: actor,
    Verbs: []string{"user.lifecycle.transition", "user.invite"},
})
```

## Verb/Object Naming Conventions

### Standard Verbs

Follow the `resource.action` or `resource.subresource.action` pattern:

| Category | Verbs | Object Type |
|----------|-------|-------------|
| **Lifecycle** | `user.lifecycle.transition`, `user.invite`, `user.password.reset` | `user` |
| **Roles** | `role.created`, `role.updated`, `role.deleted`, `role.assigned`, `role.unassigned` | `role` |
| **Settings** | `settings.updated`, `settings.snapshot.created` | `settings`, `settings.snapshot` |
| **Media** | `media.uploaded`, `media.variant.generated`, `media.deleted` | `media.asset`, `media.variant` |
| **Export** | `export.requested`, `export.completed`, `export.failed` | `export.job` |
| **Bulk** | `bulk.users.updated`, `bulk.roles.assigned` | `bulk.job` |

### Metadata Best Practices

Keep metadata flat and JSON-serializable:

```go
// Good - flat structure with clear keys
metadata := map[string]any{
    "from_state":   "pending",
    "to_state":     "active",
    "reason":       "Email verified",
    "count_total":  100,
    "count_failed": 2,
}

// Avoid - deeply nested structures
metadata := map[string]any{
    "details": map[string]any{
        "transition": map[string]any{
            "from": "pending",
            "to":   "active",
        },
    },
}
```

### Channel Categories

Use lowercase, stable channel names:

| Channel | Use Case |
|---------|----------|
| `lifecycle` | User state transitions |
| `invites` | User invitations |
| `password` | Password operations |
| `roles` | Role management |
| `settings` | Application settings |
| `media` | Media uploads and processing |
| `export` | Data exports |
| `bulk` | Batch operations |

## Activity Hooks

React to activity events for real-time notifications:

```go
svc := users.New(users.Config{
    AuthRepository: repo,
    ActivitySink:   activityStore,
    Hooks: types.Hooks{
        AfterActivity: func(ctx context.Context, record types.ActivityRecord) {
            log.Printf("Activity: %s %s/%s", record.Verb, record.ObjectType, record.ObjectID)

            // Push to WebSocket for real-time updates
            broadcastActivity(record)

            // Send to analytics
            trackEvent(record)

            // Trigger webhooks for specific verbs
            if strings.HasPrefix(record.Verb, "user.") {
                sendWebhook("user_activity", record)
            }
        },
    },
    // ...
})
```

## Common Patterns

### User Activity Timeline

```go
func getUserTimeline(ctx context.Context, svc *users.Service, userID uuid.UUID, actor types.ActorRef, limit int) ([]types.ActivityRecord, error) {
    feed, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
        Actor:  actor,
        UserID: userID,
        Pagination: types.Pagination{Limit: limit},
    })
    if err != nil {
        return nil, err
    }
    return feed.Records, nil
}
```

### Admin Audit Log

```go
func getAuditLog(ctx context.Context, svc *users.Service, actor types.ActorRef, since time.Time) ([]types.ActivityRecord, error) {
    feed, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
        Actor: actor,
        Verbs: []string{
            "user.lifecycle.transition",
            "role.assigned",
            "role.unassigned",
            "settings.updated",
        },
        Since:      &since,
        Pagination: types.Pagination{Limit: 100},
    })
    if err != nil {
        return nil, err
    }
    return feed.Records, nil
}
```

### Activity Dashboard Widget

```go
type DashboardStats struct {
    TotalUsers       int
    ActiveToday      int
    RecentActivities []types.ActivityRecord
    ActivityByVerb   map[string]int
}

func getDashboardStats(ctx context.Context, svc *users.Service, actor types.ActorRef, tenantID uuid.UUID) (*DashboardStats, error) {
    today := time.Now().Truncate(24 * time.Hour)

    // Get recent activity
    feed, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
        Actor:      actor,
        Scope:      types.ScopeFilter{TenantID: tenantID},
        Pagination: types.Pagination{Limit: 10},
    })
    if err != nil {
        return nil, err
    }

    // Get stats for today
    stats, err := svc.Queries().ActivityStats.Query(ctx, types.ActivityStatsFilter{
        Actor: actor,
        Scope: types.ScopeFilter{TenantID: tenantID},
        Since: &today,
    })
    if err != nil {
        return nil, err
    }

    return &DashboardStats{
        RecentActivities: feed.Records,
        ActivityByVerb:   stats.ByVerb,
    }, nil
}
```

### Logging Custom Module Events

```go
func logMediaUpload(ctx context.Context, svc *users.Service, actor *auth.ActorContext, assetID uuid.UUID, path string, sizeBytes int64) error {
    record, err := activity.BuildRecordFromActor(
        actor,
        "media.uploaded",
        "media.asset",
        assetID.String(),
        map[string]any{
            "path":       path,
            "size_bytes": sizeBytes,
            "mime_type":  "image/png",
        },
        activity.WithChannel("media"),
    )
    if err != nil {
        return err
    }
    return svc.ActivitySink().Log(ctx, record)
}

func logBulkOperation(ctx context.Context, svc *users.Service, actorID, tenantID uuid.UUID, jobID string, total, succeeded, failed int) error {
    record, err := activity.BuildRecordFromUUID(
        actorID,
        "bulk.users.updated",
        "bulk.job",
        jobID,
        map[string]any{
            "count_total":     total,
            "count_succeeded": succeeded,
            "count_failed":    failed,
        },
        activity.WithChannel("bulk"),
        activity.WithTenant(tenantID),
    )
    if err != nil {
        return err
    }
    return svc.ActivitySink().Log(ctx, record)
}
```

## Repository Setup

### Bun Repository (Production)

```go
import (
    "github.com/goliatone/go-users/activity"
    "github.com/uptrace/bun"
)

func setupActivityRepository(db *bun.DB) (*activity.Repository, error) {
    return activity.NewRepository(activity.RepositoryConfig{
        DB:    db,
        Clock: types.SystemClock{},
        IDGen: types.UUIDGenerator{},
    })
}

// Wire into service
activityStore, _ := setupActivityRepository(bunDB)
svc := users.New(users.Config{
    AuthRepository:     authRepo,
    ActivitySink:       activityStore,
    ActivityRepository: activityStore,
    // ...
})
```

The Bun repository implements both `ActivitySink` (for logging) and `ActivityRepository` (for querying).

### Database Schema

The `user_activity` table includes indexes for efficient querying:

```sql
-- Tenant/org scoped feeds
CREATE INDEX idx_activity_scope ON user_activity(tenant_id, org_id, created_at DESC);

-- User-specific timelines
CREATE INDEX idx_activity_user ON user_activity(user_id, created_at DESC);

-- Object drill-downs
CREATE INDEX idx_activity_object ON user_activity(object_type, object_id);

-- Verb filtering
CREATE INDEX idx_activity_verb ON user_activity(verb);
```

For high-volume deployments, consider adding:

```sql
-- Channel filtering
CREATE INDEX idx_activity_channel ON user_activity(tenant_id, channel, created_at DESC);

-- Metadata search (PostgreSQL only)
CREATE INDEX idx_activity_data ON user_activity USING GIN(data);
```

### Retention & Archiving

Activity tables grow quickly. Plan a retention strategy early:

- TTL: pick a window (e.g., 90/180 days) and schedule a daily purge of older rows.
- Partitioning: in Postgres, range-partition by `created_at` (monthly) and drop old partitions for fast deletes.
- Archive: copy expired partitions to cold storage or an audit warehouse before deletion if required.
- Maintenance: run `VACUUM`/`ANALYZE` after large purges to keep query plans stable.

## Error Handling

```go
err := svc.ActivitySink().Log(ctx, record)
if err != nil {
    // Log but don't fail the main operation
    log.Printf("Failed to log activity: %v", err)
}

// For queries
feed, err := svc.Queries().ActivityFeed.Query(ctx, filter)
if err != nil {
    if errors.Is(err, types.ErrMissingActivityRepository) {
        // Activity repository not configured
    }
    if errors.Is(err, types.ErrActorRequired) {
        // Missing actor in filter
    }
    return err
}
```

## Next Steps

- **[GUIDE_MULTITENANCY](GUIDE_MULTITENANCY.md)**: Scope activity by tenant/organization
- **[GUIDE_HOOKS](GUIDE_HOOKS.md)**: Advanced hook patterns for activity events
- **[GUIDE_QUERIES](GUIDE_QUERIES.md)**: Query patterns for activity dashboards
- **[GUIDE_REPOSITORIES](GUIDE_REPOSITORIES.md)**: Custom activity sink implementations
