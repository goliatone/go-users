package types

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// LifecycleState represents the allowed user states for lifecycle commands.
type LifecycleState string

const (
	LifecycleStatePending   LifecycleState = "pending"
	LifecycleStateActive    LifecycleState = "active"
	LifecycleStateSuspended LifecycleState = "suspended"
	LifecycleStateDisabled  LifecycleState = "disabled"
	LifecycleStateArchived  LifecycleState = "archived"
)

// ScopeFilter carries tenant/org scoping fields used by commands/queries.
type ScopeFilter struct {
	TenantID uuid.UUID
	OrgID    uuid.UUID
	Labels   map[string]uuid.UUID
}

// Clone returns a copy of the scope filter with labels detached from the
// original map reference so callers can mutate safely.
func (s ScopeFilter) Clone() ScopeFilter {
	clone := ScopeFilter{
		TenantID: s.TenantID,
		OrgID:    s.OrgID,
	}
	if len(s.Labels) > 0 {
		clone.Labels = make(map[string]uuid.UUID, len(s.Labels))
		for k, v := range s.Labels {
			clone.Labels[k] = v
		}
	}
	return clone
}

// WithLabel returns a cloned scope filter with the provided label set. Keys are
// normalized to lower-case so lookups stay consistent across transports.
func (s ScopeFilter) WithLabel(key string, id uuid.UUID) ScopeFilter {
	if strings.TrimSpace(key) == "" || id == uuid.Nil {
		return s
	}
	clone := s.Clone()
	if clone.Labels == nil {
		clone.Labels = make(map[string]uuid.UUID)
	}
	clone.Labels[strings.ToLower(key)] = id
	return clone
}

// Label returns the identifier previously stored under the key (case
// insensitive). When the label has not been set, uuid.Nil is returned.
func (s ScopeFilter) Label(key string) uuid.UUID {
	if len(s.Labels) == 0 {
		return uuid.Nil
	}
	return s.Labels[strings.ToLower(strings.TrimSpace(key))]
}

// Pagination supports query pagination across admin panels.
type Pagination struct {
	Limit  int
	Offset int
}

// LifecycleEvent is emitted after lifecycle transitions.
type LifecycleEvent struct {
	UserID     uuid.UUID
	ActorID    uuid.UUID
	FromState  LifecycleState
	ToState    LifecycleState
	Reason     string
	OccurredAt time.Time
	Scope      ScopeFilter
	Metadata   map[string]any
}

// RoleEvent is emitted when a custom role or assignment changes.
type RoleEvent struct {
	RoleID     uuid.UUID
	UserID     uuid.UUID
	Action     string
	ActorID    uuid.UUID
	Scope      ScopeFilter
	OccurredAt time.Time
	Role       RoleDefinition
}

// PreferenceEvent signals preference mutations so downstream systems can
// invalidate caches or push notifications.
type PreferenceEvent struct {
	UserID     uuid.UUID
	Scope      ScopeFilter
	Key        string
	Action     string
	ActorID    uuid.UUID
	OccurredAt time.Time
}

// ProfileEvent signals that a profile mutation occurred.
type ProfileEvent struct {
	UserID     uuid.UUID
	Scope      ScopeFilter
	ActorID    uuid.UUID
	OccurredAt time.Time
	Profile    UserProfile
}

// Hooks groups optional callbacks invoked after key workflows complete.
type Hooks struct {
	AfterLifecycle        func(context.Context, LifecycleEvent)
	AfterRoleChange       func(context.Context, RoleEvent)
	AfterPreferenceChange func(context.Context, PreferenceEvent)
	AfterProfileChange    func(context.Context, ProfileEvent)
	AfterActivity         func(context.Context, ActivityRecord)
}

// ActivityRecord describes sink inputs and is shared across sink and query layers.
type ActivityRecord struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	ActorID    uuid.UUID
	Verb       string
	ObjectType string
	ObjectID   string
	Channel    string
	IP         string
	TenantID   uuid.UUID
	OrgID      uuid.UUID
	Data       map[string]any
	OccurredAt time.Time
}

