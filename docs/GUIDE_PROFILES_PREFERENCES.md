# Profiles & Preferences Guide

This guide covers managing user profiles and scoped preferences in `go-users`. Learn how to store rich user data and implement hierarchical settings that cascade across system, tenant, organization, and user levels.

## Table of Contents

- [Overview](#overview)
- [Profiles](#profiles)
  - [Profile Fields](#profile-fields)
  - [Profile vs Core User Fields](#profile-vs-core-user-fields)
  - [Upserting Profiles](#upserting-profiles)
  - [Querying Profiles](#querying-profiles)
- [Preferences](#preferences)
  - [Scope Levels](#scope-levels)
  - [Preference Resolution and Inheritance](#preference-resolution-and-inheritance)
  - [Managing Preferences](#managing-preferences)
  - [Querying Preferences](#querying-preferences)
  - [Preference Traces](#preference-traces)
- [The Preference Resolver](#the-preference-resolver)
- [Version Tracking](#version-tracking)
- [Building Settings UIs](#building-settings-uis)
- [Common Patterns](#common-patterns)
- [Error Handling](#error-handling)
- [Next Steps](#next-steps)

---

## Overview

`go-users` separates user data into three distinct concerns:

```
┌──────────────────────────────────────────────────────────────┐
│                        User Data                             │
├──────────────────┬──────────────────┬────────────────────────┤
│   Core User      │     Profile      │     Preferences        │
│   (go-auth)      │   (go-users)     │     (go-users)         │
├──────────────────┼──────────────────┼────────────────────────┤
│ - ID             │ - DisplayName    │ - Theme settings       │
│ - Email          │ - AvatarURL      │ - Notification prefs   │
│ - Password hash  │ - Locale         │ - Feature flags        │
│ - Status         │ - Timezone       │ - UI customization     │
│ - Role           │ - Bio            │ - Application config   │
│                  │ - Contact info   │                        │
│                  │ - Metadata       │                        │
└──────────────────┴──────────────────┴────────────────────────┘
```

**Profiles** store user-facing information (display name, avatar, locale) that's typically editable by the user themselves.

**Preferences** store settings that can be defined at multiple scope levels (system, tenant, org, user), with higher-specificity levels overriding lower ones.

---

## Profiles

### Profile Fields

The `UserProfile` type contains rich user information:

```go
type UserProfile struct {
    UserID      uuid.UUID          // Links to auth user
    DisplayName string             // User's display name
    AvatarURL   string             // Profile picture URL
    Locale      string             // Language preference (e.g., "en-US")
    Timezone    string             // Timezone (e.g., "America/New_York")
    Bio         string             // Short biography
    Contact     map[string]any     // Contact information (phone, social links)
    Metadata    map[string]any     // Application-specific data
    Scope       ScopeFilter        // Tenant/org scoping
    CreatedAt   time.Time
    UpdatedAt   time.Time
    CreatedBy   uuid.UUID
    UpdatedBy   uuid.UUID
}
```

The `Contact` and `Metadata` fields are flexible JSON maps for application-specific data:

```go
// Contact info example
contact := map[string]any{
    "phone":    "+1-555-0123",
    "twitter":  "@johndoe",
    "linkedin": "linkedin.com/in/johndoe",
}

// Metadata example
metadata := map[string]any{
    "department":    "Engineering",
    "employee_id":   "EMP-001",
    "hire_date":     "2024-01-15",
    "manager_id":    "uuid-of-manager",
}
```

### Profile vs Core User Fields

Understanding when to use profiles vs core user fields:

| Use Case | Where to Store | Why |
|----------|---------------|-----|
| Email address | Core user (go-auth) | Required for authentication |
| Password | Core user (go-auth) | Security-critical |
| Role | Core user (go-auth) | Authorization decisions |
| Account status | Core user (go-auth) | Lifecycle management |
| Display name | Profile | User-editable, non-critical |
| Avatar | Profile | User-editable, display only |
| Locale/timezone | Profile | User preferences |
| Bio/about | Profile | User-editable content |
| Custom metadata | Profile | Application-specific |

### Upserting Profiles

The `ProfileUpsert` command uses PATCH semantics - only specified fields are updated:

```go
package main

import (
    "context"
    "github.com/goliatone/go-users/command"
    "github.com/goliatone/go-users/pkg/types"
    "github.com/google/uuid"
)

func updateUserProfile(ctx context.Context, svc *service.Service) error {
    userID := uuid.MustParse("user-uuid-here")
    actorID := uuid.MustParse("actor-uuid-here")

    // Only update specific fields (PATCH semantics)
    displayName := "John Doe"
    locale := "en-US"
    timezone := "America/New_York"

    var result types.UserProfile
    err := svc.Commands().ProfileUpsert.Execute(ctx, command.ProfileUpsertInput{
        UserID: userID,
        Patch: types.ProfilePatch{
            DisplayName: &displayName,  // Only these fields will be updated
            Locale:      &locale,
            Timezone:    &timezone,
            // AvatarURL, Bio, Contact, Metadata are nil = unchanged
        },
        Scope: types.ScopeFilter{
            TenantID: uuid.MustParse("tenant-uuid"),
        },
        Actor: types.ActorRef{
            ID:   actorID,
            Type: "user",
        },
        Result: &result, // Populated with the updated profile
    })
    if err != nil {
        return err
    }

    fmt.Printf("Updated profile: %s\n", result.DisplayName)
    return nil
}
```

**PATCH Semantics Explained:**

```go
// ProfilePatch uses pointer fields for optional updates
type ProfilePatch struct {
    DisplayName *string        // nil = don't change, non-nil = update
    AvatarURL   *string
    Locale      *string
    Timezone    *string
    Bio         *string
    Contact     map[string]any // nil = don't change, empty map = clear
    Metadata    map[string]any
}
```

Example: Update only the avatar:

```go
avatarURL := "https://example.com/avatars/new-avatar.png"
patch := types.ProfilePatch{
    AvatarURL: &avatarURL,
    // All other fields remain unchanged
}
```

### Querying Profiles

Retrieve a user's profile with the `ProfileDetail` query:

```go
func getProfile(ctx context.Context, svc *service.Service, userID uuid.UUID) (*types.UserProfile, error) {
    return svc.Queries().ProfileDetail.Query(ctx, query.ProfileQueryInput{
        UserID: userID,
        Scope: types.ScopeFilter{
            TenantID: uuid.MustParse("tenant-uuid"),
        },
        Actor: types.ActorRef{
            ID:   uuid.MustParse("actor-uuid"),
            Type: types.ActorRoleSystemAdmin,
        },
    })
}
```

---

## Preferences

Preferences provide hierarchical settings that can be defined at multiple scope levels, with more specific scopes overriding general ones.

### Scope Levels

Preferences support four scope levels, from most general to most specific:

```go
const (
    PreferenceLevelSystem PreferenceLevel = "system"  // Platform-wide defaults
    PreferenceLevelTenant PreferenceLevel = "tenant"  // Tenant-specific
    PreferenceLevelOrg    PreferenceLevel = "org"     // Organization-specific
    PreferenceLevelUser   PreferenceLevel = "user"    // User-specific
)
```

**Resolution Order (lowest to highest priority):**

```
┌─────────────────────────────────────────────────────────────┐
│                    Preference Resolution                    │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│   ┌──────────┐   ┌──────────┐   ┌─────┐   ┌──────┐          │
│   │  System  │ → │  Tenant  │ → │ Org │ → │ User │          │
│   │ (base)   │   │ override │   │     │   │(wins)│          │
│   └──────────┘   └──────────┘   └─────┘   └──────┘          │
│                                                             │
│   Priority:  1         2          3         4               │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### Preference Resolution and Inheritance

When resolving preferences, higher-priority levels override lower ones:

```go
// Example: Theme preference resolution
//
// System level:  { "theme": "light", "font_size": 14 }
// Tenant level:  { "theme": "dark" }  // Overrides system
// Org level:     (not set)
// User level:    { "font_size": 16 }  // Overrides system
//
// Effective:     { "theme": "dark", "font_size": 16 }
```

### Managing Preferences

#### Upserting Preferences

Set a preference at a specific scope level:

```go
func setUserTheme(ctx context.Context, svc *service.Service, userID uuid.UUID, theme string) error {
    var result types.PreferenceRecord
    return svc.Commands().PreferenceUpsert.Execute(ctx, command.PreferenceUpsertInput{
        UserID: userID,
        Level:  types.PreferenceLevelUser,  // User-specific setting
        Key:    "theme",
        Value: map[string]any{
            "mode":        theme,       // "light" or "dark"
            "accent":      "#3b82f6",
            "compact":     false,
        },
        Scope: types.ScopeFilter{
            TenantID: uuid.MustParse("tenant-uuid"),
            OrgID:    uuid.MustParse("org-uuid"),
        },
        Actor: types.ActorRef{
            ID:    userID,  // User setting their own preference
            Type:  "user",
        },
        Result: &result,
    })
}
```

#### Setting Tenant-Wide Defaults

Administrators can set tenant-level defaults:

```go
func setTenantDefaults(ctx context.Context, svc *service.Service, tenantID, adminID uuid.UUID) error {
    return svc.Commands().PreferenceUpsert.Execute(ctx, command.PreferenceUpsertInput{
        // No UserID for tenant-level preferences
        Level: types.PreferenceLevelTenant,
        Key:   "notifications",
        Value: map[string]any{
            "email_enabled":   true,
            "push_enabled":    true,
            "digest_frequency": "daily",
        },
        Scope: types.ScopeFilter{
            TenantID: tenantID,
        },
        Actor: types.ActorRef{
            ID:    adminID,
            Type:  types.ActorRoleTenantAdmin,
        },
    })
}
```

#### Deleting Preferences

Remove a preference to fall back to the next scope level:

```go
func resetUserTheme(ctx context.Context, svc *service.Service, userID uuid.UUID) error {
    return svc.Commands().PreferenceDelete.Execute(ctx, command.PreferenceDeleteInput{
        UserID: userID,
        Level:  types.PreferenceLevelUser,
        Key:    "theme",
        Scope: types.ScopeFilter{
            TenantID: uuid.MustParse("tenant-uuid"),
        },
        Actor: types.ActorRef{
            ID:    userID,
            Type:  "user",
        },
    })
    // After deletion, "theme" falls back to org/tenant/system default
}
```

### Querying Preferences

#### Get Effective Preferences

The `Preferences` query resolves all scope levels and returns the effective values:

```go
func getUserSettings(ctx context.Context, svc *service.Service, userID uuid.UUID) (map[string]any, error) {
    snapshot, err := svc.Queries().Preferences.Query(ctx, query.PreferenceQueryInput{
        UserID: userID,
        Scope: types.ScopeFilter{
            TenantID: uuid.MustParse("tenant-uuid"),
            OrgID:    uuid.MustParse("org-uuid"),
        },
        Actor: types.ActorRef{
            ID:    userID,
            Type:  "user",
        },
    })
    if err != nil {
        return nil, err
    }

    return snapshot.Effective, nil  // Merged preferences from all levels
}
```

#### Filter by Specific Keys

Request only specific preference keys:

```go
snapshot, err := svc.Queries().Preferences.Query(ctx, query.PreferenceQueryInput{
    UserID: userID,
    Keys:   []string{"theme", "notifications", "locale"},  // Only these keys
    Scope: types.ScopeFilter{
        TenantID: tenantID,
    },
    Actor: actor,
})
```

#### Filter by Specific Levels

Query only certain scope levels:

```go
snapshot, err := svc.Queries().Preferences.Query(ctx, query.PreferenceQueryInput{
    UserID: userID,
    Levels: []types.PreferenceLevel{
        types.PreferenceLevelUser,
        types.PreferenceLevelOrg,
    },
    // Excludes system and tenant levels from resolution
    Scope: types.ScopeFilter{...},
    Actor: actor,
})
```

#### Provide Base Defaults

Inject defaults that act as the base layer:

```go
snapshot, err := svc.Queries().Preferences.Query(ctx, query.PreferenceQueryInput{
    UserID: userID,
    Base: map[string]any{
        "theme": map[string]any{
            "mode": "light",
            "accent": "#000000",
        },
        "notifications": map[string]any{
            "email_enabled": true,
        },
    },
    Scope: types.ScopeFilter{...},
    Actor: actor,
})
// Base values are merged into the system layer, then system/tenant/org/user override them
```

### Preference Traces

The `PreferenceSnapshot` includes traces showing where each value originated:

```go
type PreferenceSnapshot struct {
    Effective map[string]any     // The merged result
    Traces    []PreferenceTrace  // Provenance for each key
}

type PreferenceTrace struct {
    Key    string
    Layers []PreferenceTraceLayer
}

type PreferenceTraceLayer struct {
    Level      PreferenceLevel  // system, tenant, org, user
    UserID     uuid.UUID
    Scope      ScopeFilter
    SnapshotID string           // Record ID if found
    Value      any              // Value at this level
    Found      bool             // Whether this level had a value
}
```

Example: Inspecting where a value came from:

```go
snapshot, _ := svc.Queries().Preferences.Query(ctx, input)

for _, trace := range snapshot.Traces {
    fmt.Printf("Key: %s\n", trace.Key)
    for _, layer := range trace.Layers {
        if layer.Found {
            fmt.Printf("  %s: %v (record: %s)\n",
                layer.Level, layer.Value, layer.SnapshotID)
        } else {
            fmt.Printf("  %s: (not set)\n", layer.Level)
        }
    }
}

// Output:
// Key: theme
//   system: map[mode:light accent:#000000] (record: uuid-1)
//   tenant: map[mode:dark] (record: uuid-2)
//   org: (not set)
//   user: map[accent:#3b82f6] (record: uuid-3)
```

---

### Caching (placeholder)

Preferences are read frequently in UI flows. We plan to support opt-in caching
by wrapping the preference repository with `go-repository-cache` (and exposing
constructor options once implemented). Cached repositories invalidate read
caches on writes using method-prefix eviction (repository-wide), which is safe
but coarse.

Example wiring:

```go
repo, err := preferences.NewRepository(preferences.RepositoryConfig{
    DB: bunDB,
}, preferences.WithCache(true))
```

---

## The Preference Resolver

The preference resolver handles the complexity of merging multiple scope layers. It's automatically created when you provide a `PreferenceRepository`:

```go
// Automatic setup in service.New()
svc := service.New(service.Config{
    PreferenceRepository: prefRepo,
    // PreferenceResolver is auto-created from repository
})

// Or provide a custom resolver
resolver, _ := preferences.NewResolver(preferences.ResolverConfig{
    Repository: prefRepo,
    Defaults: map[string]any{
        "theme": map[string]any{"mode": "light"},
        "locale": "en-US",
    },
})

svc := service.New(service.Config{
    PreferenceRepository: prefRepo,
    PreferenceResolver:   resolver,  // Custom resolver with defaults
})
```

The resolver uses `go-options` internally for layer merging with proper priority handling.

---

## Version Tracking

Preference records include version numbers that increment on each upsert (the repository does this automatically). go-users does not enforce optimistic concurrency checks, so applications that need it should compare versions before writing.

```go
type PreferenceRecord struct {
    ID        uuid.UUID
    UserID    uuid.UUID
    Scope     ScopeFilter
    Level     PreferenceLevel
    Key       string
    Value     map[string]any
    Version   int          // Incremented on each update
    CreatedAt time.Time
    UpdatedAt time.Time
    CreatedBy uuid.UUID
    UpdatedBy uuid.UUID
}
```

Use versions to detect concurrent modifications in your UI:

```go
// Fetch current preference (use PreferenceRepository.ListPreferences or your own API)
current, _ := getPreference(ctx, userID, "theme")
displayVersion := current.Version

// ... user makes changes in UI ...

// Before saving, check version hasn't changed
updated, _ := getPreference(ctx, userID, "theme")
if updated.Version != displayVersion {
    return errors.New("preference was modified by another session")
}

// Safe to update
savePreference(ctx, userID, "theme", newValue)
```

---

## Building Settings UIs

### Settings Page Architecture

A typical settings UI pattern:

```go
// SettingsPageData for rendering
type SettingsPageData struct {
    Profile     *types.UserProfile
    Preferences map[string]any
    Traces      map[string]string  // key -> "inherited from: tenant" etc
}

func loadSettingsPage(ctx context.Context, svc *service.Service, userID uuid.UUID) (*SettingsPageData, error) {
    // Load profile
    profile, err := svc.Queries().ProfileDetail.Query(ctx, query.ProfileQueryInput{
        UserID: userID,
        Scope:  scopeFilter,
        Actor:  actor,
    })
    if err != nil {
        return nil, err
    }

    // Load preferences with traces
    snapshot, err := svc.Queries().Preferences.Query(ctx, query.PreferenceQueryInput{
        UserID: userID,
        Keys:   []string{"theme", "notifications", "locale", "timezone"},
        Scope:  scopeFilter,
        Actor:  actor,
    })
    if err != nil {
        return nil, err
    }

    // Build trace info for UI hints
    traces := make(map[string]string)
    for _, trace := range snapshot.Traces {
        for i := len(trace.Layers) - 1; i >= 0; i-- {
            layer := trace.Layers[i]
            if layer.Found {
                if layer.Level != types.PreferenceLevelUser {
                    traces[trace.Key] = fmt.Sprintf("inherited from %s", layer.Level)
                }
                break
            }
        }
    }

    return &SettingsPageData{
        Profile:     profile,
        Preferences: snapshot.Effective,
        Traces:      traces,
    }, nil
}
```

### REST Endpoints Example

```go
// GET /api/users/{id}/profile
func handleGetProfile(w http.ResponseWriter, r *http.Request) {
    profile, err := svc.Queries().ProfileDetail.Query(r.Context(), query.ProfileQueryInput{
        UserID: getUserID(r),
        Scope:  getScopeFromRequest(r),
        Actor:  getActorFromRequest(r),
    })
    // ... return JSON
}

// PATCH /api/users/{id}/profile
func handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
    var patch types.ProfilePatch
    json.NewDecoder(r.Body).Decode(&patch)

    var result types.UserProfile
    err := svc.Commands().ProfileUpsert.Execute(r.Context(), command.ProfileUpsertInput{
        UserID: getUserID(r),
        Patch:  patch,
        Scope:  getScopeFromRequest(r),
        Actor:  getActorFromRequest(r),
        Result: &result,
    })
    // ... return JSON
}

// GET /api/users/{id}/preferences
func handleGetPreferences(w http.ResponseWriter, r *http.Request) {
    keys := r.URL.Query()["key"]  // ?key=theme&key=notifications

    snapshot, err := svc.Queries().Preferences.Query(r.Context(), query.PreferenceQueryInput{
        UserID: getUserID(r),
        Keys:   keys,
        Scope:  getScopeFromRequest(r),
        Actor:  getActorFromRequest(r),
    })
    // ... return JSON with effective and traces
}

// PUT /api/users/{id}/preferences/{key}
func handleSetPreference(w http.ResponseWriter, r *http.Request) {
    key := chi.URLParam(r, "key")
    var value map[string]any
    json.NewDecoder(r.Body).Decode(&value)

    err := svc.Commands().PreferenceUpsert.Execute(r.Context(), command.PreferenceUpsertInput{
        UserID: getUserID(r),
        Level:  types.PreferenceLevelUser,
        Key:    key,
        Value:  value,
        Scope:  getScopeFromRequest(r),
        Actor:  getActorFromRequest(r),
    })
    // ... return success
}

// DELETE /api/users/{id}/preferences/{key}
func handleResetPreference(w http.ResponseWriter, r *http.Request) {
    key := chi.URLParam(r, "key")

    err := svc.Commands().PreferenceDelete.Execute(r.Context(), command.PreferenceDeleteInput{
        UserID: getUserID(r),
        Level:  types.PreferenceLevelUser,
        Key:    key,
        Scope:  getScopeFromRequest(r),
        Actor:  getActorFromRequest(r),
    })
    // ... return success (will fall back to tenant/system default)
}
```

---

## Common Patterns

### Theme Settings

```go
// Define theme preference structure
type ThemePreference struct {
    Mode     string `json:"mode"`      // "light", "dark", "system"
    Accent   string `json:"accent"`    // Hex color
    Compact  bool   `json:"compact"`
    FontSize int    `json:"font_size"`
}

func setTheme(ctx context.Context, svc *service.Service, userID uuid.UUID, theme ThemePreference) error {
    return svc.Commands().PreferenceUpsert.Execute(ctx, command.PreferenceUpsertInput{
        UserID: userID,
        Level:  types.PreferenceLevelUser,
        Key:    "theme",
        Value: map[string]any{
            "mode":      theme.Mode,
            "accent":    theme.Accent,
            "compact":   theme.Compact,
            "font_size": theme.FontSize,
        },
        Scope: scopeFilter,
        Actor: actor,
    })
}

func getTheme(ctx context.Context, svc *service.Service, userID uuid.UUID) (*ThemePreference, error) {
    snapshot, err := svc.Queries().Preferences.Query(ctx, query.PreferenceQueryInput{
        UserID: userID,
        Keys:   []string{"theme"},
        Scope:  scopeFilter,
        Actor:  actor,
    })
    if err != nil {
        return nil, err
    }

    themeMap, ok := snapshot.Effective["theme"].(map[string]any)
    if !ok {
        // Return defaults
        return &ThemePreference{Mode: "light", FontSize: 14}, nil
    }

    return &ThemePreference{
        Mode:     getString(themeMap, "mode", "light"),
        Accent:   getString(themeMap, "accent", "#3b82f6"),
        Compact:  getBool(themeMap, "compact", false),
        FontSize: getInt(themeMap, "font_size", 14),
    }, nil
}
```

### Locale and Timezone

```go
func setLocalePreferences(ctx context.Context, svc *service.Service, userID uuid.UUID, locale, timezone string) error {
    // Update profile for persistent locale/timezone
    err := svc.Commands().ProfileUpsert.Execute(ctx, command.ProfileUpsertInput{
        UserID: userID,
        Patch: types.ProfilePatch{
            Locale:   &locale,
            Timezone: &timezone,
        },
        Scope: scopeFilter,
        Actor: actor,
    })
    if err != nil {
        return err
    }

    // Also store in preferences for consistency
    return svc.Commands().PreferenceUpsert.Execute(ctx, command.PreferenceUpsertInput{
        UserID: userID,
        Level:  types.PreferenceLevelUser,
        Key:    "locale",
        Value: map[string]any{
            "language":    locale,
            "timezone":    timezone,
            "date_format": "YYYY-MM-DD",
            "time_format": "24h",
        },
        Scope: scopeFilter,
        Actor: actor,
    })
}
```

### Notification Preferences

```go
type NotificationPrefs struct {
    EmailEnabled     bool     `json:"email_enabled"`
    PushEnabled      bool     `json:"push_enabled"`
    SMSEnabled       bool     `json:"sms_enabled"`
    DigestFrequency  string   `json:"digest_frequency"`  // "realtime", "daily", "weekly"
    QuietHoursStart  string   `json:"quiet_hours_start"` // "22:00"
    QuietHoursEnd    string   `json:"quiet_hours_end"`   // "08:00"
    Categories       []string `json:"categories"`        // Subscribed categories
}

func setNotificationPrefs(ctx context.Context, svc *service.Service, userID uuid.UUID, prefs NotificationPrefs) error {
    return svc.Commands().PreferenceUpsert.Execute(ctx, command.PreferenceUpsertInput{
        UserID: userID,
        Level:  types.PreferenceLevelUser,
        Key:    "notifications",
        Value: map[string]any{
            "email_enabled":     prefs.EmailEnabled,
            "push_enabled":      prefs.PushEnabled,
            "sms_enabled":       prefs.SMSEnabled,
            "digest_frequency":  prefs.DigestFrequency,
            "quiet_hours_start": prefs.QuietHoursStart,
            "quiet_hours_end":   prefs.QuietHoursEnd,
            "categories":        prefs.Categories,
        },
        Scope: scopeFilter,
        Actor: actor,
    })
}
```

### Feature Flags per User

```go
// Set org-level feature flags
func setOrgFeatureFlags(ctx context.Context, svc *service.Service, tenantID, orgID, adminID uuid.UUID) error {
    return svc.Commands().PreferenceUpsert.Execute(ctx, command.PreferenceUpsertInput{
        Level: types.PreferenceLevelOrg,
        Key:   "features",
        Value: map[string]any{
            "new_dashboard":     true,
            "beta_exports":      true,
            "advanced_search":   false,
        },
        Scope: types.ScopeFilter{
            TenantID: tenantID,
            OrgID:    orgID,
        },
        Actor: types.ActorRef{
            ID:    adminID,
            Type:  types.ActorRoleOrgAdmin,
        },
    })
}

// Check if feature is enabled for user
func isFeatureEnabled(ctx context.Context, svc *service.Service, userID uuid.UUID, feature string) (bool, error) {
    snapshot, err := svc.Queries().Preferences.Query(ctx, query.PreferenceQueryInput{
        UserID: userID,
        Keys:   []string{"features"},
        Scope:  scopeFilter,
        Actor:  actor,
    })
    if err != nil {
        return false, err
    }

    features, ok := snapshot.Effective["features"].(map[string]any)
    if !ok {
        return false, nil
    }

    enabled, _ := features[feature].(bool)
    return enabled, nil
}
```

---

## Error Handling

Common errors when working with profiles and preferences:

```go
import "github.com/goliatone/go-users/pkg/types"

// Profile errors
types.ErrUserIDRequired            // UserID is required for profile operations
types.ErrMissingProfileRepository  // ProfileRepository not configured

// Preference errors
types.ErrMissingPreferenceRepository  // PreferenceRepository not configured
types.ErrMissingPreferenceResolver    // PreferenceResolver not configured

// Command-specific errors
command.ErrPreferenceKeyRequired   // Key is required for preference operations
command.ErrPreferenceValueRequired // Value is required for upsert
command.ErrActorRequired           // Actor is required for all operations
```

Error handling example:

```go
func handleProfileUpdate(ctx context.Context, svc *service.Service, input command.ProfileUpsertInput) error {
    err := svc.Commands().ProfileUpsert.Execute(ctx, input)
    if err != nil {
        switch {
        case errors.Is(err, types.ErrUserIDRequired):
            return fmt.Errorf("user ID is required")
        case errors.Is(err, types.ErrMissingProfileRepository):
            return fmt.Errorf("profile service not available")
        case errors.Is(err, command.ErrActorRequired):
            return fmt.Errorf("authentication required")
        default:
            return fmt.Errorf("failed to update profile: %w", err)
        }
    }
    return nil
}
```

---

## Next Steps

- [GUIDE_HOOKS.md](GUIDE_HOOKS.md) - React to profile and preference changes
- [GUIDE_MULTITENANCY.md](GUIDE_MULTITENANCY.md) - Scope isolation for multi-tenant apps
- [GUIDE_QUERIES.md](GUIDE_QUERIES.md) - Advanced querying patterns
- [GUIDE_CRUD_INTEGRATION.md](GUIDE_CRUD_INTEGRATION.md) - REST API integration with go-crud
