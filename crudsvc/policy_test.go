package crudsvc

import (
	"context"
	"errors"
	"testing"

	"github.com/goliatone/go-crud"
	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-users/command"
	"github.com/goliatone/go-users/crudguard"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/preferences"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestUserServiceSupportIndexFilters(t *testing.T) {
	t.Helper()
	actorID := uuid.New()
	inventory := &stubUserInventoryQuery{
		result: types.UserInventoryPage{
			Users: []types.AuthUser{
				{ID: actorID, Email: "owner@example.com"},
				{ID: uuid.New(), Email: "other@example.com"},
			},
			Total: 2,
		},
	}
	svc := NewUserService(UserServiceConfig{
		Guard: &stubGuardAdapter{
			result: crudguard.GuardResult{
				Actor: types.ActorRef{ID: actorID, Type: types.ActorRoleSupport},
				Scope: types.ScopeFilter{TenantID: uuid.New()},
			},
		},
		Inventory: inventory,
	})

	ctx := newTestCrudContext(context.Background())
	records, total, err := svc.Index(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, []uuid.UUID{actorID}, inventory.lastFilter.UserIDs)
	require.Len(t, records, 1)
	require.Equal(t, actorID, records[0].ID)
}

func TestUserServiceSupportShowDenied(t *testing.T) {
	t.Helper()
	actorID := uuid.New()
	targetID := uuid.New()
	svc := NewUserService(UserServiceConfig{
		Guard: &stubGuardAdapter{
			result: crudguard.GuardResult{
				Actor: types.ActorRef{ID: actorID, Type: types.ActorRoleSupport},
				Scope: types.ScopeFilter{},
			},
		},
		AuthRepo: &stubAuthRepo{
			users: map[uuid.UUID]*types.AuthUser{
				targetID: {ID: targetID, Email: "target@example.com"},
			},
		},
	})
	ctx := newTestCrudContext(context.Background())
	_, err := svc.Show(ctx, targetID.String(), nil)
	require.Error(t, err)
}

func TestPreferenceServiceSupportRestrictions(t *testing.T) {
	t.Helper()
	actorID := uuid.New()
	otherID := uuid.New()
	repo := &stubPreferenceRepo{
		records: []types.PreferenceRecord{
			{UserID: actorID, Key: "theme"},
			{UserID: otherID, Key: "timezone"},
		},
	}
	upsert := &stubPreferenceUpsertCmd{}
	svc := NewPreferenceService(PreferenceServiceConfig{
		Guard: &stubGuardAdapter{
			result: crudguard.GuardResult{
				Actor: types.ActorRef{ID: actorID, Type: types.ActorRoleSupport},
			},
		},
		Repo:   repo,
		Store:  repo,
		Upsert: upsert,
		Delete: stubPreferenceDeleteCmd{},
	})
	ctx := newTestCrudContext(context.Background())
	records, total, err := svc.Index(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, records, 1)
	require.Equal(t, actorID, records[0].UserID)

	_, err = svc.Create(ctx, &preferences.Record{
		UserID: otherID,
		Key:    "admin_override",
		Value:  map[string]any{"mode": "dark"},
	})
	require.Error(t, err)

	_, err = svc.Create(ctx, &preferences.Record{
		UserID: actorID,
		Key:    "notifications",
		Value:  map[string]any{"channel": "email"},
	})
	require.NoError(t, err)
	require.Equal(t, actorID, upsert.lastInput.UserID)
}

func TestActivityServiceMasksIPForTenantAdmin(t *testing.T) {
	t.Helper()
	actorID := uuid.New()
	svc := NewActivityService(ActivityServiceConfig{
		Guard: &stubGuardAdapter{
			result: crudguard.GuardResult{
				Actor: types.ActorRef{ID: actorID, Type: types.ActorRoleTenantAdmin},
			},
		},
		FeedQuery: &stubActivityFeedQuery{
			result: types.ActivityPage{
				Records: []types.ActivityRecord{
					{ID: uuid.New(), UserID: actorID, ActorID: actorID, IP: "10.10.10.10", Channel: "users"},
				},
				Total: 1,
			},
		},
	})
	ctx := newTestCrudContext(context.Background())
	records, total, err := svc.Index(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, records, 1)
	require.Empty(t, records[0].IP)
}

// ----- test stubs -----

