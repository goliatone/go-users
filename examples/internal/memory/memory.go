package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

// AuthRepository is an in-memory implementation of both the auth repository and
// the user inventory repository contracts. It is only intended for examples.
type AuthRepository struct {
	mu    sync.RWMutex
	users map[uuid.UUID]*types.AuthUser
}

// NewAuthRepository provisions an in-memory auth repository.
func NewAuthRepository() *AuthRepository {
	return &AuthRepository{
		users: make(map[uuid.UUID]*types.AuthUser),
	}
}

var (
	_                                types.AuthRepository          = (*AuthRepository)(nil)
	_                                types.UserInventoryRepository = (*AuthRepository)(nil)
	ErrUserNotFound                                                = fmt.Errorf("memory auth: user not found")
	ErrRoleNotFound                                                = fmt.Errorf("memory roles: role not found")
	ErrAssignmentNotFound                                          = fmt.Errorf("memory roles: assignment not found")
	zeroUUID                                                       = uuid.Nil
	defaultLifecycleState                                          = types.LifecycleStatePending
	defaultLifecycleAllowedToActive                                = []types.LifecycleTransition{{From: types.LifecycleStatePending, To: types.LifecycleStateActive}}
	defaultLifecycleAllowedToSuspend                               = []types.LifecycleTransition{{From: types.LifecycleStateActive, To: types.LifecycleStateSuspended}}
)

