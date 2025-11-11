
### 3. Missing Core Admin Features

#### 3.1 User/Role Management - **75% Ready** ✅

**Current State via go-auth**:

The **[go-auth](https://github.com/goliatone/go-auth)** package provides comprehensive authentication and user management:

**Already Implemented** ✅:
- JWT token generation and validation with structured claims
- User model with CRUD operations via Bun ORM
- Password hashing (bcrypt) and verification
- User registration and login flows
- Password reset with email verification
- Hierarchical role system: `guest`, `member`, `admin`, `owner`
- Resource-level permissions for fine-grained access control
- HTTP middleware for route protection
- WebSocket authentication support
- CSRF protection middleware
- Session management with role capabilities
- Built-in authentication controllers

**User Model** (from go-auth):
```go
type User struct {
    ID             uuid.UUID
    Role           UserRole // guest, member, admin, owner
    FirstName      string
    LastName       string
    Username       string
    Email          string
    Phone          string
    PasswordHash   string
    ProfilePicture string
    EmailValidated bool
    LoginAttempts  int
    LoginAttemptAt *time.Time
    LoggedInAt     *time.Time
    Metadata       map[string]any // Extensible metadata
    CreatedAt      *time.Time
    UpdatedAt      *time.Time
    DeletedAt      *time.Time // Soft delete support
}
```

**Repository Interface** (from go-auth):
```go
type Users interface {
    GetByID(ctx context.Context, id string) (*User, error)
    GetByIdentifier(ctx context.Context, identifier string) (*User, error) // email, username, or UUID
    Register(ctx context.Context, user *User) (*User, error)
    Update(ctx context.Context, record *User) (*User, error)
    Upsert(ctx context.Context, record *User) (*User, error)
    ResetPassword(ctx context.Context, id uuid.UUID, passwordHash string) error
    TrackAttemptedLogin(ctx context.Context, user *User) error
    TrackSucccessfulLogin(ctx context.Context, user *User) error
}
```

**Role-Based Access Control** (from go-auth):
```go
// Hierarchical role system with permission methods
type UserRole string

const (
    RoleGuest  UserRole = "guest"  // Read-only
    RoleMember UserRole = "member" // Read + Edit
    RoleAdmin  UserRole = "admin"  // Read + Edit + Create
    RoleOwner  UserRole = "owner"  // Full access including delete
)

// Resource-level permissions in JWT claims
type AuthClaims struct {
    UserID        string
    Role          string
    ResourceRoles map[string]string // e.g., "project:123": "admin"
    // ...standard JWT fields
}

// Permission checking
session.CanRead("admin:dashboard")
session.CanEdit("content:articles")
session.CanCreate("pages")
session.CanDelete("users")
session.IsAtLeast("admin")
```

**What's Missing from go-auth** 🟡:

While go-auth provides solid authentication and basic RBAC, it lacks admin-specific features:

1. **User Management UI Operations**:
```go
// Need to add to go-auth or go-admin
ListUsers(ctx, filters UserFilters) (*PaginatedUsers, error)
BulkUpdateUsers(ctx, ids []uuid.UUID, updates UserUpdates) error
SuspendUser(ctx, id uuid.UUID) error
ActivateUser(ctx, id uuid.UUID) error
DeleteUser(ctx, id uuid.UUID) error // Currently uses soft delete, need explicit method
SearchUsers(ctx, query string) ([]*User, error)
```

2. **Custom Role Management**:
```go
// go-auth has fixed roles (guest, member, admin, owner)
// Need dynamic role management for admin UI
type CustomRoleService interface {
    CreateRole(ctx context.Context, input CreateRoleInput) (*CustomRole, error)
    UpdateRole(ctx context.Context, id uuid.UUID, input UpdateRoleInput) (*CustomRole, error)
    DeleteRole(ctx context.Context, id uuid.UUID) error
    ListRoles(ctx context.Context) ([]*CustomRole, error)
    AssignRoleToUser(ctx context.Context, userID, roleID uuid.UUID) error
}

type CustomRole struct {
    ID          uuid.UUID
    Name        string
    Description *string
    Permissions []string // "content.create", "pages.delete", etc.
    IsSystem    bool     // true for built-in roles
    CreatedAt   time.Time
}
```

3. **Permission Registry**:
```go
// Need centralized permission registry for admin UI
type PermissionRegistry interface {
    RegisterPermission(resource, action, description string)
    ListPermissions() []Permission
    CheckPermission(ctx context.Context, userID uuid.UUID, permission string) bool
}

// Example permissions
content.create, content.read, content.update, content.delete
pages.create, pages.read, pages.update, pages.delete
blocks.manage, widgets.manage, menus.manage, themes.manage
users.manage, settings.manage
```

4. **User Activity Tracking**:
```go
// Extend go-auth's login tracking
type ActivityService interface {
    LogActivity(ctx context.Context, input LogActivityInput) error
    GetUserActivity(ctx context.Context, userID uuid.UUID) ([]*Activity, error)
    GetRecentActivity(ctx context.Context, limit int) ([]*Activity, error)
}
```

**Integration Strategy**:

1. **Use go-auth as-is** for core authentication/authorization
2. **Extend in go-cms** for CMS-specific user management:
   - Create `internal/users/service.go` as wrapper around go-auth
   - Add admin-specific operations (list, search, bulk operations)
   - Add custom role management if needed
3. **Integrate into go-admin** for UI layer:
   - User management dashboard
   - Role/permission editor
   - Activity log viewer

**Updated Database Schema**:

The go-auth schema is already solid. Only need to add optional custom roles table:

```sql
-- go-auth provides users table (already exists)

-- Add custom roles table for admin UI (optional)
CREATE TABLE custom_roles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) UNIQUE NOT NULL,
    description TEXT,
    permissions JSONB NOT NULL DEFAULT '[]',
    is_system BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE user_custom_roles (
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    role_id UUID REFERENCES custom_roles(id) ON DELETE CASCADE,
    assigned_at TIMESTAMP NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, role_id)
);
```

**Future go-users Module Responsibilities**:

Once the go-auth lifecycle extensions from `../go-auth/AUTH_TDD.md`/`../go-auth/AUTH_TSK.md` land, the complementary `go-users` module should:
- Provide lifecycle services (suspend/unsuspend/disable/archive) using the shared state machine, surfacing reason codes and actor metadata.
- Implement user inventory APIs (list/filter/search/paginate/bulk ops) with status-aware filters and visibility controls atop the shared repository.
- Own an ActivitySink implementation plus REST/gRPC endpoints so go-admin can render audit trails.
- Manage custom roles/permissions (optional) and feed them into JWTs through the ClaimsDecorator.
- Orchestrate user profile management (contact info, avatars, metadata) plus preference storage so surfaces can read/write consistent details without coupling to go-auth’s core user struct.
- Expose admin workflows (invites, password resets, impersonation approvals) that wrap go-auth authenticator flows.
- Enforce policies by ensuring only authorized actors trigger lifecycle transitions and by blocking auth for non-active statuses.
- Supply integration glue (DTO mapping, caching, notification hooks) for go-admin/go-cms, along with end-to-end tests covering lifecycle → audit → claims behavior.

### Design Decisions (Latest)

- **Repository compatibility**: go-auth will expose a repository matching `go-repository-bun/Repository`, and go-users commands/queries/services will rely on that interface so consumers can wire any Bun-backed store without a hard dependency on `go-persistence-bun`.
- **Service exposure**: go-users exports a `Service` entry point (not an `app`) that bundles migrations, commands, queries, registries, and hook wiring so host applications can plumb dependencies once and share the same command surface.
- **Activity schema**: adopt the richer typed envelope (Option B). Activity tables include typed columns (`verb`, `object_type`, `object_id`, `channel`, `actor_id`, `user_id`, `tenant_id`, `org_id`, timestamps) plus a `data JSONB` column for extensibility, giving fast filtering on common fields while still supporting arbitrary metadata.
- **Tenant/org support**: start with nullable `tenant_id` and `org_id` columns on user, role, and activity tables. Commands and queries accept optional scope filters now so multi-tenant rollout later is additive (indexes and APIs already in place).
- **Scope guard & policies**: host apps inject a `ScopeResolver` + `AuthorizationPolicy` pair so every command/query resolves tenant/org defaults and enforces visibility before hitting repositories, keeping multi-tenant restrictions centralized and transport-agnostic.
- **Hook interfaces**: begin with simple function hooks (Option A). Each command accepts optional callbacks (e.g., `LifecycleHook func(ctx context.Context, event LifecycleEvent)`) so host apps can inject their own notification/audit behaviors without go-users knowing transport details.
- **Role registry caching**: surface callbacks (Option B). The default registry emits `RoleEvent` callbacks (`OnRoleChanged`) so applications can invalidate caches or propagate changes immediately, while still allowing custom registry implementations to plug in.

### Additional Concerns

- **User preferences**: go-users should define a preference store (keyed by user, scope, and optionally tenant/org) with commands for upsert/list/delete plus queries for resolving effective preferences. This enables go-admin/go-cms to persist UI, notification, or feature toggles without inventing parallel schemas.
- **User profiles**: extend beyond core go-auth fields by offering profile commands that manage structured profile sections (bio, social links, localization settings). Provide DTOs so consuming apps can decide which sections to expose, while migrations add optional tables/JSONB columns to hold profile metadata.

### Alignment with go-admin Architecture

- **Dashboard widgets**: go-admin’s `go-dashboard` package expects widgets such as `admin.widget.user_stats` and `admin.widget.recent_activity` (`/Users/goliatone/Downloads/GO-ADMIN/go-admin-architecture-updated.md:110-138`). go-users should expose query providers that supply aggregate user metrics, lifecycle breakdowns, and recent activity feeds so those widgets can bind without bespoke logic.
- **Activity + audit feeds**: the planned `go-activity` module relies on user-attributed, searchable activity streams (`.../go-admin-architecture-updated.md:134-150`). go-users’ ActivitySink must emit the same typed verbs/object identifiers so go-admin can reuse the data for dashboard widgets and audit panels.
- **Admin panels**: go-admin defines a dedicated `users` panel that consumes a repository + model (`.../go-admin-architecture-updated.md:797-804`). Our service/command layer should provide DTOs, list/search queries, and lifecycle commands that this panel can call directly, minimizing duplication inside go-admin.
- **Preferences + settings**: `go-settings` and notification packages call out user-level preferences (`.../go-admin-architecture-updated.md:178-183`). The preference store inside go-users needs to support per-user + per-tenant scopes and surface APIs/hooks so go-admin can persist dashboard layouts, notification choices, and other admin UI state (e.g., widget layout lookup at `.../go-admin-architecture-updated.md:636-647`).
- **Notifications**: go-admin’s `go-notifications` plans include notification preferences and WebSocket pushes (`.../go-admin-architecture-updated.md:122-130`). go-users should expose hooks or callback points that allow host apps to trigger those notifications after lifecycle events, role changes, or activity logs without direct dependencies.
