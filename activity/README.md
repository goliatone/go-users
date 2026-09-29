# Activity module

Audit logging helpers, a Bun-backed sink/repository, and query helpers for recent activity feeds and stats.

## Components
- `BuildRecordFromActor` and `BuildRecordFromUUID` convert request context into `types.ActivityRecord` with trimmed fields and cloned metadata.
- Bun repository (`activity.NewRepository`) implements both `types.ActivitySink` and `types.ActivityRepository` for writes and reads.
- Query handlers under `query` consume `types.ActivityFilter`/`ActivityStatsFilter` and respect tenant/org scope.

## Constructing records
Use `BuildRecordFromActor` when you have a go-auth `ActorContext` (HTTP middleware); use `BuildRecordFromUUID` when you only have an actor UUID (background jobs, message handlers).

```go
// From a request with go-auth metadata.
rec, err := activity.BuildRecordFromActor(actorCtx,
    "settings.updated",
    "settings",
    "global",
    map[string]any{"path": "ui.theme", "from": "light", "to": "dark"},
    activity.WithChannel("settings"),
)

// From a background worker with only an actor UUID.
rec, err := activity.BuildRecordFromUUID(actorID,
    "export.completed",
    "export.job",
    jobID,
    map[string]any{"format": "csv", "count": 120},
    activity.WithTenant(tenantID),
    activity.WithOrg(orgID),
    activity.WithOccurredAt(startedAt),
)
```

Record options:
- `WithChannel(string)`: module-level filter tag.
- `WithTenant(uuid.UUID)`: override tenant scope when not present in the actor context.
- `WithOrg(uuid.UUID)`: override org scope.
- `WithOccurredAt(time.Time)`: set a deterministic timestamp (defaults to `time.Now().UTC()`).

## Wiring the sink/repository

```go
store, err := activity.NewRepository(activity.RepositoryConfig{
    DB:    bunDB,
    Clock: types.SystemClock{},
    IDGen: types.UUIDGenerator{},
})
if err != nil {
    return err
}

svc := users.New(users.Config{
    ActivitySink:       store,
    ActivityRepository: store,
    // other dependencies...
})

if err := svc.ActivitySink.Log(ctx, rec); err != nil {
    return err
}
```

`Log` fills `ID`/`OccurredAt` when missing and persists to the `user_activity` table created by migration `000003_user_activity.sql`.

## Host-owned record policy and authentication events

`RecordPolicy` is an opt-in boundary for audit disclosure. Its zero configuration
removes metadata and IP; it does not change `DefaultMasker` or access-policy
behavior. Supply the same policy to ordinary writes, hooks, transactional writes
and reads. The metadata callback owns the application's allowlist/redaction.

```go
policy := activity.NewRecordPolicy(activity.RecordPolicyConfig{
    DataSanitizer: func(record types.ActivityRecord) map[string]any {
        // Return only the host's approved metadata. Do not forward arbitrary input.
        outcome, _ := record.Data["outcome"].(string)
        return map[string]any{"outcome": outcome}
    },
    RetainFailedLoginIdentifier: true, // explicit disclosure decision
})
sink := activity.SanitizingSink{Next: store, Policy: policy}
hooks = policy.SanitizeHooks(hooks) // only AfterActivity is decorated

// adapter/goauth imports as goauth; no identity lookup is performed.
authSink, err := goauth.NewActivitySink(goauth.ActivitySinkConfig{
    Sink: sink,
    Scope: types.ScopeFilter{TenantID: tenantID, OrgID: orgID},
    AnonymousActorID: authenticationGatewayID,
    RetainFailedLoginIdentifier: true,
})
if err != nil { return err }
authenticator.WithActivitySink(authSink)
```

The adapter requires nonzero tenant, organization and anonymous-actor UUIDs. It
maps known event IDs and canonical actions, outcome, target and status fields,
without copying arbitrary event metadata or provider errors. Identifier capture
is disabled by default. Enabling it on both boundaries preserves only
`attempted_identifier` for `auth.login.failure` in `authentication`: string input,
controls/format characters removed, trimmed, case preserved and at most 254
Unicode code points. Render it as escaped, **unverified attempted input**.

For a transaction, call `policy.Sanitize(record)` before mapping to `LogEntry`
and invoking `CreateTx` on the caller's transaction; the policy never starts or
commits a transaction. For reads, call `policy.Sanitize(record)` after access and
scope enforcement. A separate read policy may use `SanitizeRecord` as its base
callback while enabling the same identifier exception. Callbacks receive detached
JSON metadata; returned data is detached again before forwarding. Nil policies
fail closed; missing sink destinations return errors and downstream errors
propagate. No retries or deduplication are added.

These helpers do not authorize readers. Hosts must retain trusted scope and
permission checks, lifecycle/logger redaction, response cache policy and their
own retention decision. Repository methods remain persistence-only and do not
implicitly sanitize arbitrary direct writes.

## Queries

```go
feed, _ := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Scope:      types.ScopeFilter{TenantID: tenantID},
    Channel:    "settings",
    Pagination: types.Pagination{Limit: 20},
})

stats, _ := svc.Queries().ActivityStats.Query(ctx, types.ActivityStatsFilter{
    Scope: types.ScopeFilter{TenantID: tenantID},
})
```

Filters include optional `ActorID`, `UserID`, `ObjectType`, `ObjectID`, `Verb`, `Channel`,
`Channels`, `ChannelDenylist`, `Since`, and `Until`.

## Role-aware filtering & sanitization

Use `BuildFilterFromActor` for role-aware filters or attach the default access
policy to activity queries:

```go
policy := activity.NewDefaultAccessPolicy(
    activity.WithPolicyFilterOptions(
        activity.WithChannelAllowlist("settings", "roles"),
        activity.WithMachineActivityEnabled(false),
        activity.WithSuperadminScope(true),
    ),
)

feedQuery := query.NewActivityFeedQuery(store, scopeGuard, query.WithActivityAccessPolicy(policy))
feed, _ := feedQuery.Query(ctx, types.ActivityFilter{
    Actor:      actorRef,
    Pagination: types.Pagination{Limit: 25},
})
```

Defaults treat `system_admin`/`superadmin` as superadmins and `tenant_admin`/`admin`/`org_admin`
as admins; you can override with `WithRoleAliases`. Sanitization uses go-masker defaults and
redacts IPs for non-superadmins by default.

## Optional cursor pagination

For high-volume feeds, the cursor helper can paginate by `created_at` and `id`:

```go
cursor := &activity.ActivityCursor{
    OccurredAt: lastRecord.OccurredAt,
    ID:         lastRecord.ID,
}
query := activity.ApplyCursorPagination(db.NewSelect().Model(&rows), cursor, 50)
```

## Conventions
- Verbs/objects: `settings.updated` (`settings`), `export.completed` (`export.job`), `bulk.users.updated` (`bulk.job`), `media.uploaded` (`media.asset`).
- Channels: lowercase module names (`settings`, `export`, `bulk`, `media`) for dashboard filtering.
- Metadata: flat, JSON-serializable keys; include counts and scope hints when relevant.

See `docs/ACTIVITY.md` for deeper guidance, indexes, and schema details.