// ActivitySink is the minimal DI contract for emitting activity. Keep it stable
// and limited to Log so downstream modules can swap sinks without breaking
// changes.
type ActivitySink interface {
	Log(context.Context, ActivityRecord) error
}

// ActivityRepository exposes read-side access to activity logs.
type ActivityRepository interface {
	ListActivity(ctx context.Context, filter ActivityFilter) (ActivityPage, error)
	ActivityStats(ctx context.Context, filter ActivityStatsFilter) (ActivityStats, error)
}

// UserInventoryRepository exposes list/search helpers powering admin panels.
type UserInventoryRepository interface {
	ListUsers(ctx context.Context, filter UserInventoryFilter) (UserInventoryPage, error)
}

// RoleRegistry describes custom role CRUD operations.
type RoleRegistry interface {
	CreateRole(ctx context.Context, input RoleMutation) (*RoleDefinition, error)
	UpdateRole(ctx context.Context, id uuid.UUID, input RoleMutation) (*RoleDefinition, error)
	DeleteRole(ctx context.Context, id uuid.UUID, scope ScopeFilter, actor uuid.UUID) error
	AssignRole(ctx context.Context, userID, roleID uuid.UUID, scope ScopeFilter, actor uuid.UUID) error
	UnassignRole(ctx context.Context, userID, roleID uuid.UUID, scope ScopeFilter, actor uuid.UUID) error
	ListRoles(ctx context.Context, filter RoleFilter) (RolePage, error)
	GetRole(ctx context.Context, id uuid.UUID, scope ScopeFilter) (*RoleDefinition, error)
	ListAssignments(ctx context.Context, filter RoleAssignmentFilter) ([]RoleAssignment, error)
}

// Clock abstracts time retrieval for deterministic testing.
type Clock interface {
	Now() time.Time
}

// IDGenerator abstracts UUID creation.
type IDGenerator interface {
	UUID() uuid.UUID
}

// Logger captures basic logging hooks used by the service.
type Logger interface {
	Debug(msg string, fields ...any)
	Info(msg string, fields ...any)
	Error(msg string, err error, fields ...any)
}

// UserInventoryFilter collects filters accepted by admin search panels.
type UserInventoryFilter struct {
	Actor      ActorRef
	Scope      ScopeFilter
	Statuses   []LifecycleState
	Role       string
	Keyword    string
	Pagination Pagination
	UserIDs    []uuid.UUID
}

// Type implements gocommand.Message for query inputs.
func (UserInventoryFilter) Type() string {
	return "query.user.inventory"
}

// Validate implements gocommand.Message.
func (filter UserInventoryFilter) Validate() error {
	if filter.Actor.ID == uuid.Nil {
		return ErrActorRequired
	}
	return nil
}

// UserInventoryPage represents a paginated list of auth users.
type UserInventoryPage struct {
	Users      []AuthUser
	Total      int
	NextOffset int
	HasMore    bool
}

// RoleMutation describes create/update payloads for roles.
type RoleMutation struct {
	Name        string
	Description string
	Permissions []string
	IsSystem    bool
	Scope       ScopeFilter
	ActorID     uuid.UUID
}

// RoleDefinition mirrors the persisted role data returned by the registry.
type RoleDefinition struct {
	ID          uuid.UUID
	Name        string
	Description string
	Permissions []string
	IsSystem    bool
	Scope       ScopeFilter
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CreatedBy   uuid.UUID
	UpdatedBy   uuid.UUID
}

// RoleFilter narrows role listings.
type RoleFilter struct {
	Actor         ActorRef
	Scope         ScopeFilter
	Keyword       string
	IncludeSystem bool
	RoleIDs       []uuid.UUID
	Pagination    Pagination
}

// Type implements gocommand.Message for query inputs.
func (RoleFilter) Type() string {
	return "query.role.list"
}

// Validate implements gocommand.Message.
func (filter RoleFilter) Validate() error {
	if filter.Actor.ID == uuid.Nil {
		return ErrActorRequired
	}
	return nil
}