func (r *AuthRepository) GetByID(_ context.Context, id uuid.UUID) (*types.AuthUser, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	user, ok := r.users[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	copy := *user
	return &copy, nil
}

func (r *AuthRepository) GetByIdentifier(_ context.Context, identifier string) (*types.AuthUser, error) {
	identifier = strings.TrimSpace(strings.ToLower(identifier))
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, user := range r.users {
		if strings.EqualFold(user.Email, identifier) || strings.EqualFold(user.Username, identifier) {
			copy := *user
			return &copy, nil
		}
	}
	return nil, ErrUserNotFound
}

func (r *AuthRepository) Create(_ context.Context, input *types.AuthUser) (*types.AuthUser, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if input.ID == uuid.Nil {
		input.ID = uuid.New()
	}
	now := time.Now().UTC()
	input.CreatedAt = &now
	input.Status = defaultLifecycleState
	r.users[input.ID] = cloneAuthUser(input)
	return cloneAuthUser(input), nil
}

func (r *AuthRepository) Update(_ context.Context, input *types.AuthUser) (*types.AuthUser, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if input.ID == uuid.Nil {
		return nil, ErrUserNotFound
	}
	now := time.Now().UTC()
	input.UpdatedAt = &now
	r.users[input.ID] = cloneAuthUser(input)
	return cloneAuthUser(input), nil
}

func (r *AuthRepository) UpdateStatus(ctx context.Context, actor types.ActorRef, id uuid.UUID, next types.LifecycleState, opts ...types.TransitionOption) (*types.AuthUser, error) {
	user, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	_ = opts
	_ = actor
	user.Status = next
	return r.Update(ctx, user)
}

func (r *AuthRepository) AllowedTransitions(context.Context, uuid.UUID) ([]types.LifecycleTransition, error) {
	result := make([]types.LifecycleTransition, 0, len(defaultLifecycleAllowedToActive)+len(defaultLifecycleAllowedToSuspend))
	result = append(result, defaultLifecycleAllowedToActive...)
	result = append(result, defaultLifecycleAllowedToSuspend...)
	return result, nil
}

func (r *AuthRepository) ResetPassword(context.Context, uuid.UUID, string) error {
	return nil
}

func (r *AuthRepository) ListUsers(_ context.Context, filter types.UserInventoryFilter) (types.UserInventoryPage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	users := make([]types.AuthUser, 0, len(r.users))
	for _, user := range r.users {
		if len(filter.Statuses) > 0 && !containsState(filter.Statuses, user.Status) {
			continue
		}
		if filter.Keyword != "" && !strings.Contains(strings.ToLower(user.Email), strings.ToLower(filter.Keyword)) {
			continue
		}
		users = append(users, *user)
	}
	sort.Slice(users, func(i, j int) bool {
		return users[i].CreatedAt.Before(*users[j].CreatedAt)
	})
	limit := filter.Pagination.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := filter.Pagination.Offset
	if offset < 0 {
		offset = 0
	}
	end := offset + limit
	if end > len(users) {
		end = len(users)
	}
	page := types.UserInventoryPage{
		Users:      append([]types.AuthUser{}, users[offset:end]...),
		Total:      len(users),
		NextOffset: end,
		HasMore:    end < len(users),
	}
	return page, nil
}

func containsState(states []types.LifecycleState, state types.LifecycleState) bool {
	for _, s := range states {
		if s == state {
			return true
		}
	}
	return false
}

func cloneAuthUser(user *types.AuthUser) *types.AuthUser {
	if user == nil {
		return nil
	}
	copy := *user
	if user.Metadata != nil {
		copy.Metadata = map[string]any{}
		for k, v := range user.Metadata {
			copy.Metadata[k] = v
		}
	}
	return &copy
}

// RoleRegistry is an in-memory registry used by examples.
type RoleRegistry struct {
	mu          sync.RWMutex
	roles       map[uuid.UUID]types.RoleDefinition
	assignments map[uuid.UUID][]types.RoleAssignment
}

// NewRoleRegistry provisions the registry.
func NewRoleRegistry() *RoleRegistry {
	return &RoleRegistry{
		roles:       make(map[uuid.UUID]types.RoleDefinition),
		assignments: make(map[uuid.UUID][]types.RoleAssignment),
	}
}

var _ types.RoleRegistry = (*RoleRegistry)(nil)

func (r *RoleRegistry) CreateRole(_ context.Context, input types.RoleMutation) (*types.RoleDefinition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := uuid.New()
	role := types.RoleDefinition{
		ID:          id,
		Name:        input.Name,
		Description: input.Description,
		Permissions: append([]string{}, input.Permissions...),
		IsSystem:    input.IsSystem,
		Scope:       input.Scope,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		CreatedBy:   input.ActorID,
		UpdatedBy:   input.ActorID,
	}
	r.roles[id] = role
	return &role, nil
}

func (r *RoleRegistry) UpdateRole(_ context.Context, id uuid.UUID, input types.RoleMutation) (*types.RoleDefinition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	role, ok := r.roles[id]
	if !ok {
		return nil, ErrRoleNotFound
	}
	if input.Name != "" {
		role.Name = input.Name
	}
	role.Description = input.Description
	role.Permissions = append([]string{}, input.Permissions...)
	role.UpdatedAt = time.Now().UTC()
	role.UpdatedBy = input.ActorID
	r.roles[id] = role
	return &role, nil
}

func (r *RoleRegistry) DeleteRole(_ context.Context, id uuid.UUID, _ types.ScopeFilter, _ uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.roles, id)
	delete(r.assignments, id)
	return nil
}

func (r *RoleRegistry) AssignRole(_ context.Context, userID, roleID uuid.UUID, scope types.ScopeFilter, actor uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.roles[roleID]; !ok {
		return ErrRoleNotFound
	}
	assignments := r.assignments[roleID]
	for _, existing := range assignments {
		if existing.UserID == userID && existing.Scope == scope {
			return nil
		}
	}
	assignments = append(assignments, types.RoleAssignment{
		UserID:     userID,
		RoleID:     roleID,
		RoleName:   r.roles[roleID].Name,
		Scope:      scope,
		AssignedAt: time.Now().UTC(),
		AssignedBy: actor,
	})
	r.assignments[roleID] = assignments
	return nil
}

