# Activity Sink & Queries

Phase 4 ships the first-class activity sink and query surfaces so go-admin/go-cms can render audit feeds, lifecycle breakdowns, and other widgets without duplicating storage concerns.

## DI Contract

- The Activity sink interface lives in `pkg/types` as `ActivitySink` with the minimal method `Log(ctx, ActivityRecord) error`. Keep implementations limited to this contract so hosts can swap sinks (Bun, in-memory, streaming) without breaking changes.
- `ActivityRecord` is shared across sink and query layers; avoid extending the interface or moving it to keep compatibility for downstream modules.
- Use `activity.BuildRecordFromActor` to construct an `ActivityRecord` directly from the go-auth `ActorContext`, optionally tagging a channel via `activity.WithChannel`. The helper normalizes actor/tenant/org IDs and copies metadata to remain nil-safe.

## Verb/Object Conventions

Recommended verbs and object types for admin modules so dashboards and exports stay consistent:

- Settings: `settings.updated`, `settings.snapshot.created` with `object_type=settings` or `settings.snapshot`; metadata keys: `path`, `from`, `to`, `scope` (tenant/org), `version`.
- Media: `media.uploaded`, `media.variant.generated` with `object_type=media.asset` or `media.variant`; metadata: `asset_id`, `path`, `format`, `size_bytes`, `variant`.
- Export: `export.requested`, `export.completed`, `export.failed` with `object_type=export.job`; metadata: `job_id`, `format`, `count`, `status`, `error` (for failures).
- Bulk: `bulk.users.updated`, `bulk.roles.assigned` with `object_type=bulk.job`; metadata: `job_id`, `target`, `count_total`, `count_succeeded`, `count_failed`.

Metadata structure: prefer flat keys with clear units; always include `object_id` when relevant, `tenant_id`/`org_id` scope hints when the action affects multiple tenants, and counts for batch operations. Keep values JSON-serializable primitives or small maps to avoid bloating rows.

## Channel Guidance

Use the `channel` field for module-level filtering (e.g., `settings`, `media`, `export`, `bulk`). Keep channels lowercase, stable, and limited to the module name; reserve verb/object fields for finer-grained analysis. When emitting from shared helpers, set the channel via `activity.WithChannel("settings")`; omit it for legacy compatibility when a module tag is not needed.

## Enrichment Metadata

Write time enrichment stores stable display details in `ActivityRecord.Data` to avoid read-time lookups. These keys are flat by design to keep JSONB merges safe and queryable.

Reserved keys:
- `actor_display`, `actor_email`, `actor_id`, `actor_type`
- `object_display`, `object_type`, `object_id`, `object_deleted`
- `session_id`
- `enriched_at`, `enricher_version`

Invariants:
- Keys are flat and stable; do not repurpose them for other meanings.
- Values should be JSON-serializable primitives (string/bool) to keep indexing and merges safe.
- `enriched_at` is an RFC3339Nano string; update it only when new enrichment keys are added.
- Default `enricher_version` is `v1` (see `activity.DefaultEnricherVersion`); applications can override when wiring custom enrichers.

Data update semantics:
- Missing-key only: add keys that are absent in `data`.
- Never overwrite user-provided metadata keys by default.
- Refresh `enriched_at` only when a new enrichment key is written.

Write-time enrichment modes (configurable):
- `none`: no enrichment at write time.
- `wrapper`: enrich via `activity.EnrichedSink` before persistence.
- `repo`: enrich inside the repository `Log` hook.
- `hybrid`: wrapper enriches app-specific metadata (like `session_id`), then the repo enriches actor/object fields.

Enablement hooks live on `service.Config`:
- `ActivityEnricher`, `ActivityEnrichmentStore`
- `EnrichmentEnabled`, `EnrichmentScope`, `EnrichmentWriteMode`
- Optional error handling via `ActivityEnrichmentErrorStrategy` or `ActivityEnrichmentErrorHandler`

Object display rules:
- Use a stable, human-readable format per object type (avoid ephemeral values).
- Unknown types should fall back to `object_type:object_id`.
- When an object is deleted, set `object_deleted = true` and keep the last known `object_display` when available (otherwise use the fallback format).

Session ID extraction order (deterministic):
1. JWT `jti` claim (stable per session)
2. `claims.Metadata["session_id"]`
3. `auth.ActorContext.Metadata["session_id"]`

