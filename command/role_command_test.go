package command

import (
	"context"
	"errors"
	"testing"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCreateRoleCommand_PopulatesResult(t *testing.T) {
	reg := &fakeRoleRegistry{}
	cmd := NewCreateRoleCommand(reg, scope.NopGuard())
	result := &types.RoleDefinition{}

	err := cmd.Execute(context.Background(), CreateRoleInput{
		Name:   "Editors",
		Actor:  types.ActorRef{ID: uuid.New()},
		Result: result,
	})

	require.NoError(t, err)
	require.Equal(t, "Editors", reg.lastMutation.Name)
	require.NotEqual(t, uuid.Nil, result.ID)
}

func TestAssignRoleCommand_ValidatesUser(t *testing.T) {
	reg := &fakeRoleRegistry{}
	cmd := NewAssignRoleCommand(reg, scope.NopGuard())

	err := cmd.Execute(context.Background(), AssignRoleInput{
		RoleID: uuid.New(),
		Actor:  types.ActorRef{ID: uuid.New()},
	})

	require.ErrorIs(t, err, ErrUserIDRequired)
}

type fakeRoleRegistry struct {
	lastMutation types.RoleMutation
	lastAssign   struct {
		UserID uuid.UUID
		RoleID uuid.UUID
	}
}

func (f *fakeRoleRegistry) CreateRole(ctx context.Context, input types.RoleMutation) (*types.RoleDefinition, error) {
	f.lastMutation = input
	return &types.RoleDefinition{
		ID:   uuid.New(),
		Name: input.Name,
	}, nil
}

func (f *fakeRoleRegistry) UpdateRole(context.Context, uuid.UUID, types.RoleMutation) (*types.RoleDefinition, error) {
	return nil, nil
}

func (f *fakeRoleRegistry) DeleteRole(context.Context, uuid.UUID, types.ScopeFilter, uuid.UUID) error {
	return nil
}

func (f *fakeRoleRegistry) AssignRole(ctx context.Context, userID, roleID uuid.UUID, _ types.ScopeFilter, _ uuid.UUID) error {
	f.lastAssign.UserID = userID
	f.lastAssign.RoleID = roleID
	return nil
}

func (f *fakeRoleRegistry) UnassignRole(context.Context, uuid.UUID, uuid.UUID, types.ScopeFilter, uuid.UUID) error {
	return nil
}

func (f *fakeRoleRegistry) ListRoles(context.Context, types.RoleFilter) (types.RolePage, error) {
	return types.RolePage{}, nil
}

func (f *fakeRoleRegistry) GetRole(context.Context, uuid.UUID, types.ScopeFilter) (*types.RoleDefinition, error) {
	return nil, nil
}

func (f *fakeRoleRegistry) ListAssignments(context.Context, types.RoleAssignmentFilter) ([]types.RoleAssignment, error) {
	return nil, nil
}

func TestGrantValidationRejectsInvalidCatalogResultsBeforeMutation(t *testing.T) {
	lookupErr := errors.New("catalog unavailable")
	roleID := uuid.New()
	tenant := uuid.New()
	scope := types.ScopeFilter{TenantID: tenant}.WithLabel("group", uuid.New())
	for _, tc := range []struct {
		name string
		role *types.RoleDefinition
		err  error
	}{
		{name: "missing definition"},
		{name: "wrong ID", role: &types.RoleDefinition{ID: uuid.New(), Scope: types.ScopeFilter{TenantID: tenant}}},
		{name: "wrong catalog", role: &types.RoleDefinition{ID: roleID}},
		{name: "labeled catalog", role: &types.RoleDefinition{ID: roleID, Scope: scope}},
		{name: "lookup failure", err: lookupErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &candidateRegistry{role: tc.role, lookupErr: tc.err}
			cfg := RoleAssignmentCommandConfig{Registry: repo, RequireGrantValidation: true, GrantValidator: types.RoleGrantValidatorFunc(func(context.Context, types.RoleGrantCheck) error {
				t.Fatal("invalid definition reached validator")
				return nil
			})}
			input := AssignRoleInput{UserID: uuid.New(), RoleID: roleID, Scope: scope, Actor: types.ActorRef{ID: uuid.New()}}
			err := NewAssignRoleCommandWithConfig(cfg).Execute(context.Background(), input)
			want := types.ErrInvalidRoleGrantDefinition
			if tc.err != nil {
				want = lookupErr
			}
			require.ErrorIs(t, err, want)
			require.Empty(t, repo.lookupScope.Labels)
			require.Equal(t, tenant, repo.lookupScope.TenantID)
			require.Zero(t, repo.writes)
		})
	}
}

func TestGrantValidationPreservesGenericPolicyDenial(t *testing.T) {
	repo := &candidateRegistry{}
	guard := scope.NewGuard(nil, types.AuthorizationPolicyFunc(func(context.Context, types.PolicyCheck) error { return types.ErrUnauthorizedScope }))
	cfg := RoleAssignmentCommandConfig{Registry: repo, ScopeGuard: guard, GrantValidator: types.RoleGrantValidatorFunc(func(context.Context, types.RoleGrantCheck) error {
		t.Fatal("scope denial must precede candidate validation")
		return nil
	})}
	input := AssignRoleInput{UserID: uuid.New(), RoleID: uuid.New(), Actor: types.ActorRef{ID: uuid.New()}}
	require.ErrorIs(t, NewAssignRoleCommandWithConfig(cfg).Execute(context.Background(), input), types.ErrUnauthorizedScope)
	require.ErrorIs(t, NewUnassignRoleCommandWithConfig(cfg).Execute(context.Background(), UnassignRoleInput(input)), types.ErrUnauthorizedScope)
	require.Zero(t, repo.lookups)
	require.Zero(t, repo.writes)
}

type candidateRegistry struct {
	fakeRoleRegistry
	role            *types.RoleDefinition
	lookupErr       error
	lookupScope     types.ScopeFilter
	lookups, writes int
}

func (r *candidateRegistry) GetRole(_ context.Context, _ uuid.UUID, scope types.ScopeFilter) (*types.RoleDefinition, error) {
	r.lookups++
	r.lookupScope = scope
	return r.role, r.lookupErr
}
func (r *candidateRegistry) AssignRole(context.Context, uuid.UUID, uuid.UUID, types.ScopeFilter, uuid.UUID) error {
	r.writes++
	return nil
}
func (r *candidateRegistry) UnassignRole(context.Context, uuid.UUID, uuid.UUID, types.ScopeFilter, uuid.UUID) error {
	r.writes++
	return nil
}