func (r *RoleRegistry) UnassignRole(_ context.Context, userID, roleID uuid.UUID, scope types.ScopeFilter, _ uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	assignments, ok := r.assignments[roleID]
	if !ok {
		return ErrAssignmentNotFound
	}
	filtered := assignments[:0]
	for _, assignment := range assignments {
		if assignment.UserID == userID && assignment.Scope == scope {
			continue
		}
		filtered = append(filtered, assignment)
	}
	r.assignments[roleID] = append([]types.RoleAssignment{}, filtered...)
	return nil
}

func (r *RoleRegistry) ListRoles(_ context.Context, filter types.RoleFilter) (types.RolePage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	roles := make([]types.RoleDefinition, 0, len(r.roles))
	for _, role := range r.roles {
		if !filter.IncludeSystem && role.IsSystem {
			continue
		}
		if filter.Keyword != "" && !strings.Contains(strings.ToLower(role.Name), strings.ToLower(filter.Keyword)) {
			continue
		}
		roles = append(roles, role)
	}
	return types.RolePage{Roles: roles, Total: len(roles)}, nil
}

func (r *RoleRegistry) GetRole(_ context.Context, id uuid.UUID, _ types.ScopeFilter) (*types.RoleDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	role, ok := r.roles[id]
	if !ok {
		return nil, ErrRoleNotFound
	}
	copy := role
	return &copy, nil
}

func (r *RoleRegistry) ListAssignments(_ context.Context, filter types.RoleAssignmentFilter) ([]types.RoleAssignment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	results := make([]types.RoleAssignment, 0)
	for roleID, assignments := range r.assignments {
		if filter.RoleID != uuid.Nil && filter.RoleID != roleID {
			continue
		}
		for _, assignment := range assignments {
			if filter.UserID != uuid.Nil && assignment.UserID != filter.UserID {
				continue
			}
			results = append(results, assignment)
		}
	}
	return results, nil
}

// ActivityStore logs activity entries in memory and exposes query helpers.
type ActivityStore struct {
	mu      sync.RWMutex
	records []types.ActivityRecord
}

// NewActivityStore provisions the store.
func NewActivityStore() *ActivityStore {
	return &ActivityStore{}
}

var (
	_ types.ActivitySink       = (*ActivityStore)(nil)
	_ types.ActivityRepository = (*ActivityStore)(nil)
)

func (s *ActivityStore) Log(_ context.Context, record types.ActivityRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.ID == uuid.Nil {
		record.ID = uuid.New()
	}
	if record.OccurredAt.IsZero() {
		record.OccurredAt = time.Now().UTC()
	}
	s.records = append([]types.ActivityRecord{record}, s.records...)
	return nil
}

