package crudsvc

import (
	"context"
	"testing"

	auth "github.com/goliatone/go-auth"
	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/command"
	"github.com/goliatone/go-users/crudguard"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestUserServiceCreateDirectMode(t *testing.T) {
	t.Helper()
	createCmd := &stubUserCreateCmd{}
	inviteCmd := &stubUserInviteCmd{}
	guard := &stubGuardAdapter{
		result: crudguard.GuardResult{
			Actor: types.ActorRef{ID: uuid.New(), Type: types.ActorRoleSystemAdmin},
			Scope: types.ScopeFilter{TenantID: uuid.New()},
		},
	}
	svc := NewUserService(UserServiceConfig{
		Guard:  guard,
		Create: createCmd,
		Invite: inviteCmd,
	})
	ctx := newTestCrudContext(context.Background())
	record := &auth.User{
		Email:    "direct@example.com",
		Metadata: map[string]any{"source": "api"},
	}

	created, err := svc.Create(ctx, record)
	require.NoError(t, err)
	require.NotNil(t, created)
	require.Equal(t, 1, createCmd.calls)
	require.Equal(t, 0, inviteCmd.calls)
	require.Equal(t, "direct@example.com", createCmd.lastInput.User.Email)
	require.Equal(t, "api", createCmd.lastInput.User.Metadata["source"])
}

func TestUserServiceCreateInviteModeQuery(t *testing.T) {
	t.Helper()
	createCmd := &stubUserCreateCmd{}
	inviteCmd := &stubUserInviteCmd{}
	guard := &stubGuardAdapter{
		result: crudguard.GuardResult{
			Actor: types.ActorRef{ID: uuid.New(), Type: types.ActorRoleSystemAdmin},
			Scope: types.ScopeFilter{OrgID: uuid.New()},
		},
	}
	svc := NewUserService(UserServiceConfig{
		Guard:  guard,
		Create: createCmd,
		Invite: inviteCmd,
	})
	ctx := newTestCrudContext(context.Background())
	ctx.queries["mode"] = "invite"
	record := &auth.User{
		Email:     "invite@example.com",
		Username:  "invitee",
		FirstName: "Inv",
		LastName:  "Itee",
		Role:      auth.RoleMember,
	}

	created, err := svc.Create(ctx, record)
	require.NoError(t, err)
	require.NotNil(t, created)
	require.Equal(t, 0, createCmd.calls)
	require.Equal(t, 1, inviteCmd.calls)
	require.Equal(t, "invite@example.com", inviteCmd.lastInput.Email)
	require.Equal(t, "invitee", inviteCmd.lastInput.Username)
}

func TestUserServiceCreateInviteModeMetadata(t *testing.T) {
	t.Helper()
	createCmd := &stubUserCreateCmd{}
	inviteCmd := &stubUserInviteCmd{}
	svc := NewUserService(UserServiceConfig{
		Guard: &stubGuardAdapter{
			result: crudguard.GuardResult{
				Actor: types.ActorRef{ID: uuid.New(), Type: types.ActorRoleSystemAdmin},
				Scope: types.ScopeFilter{TenantID: uuid.New()},
			},
		},
		Create: createCmd,
		Invite: inviteCmd,
	})
	ctx := newTestCrudContext(context.Background())
	record := &auth.User{
		Email: "create-invite@example.com",
		Metadata: map[string]any{
			"create_mode": "create_invite",
			"source":      "console",
		},
	}

	created, err := svc.Create(ctx, record)
	require.NoError(t, err)
	require.NotNil(t, created)
	require.Equal(t, 0, createCmd.calls)
	require.Equal(t, 1, inviteCmd.calls)
	_, ok := inviteCmd.lastInput.Metadata["create_mode"]
	require.False(t, ok)
	require.Equal(t, "console", inviteCmd.lastInput.Metadata["source"])
}

func TestUserServiceCreateInvalidMode(t *testing.T) {
	t.Helper()
	svc := NewUserService(UserServiceConfig{
		Guard: &stubGuardAdapter{
			result: crudguard.GuardResult{
				Actor: types.ActorRef{ID: uuid.New(), Type: types.ActorRoleSystemAdmin},
				Scope: types.ScopeFilter{},
			},
		},
		Create: &stubUserCreateCmd{},
		Invite: &stubUserInviteCmd{},
	})
	ctx := newTestCrudContext(context.Background())
	ctx.queries["mode"] = "nope"

	_, err := svc.Create(ctx, &auth.User{Email: "bad@example.com"})
	require.Error(t, err)
}

func TestUserServiceDeleteArchivesLifecycle(t *testing.T) {
	t.Helper()
	lifecycle := &stubUserLifecycleCmd{}
	guard := &stubGuardAdapter{
		result: crudguard.GuardResult{
			Actor: types.ActorRef{ID: uuid.New(), Type: types.ActorRoleSystemAdmin},
			Scope: types.ScopeFilter{OrgID: uuid.New()},
		},
	}
	svc := NewUserService(UserServiceConfig{
		Guard:     guard,
		Lifecycle: lifecycle,
	})
	userID := uuid.New()
	ctx := newTestCrudContext(context.Background())

	err := svc.Delete(ctx, &auth.User{ID: userID})
	require.NoError(t, err)
	require.Equal(t, 1, lifecycle.calls)
	require.Equal(t, userID, lifecycle.lastInput.UserID)
	require.Equal(t, types.LifecycleStateArchived, lifecycle.lastInput.Target)
}

func TestUserServiceDeleteBatchUsesBulkLifecycle(t *testing.T) {
	t.Helper()
	bulk := &stubBulkLifecycleCmd{}
	guard := &stubGuardAdapter{
		result: crudguard.GuardResult{
			Actor: types.ActorRef{ID: uuid.New(), Type: types.ActorRoleSystemAdmin},
			Scope: types.ScopeFilter{TenantID: uuid.New()},
		},
	}
	svc := NewUserService(UserServiceConfig{
		Guard:         guard,
		BulkLifecycle: bulk,
	})
	idOne := uuid.New()
	idTwo := uuid.New()
	ctx := newTestCrudContext(context.Background())

	err := svc.DeleteBatch(ctx, []*auth.User{{ID: idOne}, {ID: idTwo}})
	require.NoError(t, err)
	require.Equal(t, 1, bulk.calls)
	require.Equal(t, []uuid.UUID{idOne, idTwo}, bulk.lastInput.UserIDs)
	require.Equal(t, types.LifecycleStateArchived, bulk.lastInput.Target)
}

func TestUserServiceCreateBatch(t *testing.T) {
	t.Helper()
	createCmd := &stubUserCreateCmd{}
	guard := &stubGuardAdapter{
		result: crudguard.GuardResult{
			Actor: types.ActorRef{ID: uuid.New(), Type: types.ActorRoleSystemAdmin},
			Scope: types.ScopeFilter{},
		},
	}
	svc := NewUserService(UserServiceConfig{
		Guard:  guard,
		Create: createCmd,
		Invite: &stubUserInviteCmd{},
	})
	ctx := newTestCrudContext(context.Background())

	records := []*auth.User{
		{Email: "first@example.com"},
		{Email: "second@example.com"},
	}
	created, err := svc.CreateBatch(ctx, records)
	require.NoError(t, err)
	require.Len(t, created, 2)
	require.Equal(t, 2, createCmd.calls)
}

func TestUserServiceUpdateBatch(t *testing.T) {
	t.Helper()
	updateCmd := &stubUserUpdateCmd{}
	guard := &stubGuardAdapter{
		result: crudguard.GuardResult{
			Actor: types.ActorRef{ID: uuid.New(), Type: types.ActorRoleSystemAdmin},
			Scope: types.ScopeFilter{},
		},
	}
	svc := NewUserService(UserServiceConfig{
		Guard:  guard,
		Update: updateCmd,
	})
	ctx := newTestCrudContext(context.Background())

	idOne := uuid.New()
	idTwo := uuid.New()
	records := []*auth.User{
		{ID: idOne, Email: "first@example.com"},
		{ID: idTwo, Email: "second@example.com"},
	}
	updated, err := svc.UpdateBatch(ctx, records)
	require.NoError(t, err)
	require.Len(t, updated, 2)
	require.Equal(t, 2, updateCmd.calls)
}

func TestCollectUserIDs(t *testing.T) {
	t.Helper()
	id := uuid.MustParse("79c1f998-d0f3-4f1d-9818-250a0f1c8b1d")
	ids, err := collectUserIDs([]*auth.User{{ID: id}})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{id}, ids)
}