type stubGuardAdapter struct {
	result    crudguard.GuardResult
	err       error
	lastInput crudguard.GuardInput
}

func (s *stubGuardAdapter) Enforce(in crudguard.GuardInput) (crudguard.GuardResult, error) {
	s.lastInput = in
	if s.err != nil {
		return crudguard.GuardResult{}, s.err
	}
	return s.result, nil
}

type stubUserInventoryQuery struct {
	result     types.UserInventoryPage
	lastFilter types.UserInventoryFilter
}

func (s *stubUserInventoryQuery) Query(_ context.Context, filter types.UserInventoryFilter) (types.UserInventoryPage, error) {
	s.lastFilter = filter
	return s.result, nil
}

type stubAuthRepo struct {
	users map[uuid.UUID]*types.AuthUser
}

func (s *stubAuthRepo) GetByID(_ context.Context, id uuid.UUID) (*types.AuthUser, error) {
	if s.users == nil {
		return nil, nil
	}
	if user, ok := s.users[id]; ok {
		copy := *user
		return &copy, nil
	}
	return nil, nil
}

func (s *stubAuthRepo) GetByIdentifier(context.Context, string) (*types.AuthUser, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAuthRepo) Create(context.Context, *types.AuthUser) (*types.AuthUser, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAuthRepo) Update(context.Context, *types.AuthUser) (*types.AuthUser, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAuthRepo) UpdateStatus(context.Context, types.ActorRef, uuid.UUID, types.LifecycleState, ...types.TransitionOption) (*types.AuthUser, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAuthRepo) AllowedTransitions(context.Context, uuid.UUID) ([]types.LifecycleTransition, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAuthRepo) ResetPassword(context.Context, uuid.UUID, string) error {
	return errors.New("not implemented")
}

type stubPreferenceRepo struct {
	records []types.PreferenceRecord
}

func (s *stubPreferenceRepo) ListPreferences(context.Context, types.PreferenceFilter) ([]types.PreferenceRecord, error) {
	dst := make([]types.PreferenceRecord, len(s.records))
	copy(dst, s.records)
	return dst, nil
}

func (s *stubPreferenceRepo) UpsertPreference(_ context.Context, record types.PreferenceRecord) (*types.PreferenceRecord, error) {
	s.records = append(s.records, record)
	return &record, nil
}

func (s *stubPreferenceRepo) DeletePreference(context.Context, uuid.UUID, types.ScopeFilter, types.PreferenceLevel, string) error {
	return nil
}

func (s *stubPreferenceRepo) GetByID(context.Context, string, ...repository.SelectCriteria) (*preferences.Record, error) {
	return nil, errors.New("not implemented")
}

type stubPreferenceUpsertCmd struct {
	lastInput command.PreferenceUpsertInput
	err       error
}

func (s *stubPreferenceUpsertCmd) Execute(_ context.Context, input command.PreferenceUpsertInput) error {
	s.lastInput = input
	return s.err
}

type stubPreferenceDeleteCmd struct{}

func (stubPreferenceDeleteCmd) Execute(context.Context, command.PreferenceDeleteInput) error {
	return nil
}

type stubActivityFeedQuery struct {
	result types.ActivityPage
}

func (s *stubActivityFeedQuery) Query(context.Context, types.ActivityFilter) (types.ActivityPage, error) {
	return s.result, nil
}

type testCrudContext struct {
	ctx     context.Context
	queries map[string]string
}

func newTestCrudContext(ctx context.Context) *testCrudContext {
	return &testCrudContext{
		ctx:     ctx,
		queries: map[string]string{},
	}
}

func (t *testCrudContext) UserContext() context.Context {
	return t.ctx
}

func (t *testCrudContext) Params(string, ...string) string {
	return ""
}

func (t *testCrudContext) BodyParser(out any) error {
	return nil
}

func (t *testCrudContext) Query(key string, defaultValue ...string) string {
	if v, ok := t.queries[key]; ok {
		return v
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}

func (t *testCrudContext) QueryInt(string, ...int) int {
	return 0
}

func (t *testCrudContext) Queries() map[string]string {
	return t.queries
}

func (t *testCrudContext) Body() []byte {
	return nil
}

func (t *testCrudContext) Status(int) crud.Response {
	return t
}

func (t *testCrudContext) JSON(any, ...string) error {
	return nil
}

func (t *testCrudContext) SendStatus(int) error {
	return nil
}
