package command

import (
	"context"
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
	lastDelete   uuid.UUID
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
