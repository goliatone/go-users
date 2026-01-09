package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/goliatone/go-users/command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/query"
	"github.com/goliatone/go-users/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestService_MultiTenantIsolation(t *testing.T) {
	ctx := context.Background()
	tenantA := uuid.New()
	tenantB := uuid.New()

	authRepo := newMTAuthRepo()
	userTenantA := authRepo.seedUser(tenantA)

	roleRegistry := newMTRoleRegistry()
	activityStore := newMTActivityStore()
	preferenceRepo := newMTPreferenceRepo()
	profileRepo := newMTProfileRepo()

	actorA := types.ActorRef{ID: uuid.New(), Type: "tenant-admin"}
	actorB := types.ActorRef{ID: uuid.New(), Type: "tenant-admin"}

	resolver := staticScopeResolver{
		scopes: map[uuid.UUID]types.ScopeFilter{
			actorA.ID: {TenantID: tenantA},
			actorB.ID: {TenantID: tenantB},
		},
	}
	policy := tenantPolicy{
		allowed: map[uuid.UUID]uuid.UUID{
			actorA.ID: tenantA,
			actorB.ID: tenantB,
		},
	}

	svc := service.New(service.Config{
		AuthRepository:       authRepo,
		InventoryRepository:  authRepo,
		RoleRegistry:         roleRegistry,
		ActivitySink:         activityStore,
		ActivityRepository:   activityStore,
		ProfileRepository:    profileRepo,
		PreferenceRepository: preferenceRepo,
		Hooks:                types.Hooks{},
		Logger:               types.NopLogger{},
		ScopeResolver:        resolver,
		AuthorizationPolicy:  policy,
	})

	scopeTenantA := types.ScopeFilter{TenantID: tenantA}

	// Tenant A actor can transition their user.
	err := svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
		UserID: userTenantA,
		Target: types.LifecycleStateSuspended,
		Actor:  actorA,
		Scope:  scopeTenantA,
	})
	require.NoError(t, err)

	// Tenant B actor attempting to target tenant A scope is rejected.
	err = svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
		UserID: userTenantA,
		Target: types.LifecycleStateActive,
		Actor:  actorB,
		Scope:  scopeTenantA,
	})
	require.ErrorIs(t, err, types.ErrUnauthorizedScope)

	// Create roles for each tenant via the command layer so guard enforcement is exercised.
	roleResult := &types.RoleDefinition{}
	err = svc.Commands().CreateRole.Execute(ctx, command.CreateRoleInput{
		Name:   "Tenant A Editors",
		Actor:  actorA,
		Scope:  types.ScopeFilter{},
		Result: roleResult,
	})
	require.NoError(t, err)

	err = svc.Commands().CreateRole.Execute(ctx, command.CreateRoleInput{
		Name:  "Tenant B Viewers",
		Actor: actorB,
		Scope: types.ScopeFilter{},
	})
	require.NoError(t, err)

	rolePage, err := svc.Queries().RoleList.Query(ctx, types.RoleFilter{
		Actor: actorA,
		Scope: types.ScopeFilter{},
	})
	require.NoError(t, err)
	require.Equal(t, 1, len(rolePage.Roles))
	require.Equal(t, tenantA, rolePage.Roles[0].Scope.TenantID)
	require.Equal(t, "Tenant A Editors", rolePage.Roles[0].Name)

	// Seed an activity record for tenant B to prove filtering occurs.
	err = svc.Commands().LogActivity.Execute(ctx, command.ActivityLogInput{
		Record: types.ActivityRecord{
			ID:       uuid.New(),
			Verb:     "demo.other",
			Channel:  "tests",
			TenantID: tenantB,
		},
	})
	require.NoError(t, err)

	feed, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
		Actor:      actorA,
		Scope:      types.ScopeFilter{},
		Pagination: types.Pagination{Limit: 10},
	})
	require.NoError(t, err)
	require.NotEmpty(t, feed.Records)
	for _, rec := range feed.Records {
		require.Equal(t, tenantA, rec.TenantID)
	}

	// Tenant A can upsert preferences in their scope.
	err = svc.Commands().PreferenceUpsert.Execute(ctx, command.PreferenceUpsertInput{
		UserID: userTenantA,
		Key:    "notifications.email",
		Value: map[string]any{
			"frequency": "daily",
		},
		Actor: actorA,
	})
	require.NoError(t, err)

	// Tenant B cannot target tenant A scope.
	err = svc.Commands().PreferenceUpsert.Execute(ctx, command.PreferenceUpsertInput{
		UserID: userTenantA,
		Key:    "notifications.email",
		Value: map[string]any{
			"frequency": "weekly",
		},
		Scope: scopeTenantA,
		Actor: actorB,
	})
	require.ErrorIs(t, err, types.ErrUnauthorizedScope)

	snapshot, err := svc.Queries().Preferences.Query(ctx, query.PreferenceQueryInput{
		UserID: userTenantA,
		Actor:  actorA,
	})
	require.NoError(t, err)
	val, ok := snapshot.Effective["notifications.email"]
	require.True(t, ok)
	if pref, ok := val.(map[string]any); ok {
		require.Equal(t, "daily", pref["frequency"])
	} else {
		require.Equal(t, "daily", val)
	}

	foreignSnapshot, err := svc.Queries().Preferences.Query(ctx, query.PreferenceQueryInput{
		UserID: userTenantA,
		Actor:  actorB,
	})
	require.NoError(t, err)
	_, ok = foreignSnapshot.Effective["notifications.email"]
	require.False(t, ok)
}