Backfill job (cron command):
- Command lives at `command/activity_enrichment` and can be scheduled via `EnrichmentJobSchedule`.
- Config inputs: `Schedule` (default `0 * * * *`), `BatchSize`, `EnrichedAtCutoff` (duration), and optional `Scope`.
- Start with an hourly schedule for initial backfill, then reduce to daily once the backlog is cleared.
- The job only merges missing keys and refreshes `enriched_at` so it is safe to run repeatedly.

## Role-Aware Access Policy & Sanitization

Use the role-aware helper and access policy when exposing activity feeds to admin clients:

- `activity.BuildFilterFromActor` enforces scope and role visibility (non-admins are forced to self-only, admins stay tenant/org scoped, and superadmins can optionally widen scope).
- `activity.ActivityAccessPolicy` provides a standard interface for applying filters and sanitizing records.
- `activity.NewDefaultAccessPolicy` masks sensitive data via go-masker and redacts IPs for non-superadmins by default.

```go
policy := activity.NewDefaultAccessPolicy(
    activity.WithPolicyFilterOptions(
        activity.WithChannelDenylist("system"),
        activity.WithMachineActivityEnabled(false),
        activity.WithSuperadminScope(true),
    ),
)

feedQuery := query.NewActivityFeedQuery(store, scopeGuard, query.WithActivityAccessPolicy(policy))
statsQuery := query.NewActivityStatsQuery(store, scopeGuard, query.WithActivityAccessPolicy(policy))
```

Defaults:
- Superadmin role aliases: `system_admin`, `superadmin`
- Admin role aliases: `tenant_admin`, `admin`, `org_admin`
- Machine activity markers: actor types (`system`, `machine`, `job`, `task`) and data keys (`system`, `machine`)

The default policy also drops `Data` for support roles and allows custom maskers or IP redaction toggles via `WithPolicyMasker` and `WithIPRedaction`.

## Repository & Index Guidance

Current SQLite/Postgres schema ships indexes for:

- `(tenant_id, org_id, created_at)` – tenant/org scoped feeds.
- `(user_id, created_at)` – user-specific timelines.
- `(object_type, object_id)` – object drill-downs.
- `verb` – verb-specific filtering.

Queries: prefer `ActivityFilter` and `ActivityStatsFilter` to leverage these indexes. When filtering by channel in high-volume modules, add an optional composite index `(tenant_id, channel, created_at)` (or `(org_id, channel, created_at)` for org-scoped deployments). For metadata-heavy use cases, Postgres consumers can add a GIN index on `data` to accelerate keyword searches.

Enrichment filter indexing (Postgres):
- Missing-key scans: `CREATE INDEX ON user_activity ((data->>'actor_display'));`
- Object display scans: `CREATE INDEX ON user_activity ((data->>'object_display'));`
- Staleness scans: `CREATE INDEX ON user_activity (((data->>'enriched_at')::timestamptz));`
- For broader JSONB predicates, add `CREATE INDEX ON user_activity USING GIN (data);`

Retention & archiving guidance for high-volume deployments:

- TTL: pick a retention window (e.g., 90 or 180 days) and schedule a daily purge for older records.
- Partitioning: in Postgres, range-partition by month on `created_at` and drop old partitions to purge efficiently.
- Archive: copy old partitions to cold storage (S3, BigQuery, or an archive DB) before deletion if audit trails require it.
- Maintenance: run `VACUUM`/`ANALYZE` after large deletes to keep query plans stable.

## Optional Cursor Pagination Helper

For high-volume feeds, consumers can use the cursor helper without changing repository interfaces:

```go
cursor := &activity.ActivityCursor{
    OccurredAt: lastRecord.OccurredAt,
    ID:         lastRecord.ID,
}

var rows []activity.LogEntry
err := activity.ApplyCursorPagination(db.NewSelect().Model(&rows), cursor, 50).Scan(ctx)
```

The helper orders by `created_at DESC, id DESC` and returns rows older than the cursor.

## Integration & Examples

Inject sinks and emit module-aligned records via helpers:

```go
store, _ := activity.NewRepository(activity.RepositoryConfig{DB: bunDB})
cfg := users.Config{ActivitySink: store, ActivityRepository: store /* other deps */}
svc := users.New(cfg)

actor := auth.ActorContext{ActorID: adminID.String(), TenantID: tenantID.String()}
rec, _ := activity.BuildRecordFromActor(&actor, "settings.updated", "settings", "global", map[string]any{"path": "feature.flags"}, activity.WithChannel("settings"))
_ = svc.ActivitySink.Log(ctx, rec)
```