// RolePage represents a paginated set of roles.
type RolePage struct {
	Roles      []RoleDefinition
	Total      int
	NextOffset int
	HasMore    bool
}

// RoleAssignment describes a user->role mapping.
type RoleAssignment struct {
	UserID     uuid.UUID
	RoleID     uuid.UUID
	RoleName   string
	Scope      ScopeFilter
	AssignedAt time.Time
	AssignedBy uuid.UUID
}

// RoleAssignmentFilter filters assignment queries.
type RoleAssignmentFilter struct {
	Actor   ActorRef
	Scope   ScopeFilter
	UserID  uuid.UUID
	RoleID  uuid.UUID
	UserIDs []uuid.UUID
	RoleIDs []uuid.UUID
}

// Type implements gocommand.Message for query inputs.
func (RoleAssignmentFilter) Type() string {
	return "query.role.assignments"
}

// Validate implements gocommand.Message.
func (filter RoleAssignmentFilter) Validate() error {
	if filter.Actor.ID == uuid.Nil {
		return ErrActorRequired
	}
	return nil
}

// UserProfile captures the structured profile data stored alongside auth users.
type UserProfile struct {
	UserID      uuid.UUID
	DisplayName string
	AvatarURL   string
	Locale      string
	Timezone    string
	Bio         string
	Contact     map[string]any
	Metadata    map[string]any
	Scope       ScopeFilter
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CreatedBy   uuid.UUID
	UpdatedBy   uuid.UUID
}

// ProfilePatch represents partial updates applied to a user profile.
type ProfilePatch struct {
	DisplayName *string
	AvatarURL   *string
	Locale      *string
	Timezone    *string
	Bio         *string
	Contact     map[string]any
	Metadata    map[string]any
}

// ProfileRepository persists and retrieves profile records.
type ProfileRepository interface {
	GetProfile(ctx context.Context, userID uuid.UUID, scope ScopeFilter) (*UserProfile, error)
	UpsertProfile(ctx context.Context, profile UserProfile) (*UserProfile, error)
}

// PreferenceLevel identifies the precedence layer for a stored preference.
type PreferenceLevel string

const (
	PreferenceLevelSystem PreferenceLevel = "system"
	PreferenceLevelTenant PreferenceLevel = "tenant"
	PreferenceLevelOrg    PreferenceLevel = "org"
	PreferenceLevelUser   PreferenceLevel = "user"
)

// PreferenceRecord represents a stored scoped preference entry.
type PreferenceRecord struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Scope     ScopeFilter
	Level     PreferenceLevel
	Key       string
	Value     map[string]any
	Version   int
	CreatedAt time.Time
	UpdatedAt time.Time
	CreatedBy uuid.UUID
	UpdatedBy uuid.UUID
}

// PreferenceFilter narrows preference listing queries.
type PreferenceFilter struct {
	UserID uuid.UUID
	Scope  ScopeFilter
	Level  PreferenceLevel
	Keys   []string
}

// PreferenceRepository exposes CRUD helpers for scoped preferences.
type PreferenceRepository interface {
	ListPreferences(ctx context.Context, filter PreferenceFilter) ([]PreferenceRecord, error)
	UpsertPreference(ctx context.Context, record PreferenceRecord) (*PreferenceRecord, error)
	DeletePreference(ctx context.Context, userID uuid.UUID, scope ScopeFilter, level PreferenceLevel, key string) error
}

// PreferenceSnapshot depicts the effective settings plus provenance per key.
type PreferenceSnapshot struct {
	Effective map[string]any
	Traces    []PreferenceTrace
}

// PreferenceTrace captures how each scope contributed to a key.
type PreferenceTrace struct {
	Key    string
	Layers []PreferenceTraceLayer
}

// PreferenceTraceLayer captures a single scope contribution.
type PreferenceTraceLayer struct {
	Level      PreferenceLevel
	UserID     uuid.UUID
	Scope      ScopeFilter
	SnapshotID string
	Value      any
	Found      bool
}