// --- Test doubles ---

type staticScopeResolver struct {
	scopes map[uuid.UUID]types.ScopeFilter
}

func (r staticScopeResolver) ResolveScope(_ context.Context, actor types.ActorRef, requested types.ScopeFilter) (types.ScopeFilter, error) {
	if requested.TenantID != uuid.Nil || requested.OrgID != uuid.Nil {
		return requested, nil
	}
	if resolved, ok := r.scopes[actor.ID]; ok {
		return resolved, nil
	}
	return requested, nil
}

type tenantPolicy struct {
	allowed map[uuid.UUID]uuid.UUID
}

func (p tenantPolicy) Authorize(_ context.Context, check types.PolicyCheck) error {
	tenant := p.allowed[check.Actor.ID]
	if tenant == uuid.Nil || check.Scope.TenantID == uuid.Nil {
		return nil
	}
	if tenant != check.Scope.TenantID {
		return types.ErrUnauthorizedScope
	}
	return nil
}

type mtAuthRepo struct {
	users map[uuid.UUID]*mtUser
}

type mtUser struct {
	user   *types.AuthUser
	tenant uuid.UUID
}

func newMTAuthRepo() *mtAuthRepo {
	return &mtAuthRepo{
		users: make(map[uuid.UUID]*mtUser),
	}
}

func (r *mtAuthRepo) seedUser(tenant uuid.UUID) uuid.UUID {
	id := uuid.New()
	now := time.Now().UTC()
	r.users[id] = &mtUser{
		user: &types.AuthUser{
			ID:        id,
			Status:    types.LifecycleStateActive,
			CreatedAt: &now,
		},
		tenant: tenant,
	}
	return id
}

func (r *mtAuthRepo) GetByID(_ context.Context, id uuid.UUID) (*types.AuthUser, error) {
	entry, ok := r.users[id]
	if !ok {
		return nil, types.ErrUserIDRequired
	}
	return entry.user, nil
}

func (r *mtAuthRepo) GetByIdentifier(_ context.Context, identifier string) (*types.AuthUser, error) {
	for _, entry := range r.users {
		if entry.user.Email == identifier {
			return entry.user, nil
		}
	}
	return nil, types.ErrUserIDRequired
}

func (r *mtAuthRepo) Create(_ context.Context, input *types.AuthUser) (*types.AuthUser, error) {
	if input.ID == uuid.Nil {
		input.ID = uuid.New()
	}
	r.users[input.ID] = &mtUser{user: input}
	return input, nil
}

func (r *mtAuthRepo) Update(_ context.Context, input *types.AuthUser) (*types.AuthUser, error) {
	r.users[input.ID] = &mtUser{user: input}
	return input, nil
}

func (r *mtAuthRepo) UpdateStatus(ctx context.Context, actor types.ActorRef, id uuid.UUID, next types.LifecycleState, _ ...types.TransitionOption) (*types.AuthUser, error) {
	user, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	user.Status = next
	_ = actor
	return user, nil
}

func (r *mtAuthRepo) AllowedTransitions(context.Context, uuid.UUID) ([]types.LifecycleTransition, error) {
	return nil, nil
}

func (r *mtAuthRepo) ResetPassword(context.Context, uuid.UUID, string) error {
	return nil
}

func (r *mtAuthRepo) ListUsers(_ context.Context, filter types.UserInventoryFilter) (types.UserInventoryPage, error) {
	var users []types.AuthUser
	for _, entry := range r.users {
		if filter.Scope.TenantID != uuid.Nil && entry.tenant != filter.Scope.TenantID {
			continue
		}
		users = append(users, *entry.user)
	}
	return types.UserInventoryPage{
		Users: users,
		Total: len(users),
	}, nil
}

type mtRoleRegistry struct {
	roles map[uuid.UUID][]types.RoleDefinition
}

func newMTRoleRegistry() *mtRoleRegistry {
	return &mtRoleRegistry{
		roles: make(map[uuid.UUID][]types.RoleDefinition),
	}
}

func (r *mtRoleRegistry) CreateRole(_ context.Context, input types.RoleMutation) (*types.RoleDefinition, error) {
	role := types.RoleDefinition{
		ID:    uuid.New(),
		Name:  input.Name,
		Order: input.Order,
		Scope: input.Scope,
	}
	tenant := input.Scope.TenantID
	r.roles[tenant] = append(r.roles[tenant], role)
	return &role, nil
}

