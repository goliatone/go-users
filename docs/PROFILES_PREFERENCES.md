# Profiles & Preferences

Phase 5 introduces the profile and preference workflows that downstream panels (go-admin/go-cms) rely on for richer user data and scoped settings.

## Schema Recap

Migration `000004_profiles_preferences.sql` provisions:

| Table | Key Columns | Notes |
| ----- | ----------- | ----- |
| `user_profiles` | `user_id` (PK), `tenant_id`, `org_id`, `display_name`, `avatar_url`, `locale`, `timezone`, `bio`, `contact JSONB`, `metadata JSONB`, `created_by`, `updated_by` | One-to-one row per auth user. Scope columns default to all-zero UUIDs so single-tenant deployments can omit them. |
| `user_preferences` | `id` (PK), `user_id`, `tenant_id`, `org_id`, `scope_level`, `key`, `value JSONB`, `version`, `created_by`, `updated_by` | Scoped key/value store. `scope_level ∈ {system, tenant, org, user}`. Unique index on `(user_id, tenant_id, org_id, lower(key))` keeps rows idempotent. |

All UUID scope fields default to the zero UUID so legacy single-tenant deployments can skip new filters. Hosts should continue supplying tenant/org identifiers to prevent data leakage.

## Commands

| Command | Input | Validation |
| ------- | ----- | ---------- |
| `command.ProfileUpsert` | `ProfileUpsertInput{UserID, Scope, Actor, Patch types.ProfilePatch}`. Patch fields support pointer semantics so nil = no change, empty string = clear. `Patch.Contact`/`Patch.Metadata` replace the stored JSON blobs. | Requires `UserID` + `Actor`. Missing repo → `types.ErrMissingProfileRepository`. |
| `command.PreferenceUpsert` | `PreferenceUpsertInput{UserID, Scope, Level, Key, Value, Actor}`. `Level` defaults to `user`. `Value` must be a JSON-safe map. | Requires `Key`, `Value`, and `UserID` when `Level=user`. Emits `AfterPreferenceChange` with action `preference.upsert`. |
| `command.PreferenceDelete` | `PreferenceDeleteInput{UserID, Scope, Level, Key, Actor}`. | Same validation as upsert; emits `preference.delete`. |

`.Hooks.AfterProfileChange` and `.Hooks.AfterPreferenceChange` now receive strongly typed events so go-settings/go-notifications can invalidate caches or publish WebSocket updates immediately.

## Queries

| Query | Description |
| ----- | ----------- |
| `query.ProfileQuery` | `Query(ctx, ProfileQueryInput{UserID, Scope})` → `*types.UserProfile`. Returns `nil` when the profile has not been created. |
| `query.PreferenceQuery` | Uses the go-options based resolver to produce `types.PreferenceSnapshot{Effective map[string]any, Traces []PreferenceTrace}`. `PreferenceTrace` details how each scope contributed to the resolved value (scope level, tenant/org IDs, record snapshot ID, value, whether the layer supplied a value). |

## Preference Resolution

`preferences.Resolver` composes layers in the following order by default:

1. System defaults (`ResolverConfig.Defaults` + any `scope_level=system` rows)
2. Tenant rows (`scope_level=tenant`, keyed by `tenant_id`)
3. Org rows (`scope_level=org`, keyed by `(tenant_id, org_id)`)
4. User rows (`scope_level=user`, keyed by `(user_id, tenant_id, org_id)`).

Each layer becomes an `opts.Layer` so `ResolveWithTrace` can capture provenance:

```go
resolver, _ := preferences.NewResolver(preferences.ResolverConfig{
    Repository: prefRepo,                // Bun repository implements types.PreferenceRepository
    Defaults: map[string]any{
        "notifications.email": map[string]any{"enabled": false},
    },
})

snapshot, _ := resolver.Resolve(ctx, preferences.ResolveInput{
    UserID: userID,
    Scope:  types.ScopeFilter{TenantID: tenantID, OrgID: orgID},
    Keys:   []string{"notifications.email"},
})

snapshot.Effective["notifications.email"] // => map[enabled:true frequency:"daily"]
snapshot.Traces[0].Layers                 // => system → tenant → user layers, each with SnapshotID + Found flag
```

`query.PreferenceQuery` wraps this resolver so transports can serve provenanced preference payloads without importing go-options directly.

## Admin Payloads

*Profile form example* (PATCH semantics):

```json
{
  "display_name": "Alex Patel",
  "avatar_url": "https://cdn.example.com/avatars/alex.png",
  "bio": "Lifecycle owner for go-admin",
  "contact": {
    "email": "alex@example.com",
    "slack": "@alex"
  },
  "metadata": {
    "team": "admin-experience",
    "favorite_widgets": ["user_stats", "recent_activity"]
  }
}
```

*Preference upsert payload*:

```json
{
  "level": "user",
  "key": "notifications.email",
  "value": {
    "enabled": true,
    "frequency": "daily"
  }
}
```

*Resolved snapshot response* (`types.PreferenceSnapshot` rendered via HTTP):

```json
{
  "effective": {
    "notifications.email": {
      "enabled": true,
      "frequency": "daily"
    }
  },
  "traces": [
    {
      "key": "notifications.email",
      "layers": [
        {"level": "system", "snapshot_id": "", "found": true, "value": {"enabled": false}},
        {"level": "tenant", "snapshot_id": "f6e0...", "found": true, "value": {"frequency": "daily"}},
        {"level": "user", "snapshot_id": "d2ab...", "found": true, "value": {"enabled": true}}
      ]
    }
  ]
}
```

Transport handlers (REST, gRPC, CLI) should rely on the command/query surfaces above so multi-tenant enforcement and hook emission stay centralized inside go-users.