// ActivityFilter narrows activity feed queries.
type ActivityFilter struct {
	Actor      ActorRef
	Scope      ScopeFilter
	UserID     uuid.UUID
	ActorID    uuid.UUID
	Verbs      []string
	ObjectType string
	ObjectID   string
	Channel    string
	Since      *time.Time
	Until      *time.Time
	Pagination Pagination
	Keyword    string
}

// Type implements gocommand.Message for query inputs.
func (ActivityFilter) Type() string {
	return "query.activity.feed"
}

// Validate implements gocommand.Message.
func (filter ActivityFilter) Validate() error {
	if filter.Actor.ID == uuid.Nil {
		return ErrActorRequired
	}
	return nil
}

// ActivityPage represents a paginated feed response.
type ActivityPage struct {
	Records    []ActivityRecord
	Total      int
	NextOffset int
	HasMore    bool
}

// ActivityStatsFilter scopes aggregate activity queries.
type ActivityStatsFilter struct {
	Actor ActorRef
	Scope ScopeFilter
	Since *time.Time
	Until *time.Time
	Verbs []string
}

// Type implements gocommand.Message for query inputs.
func (ActivityStatsFilter) Type() string {
	return "query.activity.stats"
}

// Validate implements gocommand.Message.
func (filter ActivityStatsFilter) Validate() error {
	if filter.Actor.ID == uuid.Nil {
		return ErrActorRequired
	}
	return nil
}

// ActivityStats powers dashboard widgets summarizing verbs/channels.
type ActivityStats struct {
	Total  int
	ByVerb map[string]int
}

// SystemClock defers to time.Now for production usage.
type SystemClock struct{}

// Now returns the current UTC time.
func (SystemClock) Now() time.Time { return time.Now().UTC() }

// UUIDGenerator produces UUIDv4 identifiers.
type UUIDGenerator struct{}

// UUID returns a randomly generated UUID.
func (UUIDGenerator) UUID() uuid.UUID { return uuid.New() }

// NopLogger discards all log lines.
type NopLogger struct{}

// Debug implements Logger.
func (NopLogger) Debug(string, ...any) {}

// Info implements Logger.
func (NopLogger) Info(string, ...any) {}

// Error implements Logger.
func (NopLogger) Error(string, error, ...any) {}

var (
	// ErrActorRequired indicates an actor reference was not supplied.
	ErrActorRequired = errors.New("go-users: actor reference required")
	// ErrUserIDRequired indicates a user identifier was omitted.
	ErrUserIDRequired = errors.New("go-users: user id required")
	// ErrServiceNotReady indicates the service has not been properly configured.
	ErrServiceNotReady = errors.New("go-users: service not ready")
	// ErrMissingAuthRepository occurs when no auth repository was supplied.
	ErrMissingAuthRepository = errors.New("go-users: missing auth repository")
	// ErrMissingRoleRegistry occurs when no role registry was supplied.
	ErrMissingRoleRegistry = errors.New("go-users: missing role registry")
	// ErrMissingActivitySink occurs when no activity sink was supplied.
	ErrMissingActivitySink = errors.New("go-users: missing activity sink")
	// ErrMissingInventoryRepository occurs when the service lacks a user inventory data source.
	ErrMissingInventoryRepository = errors.New("go-users: missing inventory repository")
	// ErrMissingActivityRepository occurs when no activity repository was supplied.
	ErrMissingActivityRepository = errors.New("go-users: missing activity repository")
	// ErrMissingProfileRepository occurs when profile commands lack a storage backend.
	ErrMissingProfileRepository = errors.New("go-users: missing profile repository")
	// ErrMissingPreferenceRepository occurs when preference commands or queries lack storage.
	ErrMissingPreferenceRepository = errors.New("go-users: missing preference repository")
	// ErrMissingPreferenceResolver occurs when preference queries lack a resolver.
	ErrMissingPreferenceResolver = errors.New("go-users: missing preference resolver")
)