func (s *ActivityStore) ListActivity(_ context.Context, filter types.ActivityFilter) (types.ActivityPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	filtered := make([]types.ActivityRecord, 0, len(s.records))
	for _, record := range s.records {
		if filter.Scope.TenantID != zeroUUID && record.TenantID != filter.Scope.TenantID {
			continue
		}
		if len(filter.Verbs) > 0 && !containsVerb(filter.Verbs, record.Verb) {
			continue
		}
		filtered = append(filtered, record)
	}
	limit := filter.Pagination.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	offset := filter.Pagination.Offset
	if offset < 0 {
		offset = 0
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return types.ActivityPage{
		Records:    append([]types.ActivityRecord{}, filtered[offset:end]...),
		Total:      len(filtered),
		NextOffset: end,
		HasMore:    end < len(filtered),
	}, nil
}

func (s *ActivityStore) ActivityStats(_ context.Context, filter types.ActivityStatsFilter) (types.ActivityStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stats := types.ActivityStats{
		ByVerb: make(map[string]int),
	}
	for _, record := range s.records {
		if filter.Scope.TenantID != zeroUUID && record.TenantID != filter.Scope.TenantID {
			continue
		}
		stats.Total++
		stats.ByVerb[record.Verb]++
	}
	return stats, nil
}

func containsVerb(verbs []string, verb string) bool {
	for _, v := range verbs {
		if v == verb {
			return true
		}
	}
	return false
}

// ProfileRepository stores profile rows in memory.
type ProfileRepository struct {
	mu       sync.RWMutex
	profiles map[string]types.UserProfile
}

// NewProfileRepository provisions the repo.
func NewProfileRepository() *ProfileRepository {
	return &ProfileRepository{
		profiles: make(map[string]types.UserProfile),
	}
}

var _ types.ProfileRepository = (*ProfileRepository)(nil)

func (r *ProfileRepository) GetProfile(_ context.Context, userID uuid.UUID, scope types.ScopeFilter) (*types.UserProfile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	profile, ok := r.profiles[profileKey(userID, scope)]
	if !ok {
		return nil, nil
	}
	copy := profile
	return &copy, nil
}

func (r *ProfileRepository) UpsertProfile(_ context.Context, profile types.UserProfile) (*types.UserProfile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if profile.UserID == uuid.Nil {
		profile.UserID = uuid.New()
	}
	if profile.Contact == nil {
		profile.Contact = make(map[string]any)
	}
	if profile.Metadata == nil {
		profile.Metadata = make(map[string]any)
	}
	now := time.Now().UTC()
	if profile.CreatedAt.IsZero() {
		profile.CreatedAt = now
	}
	profile.UpdatedAt = now
	r.profiles[profileKey(profile.UserID, profile.Scope)] = profile
	copy := profile
	return &copy, nil
}

func profileKey(userID uuid.UUID, scope types.ScopeFilter) string {
	return fmt.Sprintf("%s:%s:%s", userID, scope.TenantID, scope.OrgID)
}

// PreferenceRepository persists preference records in memory.
type PreferenceRepository struct {
	mu       sync.RWMutex
	records  map[string]types.PreferenceRecord
	versions map[string]int
}

// NewPreferenceRepository provisions the repo.
func NewPreferenceRepository() *PreferenceRepository {
	return &PreferenceRepository{
		records:  make(map[string]types.PreferenceRecord),
		versions: make(map[string]int),
	}
}

var _ types.PreferenceRepository = (*PreferenceRepository)(nil)

func (r *PreferenceRepository) ListPreferences(_ context.Context, filter types.PreferenceFilter) ([]types.PreferenceRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	results := make([]types.PreferenceRecord, 0, len(r.records))
	for _, record := range r.records {
		if filter.UserID != uuid.Nil && record.UserID != filter.UserID {
			continue
		}
		if filter.Scope.TenantID != uuid.Nil && record.Scope.TenantID != filter.Scope.TenantID {
			continue
		}
		if filter.Scope.OrgID != uuid.Nil && record.Scope.OrgID != filter.Scope.OrgID {
			continue
		}
		if filter.Level != "" && record.Level != filter.Level {
			continue
		}
		if len(filter.Keys) > 0 && !containsKey(filter.Keys, record.Key) {
			continue
		}
		results = append(results, record)
	}
	return results, nil
}

func (r *PreferenceRepository) UpsertPreference(_ context.Context, record types.PreferenceRecord) (*types.PreferenceRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := preferenceKey(record)
	version := r.versions[key] + 1
	r.versions[key] = version
	record.ID = uuid.New()
	record.Version = version
	now := time.Now().UTC()
	record.CreatedAt = now
	record.UpdatedAt = now
	r.records[key] = record
	copy := record
	return &copy, nil
}

func (r *PreferenceRepository) DeletePreference(_ context.Context, userID uuid.UUID, scope types.ScopeFilter, level types.PreferenceLevel, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.records, fmt.Sprintf("%s:%s:%s:%s:%s", userID, scope.TenantID, scope.OrgID, level, strings.ToLower(key)))
	return nil
}

func preferenceKey(record types.PreferenceRecord) string {
	return fmt.Sprintf("%s:%s:%s:%s:%s", record.UserID, record.Scope.TenantID, record.Scope.OrgID, record.Level, strings.ToLower(record.Key))
}

func containsKey(keys []string, target string) bool {
	for _, key := range keys {
		if strings.EqualFold(key, target) {
			return true
		}
	}
	return false
}