// --- test stubs ---

type stubUserCreateCmd struct {
	calls     int
	lastInput command.UserCreateInput
	err       error
}

func (s *stubUserCreateCmd) Execute(_ context.Context, input command.UserCreateInput) error {
	s.calls++
	s.lastInput = input
	if input.Result != nil && input.User != nil {
		user := *input.User
		if user.ID == uuid.Nil {
			user.ID = uuid.New()
		}
		*input.Result = user
	}
	return s.err
}

type stubUserUpdateCmd struct {
	calls     int
	lastInput command.UserUpdateInput
	err       error
}

func (s *stubUserUpdateCmd) Execute(_ context.Context, input command.UserUpdateInput) error {
	s.calls++
	s.lastInput = input
	if input.Result != nil && input.User != nil {
		user := *input.User
		*input.Result = user
	}
	return s.err
}

type stubUserInviteCmd struct {
	calls     int
	lastInput command.UserInviteInput
	err       error
}

func (s *stubUserInviteCmd) Execute(_ context.Context, input command.UserInviteInput) error {
	s.calls++
	s.lastInput = input
	user := &types.AuthUser{
		ID:        uuid.New(),
		Email:     input.Email,
		Username:  input.Username,
		FirstName: input.FirstName,
		LastName:  input.LastName,
		Role:      input.Role,
		Status:    types.LifecycleStatePending,
		Metadata:  input.Metadata,
	}
	if input.Result != nil {
		*input.Result = command.UserInviteResult{
			User:  user,
			Token: "invite-token",
		}
	}
	return s.err
}

type stubUserLifecycleCmd struct {
	calls     int
	lastInput command.UserLifecycleTransitionInput
	err       error
}

func (s *stubUserLifecycleCmd) Execute(_ context.Context, input command.UserLifecycleTransitionInput) error {
	s.calls++
	s.lastInput = input
	return s.err
}

type stubBulkLifecycleCmd struct {
	calls     int
	lastInput command.BulkUserTransitionInput
	err       error
}

func (s *stubBulkLifecycleCmd) Execute(_ context.Context, input command.BulkUserTransitionInput) error {
	s.calls++
	s.lastInput = input
	return s.err
}

var _ gocommand.Commander[command.UserCreateInput] = (*stubUserCreateCmd)(nil)
var _ gocommand.Commander[command.UserUpdateInput] = (*stubUserUpdateCmd)(nil)
var _ gocommand.Commander[command.UserInviteInput] = (*stubUserInviteCmd)(nil)
var _ gocommand.Commander[command.UserLifecycleTransitionInput] = (*stubUserLifecycleCmd)(nil)
var _ gocommand.Commander[command.BulkUserTransitionInput] = (*stubBulkLifecycleCmd)(nil)