Module examples (verbs/objects align with the conventions above):

- Settings: `BuildRecordFromActor(..., "settings.updated", "settings", "global", {"path": "ui.theme", "from": "light", "to": "dark"}, WithChannel("settings"))`
- Media: `BuildRecordFromActor(..., "media.uploaded", "media.asset", assetID, {"path": "/uploads/img.png", "size_bytes": 2048}, WithChannel("media"))`
- Export: `BuildRecordFromActor(..., "export.completed", "export.job", jobID, {"format": "csv", "count": 120}, WithChannel("export"))`
- Bulk: `BuildRecordFromActor(..., "bulk.users.updated", "bulk.job", jobID, {"count_total": 500, "count_succeeded": 498}, WithChannel("bulk"))`

## Schema

Migration `000003_user_activity.sql` provisions the `user_activity` table with:

| Column      | Description                                      |
| ----------- | ------------------------------------------------ |
| `id`        | TEXT primary key (UUID string) generated by the sink |
| `user_id`   | Optional user UUID string referenced by the event |
| `actor_id`  | UUID string of the admin/process that triggered action |
| `tenant_id` | Tenant scope (defaults to all-zero UUID string)  |
| `org_id`    | Org scope (defaults to all-zero UUID string)     |
| `verb`      | Namespaced verb (`user.lifecycle.transition`)    |
| `object_type` / `object_id` | Subject of the action            |
| `channel`   | Source (“lifecycle”, “roles”, “preferences”)     |
| `ip`        | Optional IP address                              |
| `data`      | JSON payload with arbitrary metadata             |
| `created_at`| Timestamp assigned by the sink                   |

Indexes exist for `(tenant_id, org_id, created_at)`, `(user_id, created_at)`, `(object_type, object_id)`, and `verb` so dashboard filters remain fast. Postgres deployers can add a GIN index on `data` if they need richer querying.

## Default Bun Repository

`activity.NewRepository` returns a struct that implements both `types.ActivitySink` and `types.ActivityRepository`.

```go
store, err := activity.NewRepository(activity.RepositoryConfig{
    DB:    bunDB,
    Clock: types.SystemClock{},
    IDGen: types.UUIDGenerator{},
})
if err != nil {
    log.Fatal(err)
}

svc := users.New(users.Config{
    ActivitySink:       store,
    ActivityRepository: store,
    // other dependencies...
})
```

- `Log(ctx, record)` persists entries and automatically fills `ID`/`OccurredAt` when omitted.
- `ListActivity(ctx, filter)` powers feed-style pagination with scope, verb, keyword, and time-range filters.
- `ActivityStats(ctx, filter)` aggregates counts per verb for widgets like `admin.widget.user_stats`.

Because the repository satisfies both interfaces, most hosts only need to construct it once and wire it into the service configuration.

## Queries

The `query` package now exposes:

- `ActivityFeed` → paginated feed DTOs (`types.ActivityPage`) suitable for recent activity panels.
- `ActivityStatsQuery` → grouped counts per verb (`types.ActivityStats`).

Example:

```go
feed, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Scope:      types.ScopeFilter{TenantID: tenantID},
    Verbs:      []string{"user.lifecycle.transition"},
    Pagination: types.Pagination{Limit: 20},
})
stats, _ := svc.Queries().ActivityStats.Query(ctx, types.ActivityStatsFilter{
    Scope: types.ScopeFilter{TenantID: tenantID},
})
```

Filters accept optional `UserID`, `ActorID`, `Channel`, `Channels`, `ChannelDenylist`,
`Since`, `Until`, and keyword matching (`verb`, `object_type`, `object_id`).

## Hooks and Commands

- Commands (lifecycle, roles, invites, password reset) already log structured events via the injected sink. After persistence, they trigger `Hooks.AfterActivity`, allowing apps to push WebSocket notifications or replicate events elsewhere.
- `command.ActivityLog` provides a reusable command that other modules can invoke to log custom verbs while still benefiting from hooks and storage.

## Testing & Extensibility

- `activity/bun_repository_test.go` exercises the Bun repository against SQLite.
- `command` integration tests wire lifecycle commands → activity sink → queries to ensure data flows correctly.
- Hosts can swap the storage layer by implementing `types.ActivitySink` and `types.ActivityRepository` (e.g., streaming to Kafka or using an external analytics DB).

Keep DTOs tenant-aware and prefer `types.ActivityFilter`/`types.ActivityStatsFilter` when exposing new transport endpoints so multi-tenant scoping remains enforced.