func (r *mtRoleRegistry) UpdateRole(_ context.Context, id uuid.UUID, input types.RoleMutation) (*types.RoleDefinition, error) {
	return &types.RoleDefinition{ID: id, Name: input.Name, Order: input.Order, Scope: input.Scope}, nil
}

func (r *mtRoleRegistry) DeleteRole(context.Context, uuid.UUID, types.ScopeFilter, uuid.UUID) error {
	return nil
}

func (r *mtRoleRegistry) AssignRole(context.Context, uuid.UUID, uuid.UUID, types.ScopeFilter, uuid.UUID) error {
	return nil
}

func (r *mtRoleRegistry) UnassignRole(context.Context, uuid.UUID, uuid.UUID, types.ScopeFilter, uuid.UUID) error {
	return nil
}

func (r *mtRoleRegistry) ListRoles(_ context.Context, filter types.RoleFilter) (types.RolePage, error) {
	roles := append([]types.RoleDefinition(nil), r.roles[filter.Scope.TenantID]...)
	return types.RolePage{
		Roles: roles,
		Total: len(roles),
	}, nil
}

func (r *mtRoleRegistry) GetRole(context.Context, uuid.UUID, types.ScopeFilter) (*types.RoleDefinition, error) {
	return nil, nil
}

func (r *mtRoleRegistry) ListAssignments(context.Context, types.RoleAssignmentFilter) ([]types.RoleAssignment, error) {
	return nil, nil
}

type mtActivityStore struct {
	records []types.ActivityRecord
}

func newMTActivityStore() *mtActivityStore {
	return &mtActivityStore{}
}

func (s *mtActivityStore) Log(_ context.Context, record types.ActivityRecord) error {
	s.records = append(s.records, record)
	return nil
}

func (s *mtActivityStore) ListActivity(_ context.Context, filter types.ActivityFilter) (types.ActivityPage, error) {
	var records []types.ActivityRecord
	for _, record := range s.records {
		if filter.Scope.TenantID != uuid.Nil && record.TenantID != filter.Scope.TenantID {
			continue
		}
		records = append(records, record)
	}
	return types.ActivityPage{
		Records: records,
		Total:   len(records),
	}, nil
}

func (s *mtActivityStore) ActivityStats(_ context.Context, filter types.ActivityStatsFilter) (types.ActivityStats, error) {
	stats := types.ActivityStats{
		ByVerb: make(map[string]int),
	}
	for _, record := range s.records {
		if filter.Scope.TenantID != uuid.Nil && record.TenantID != filter.Scope.TenantID {
			continue
		}
		stats.Total++
		stats.ByVerb[record.Verb]++
	}
	return stats, nil
}

type mtPreferenceRepo struct {
	records map[string]types.PreferenceRecord
}

func newMTPreferenceRepo() *mtPreferenceRepo {
	return &mtPreferenceRepo{
		records: make(map[string]types.PreferenceRecord),
	}
}

func (r *mtPreferenceRepo) ListPreferences(_ context.Context, filter types.PreferenceFilter) ([]types.PreferenceRecord, error) {
	var result []types.PreferenceRecord
	for _, record := range r.records {
		if filter.UserID != uuid.Nil && record.UserID != filter.UserID {
			continue
		}
		if filter.Scope.TenantID != uuid.Nil && record.Scope.TenantID != filter.Scope.TenantID {
			continue
		}
		result = append(result, record)
	}
	return result, nil
}

func (r *mtPreferenceRepo) UpsertPreference(_ context.Context, record types.PreferenceRecord) (*types.PreferenceRecord, error) {
	key := preferenceKey(record.UserID, record.Scope, record.Key)
	copy := record
	r.records[key] = copy
	return &copy, nil
}

func (r *mtPreferenceRepo) DeletePreference(_ context.Context, userID uuid.UUID, scope types.ScopeFilter, _ types.PreferenceLevel, keyName string) error {
	delete(r.records, preferenceKey(userID, scope, keyName))
	return nil
}

func preferenceKey(userID uuid.UUID, scope types.ScopeFilter, keyName string) string {
	return userID.String() + ":" + scope.TenantID.String() + ":" + keyName
}

type mtProfileRepo struct {
	records map[string]*types.UserProfile
}

func newMTProfileRepo() *mtProfileRepo {
	return &mtProfileRepo{
		records: make(map[string]*types.UserProfile),
	}
}

func (r *mtProfileRepo) GetProfile(_ context.Context, userID uuid.UUID, scope types.ScopeFilter) (*types.UserProfile, error) {
	return r.records[profileKey(userID, scope)], nil
}

func (r *mtProfileRepo) UpsertProfile(_ context.Context, profile types.UserProfile) (*types.UserProfile, error) {
	key := profileKey(profile.UserID, profile.Scope)
	copy := profile
	r.records[key] = &copy
	return &copy, nil
}

func profileKey(userID uuid.UUID, scope types.ScopeFilter) string {
	return userID.String() + ":" + scope.TenantID.String()
}
