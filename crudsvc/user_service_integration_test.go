package crudsvc

import (
	"context"
	"errors"
	"testing"
	"time"

	auth "github.com/goliatone/go-auth"
	"github.com/goliatone/go-users/command"
	"github.com/goliatone/go-users/crudguard"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestUserServiceIntegrationCreateModesAndBulkDelete(t *testing.T) {
	t.Helper()
	repo := newIntegrationAuthRepo()
	actor := types.ActorRef{ID: uuid.New(), Type: types.ActorRoleSystemAdmin}
	guard := &stubGuardAdapter{
		result: crudguard.GuardResult{
			Actor: actor,
			Scope: types.ScopeFilter{TenantID: uuid.New()},
		},
	}

	lifecycle := command.NewUserLifecycleTransitionCommand(command.LifecycleCommandConfig{
		Repository: repo,
		Policy:     types.DefaultTransitionPolicy(),
		ScopeGuard: scope.NopGuard(),
	})
	bulkLifecycle := command.NewBulkUserTransitionCommand(lifecycle)
	createCmd := command.NewUserCreateCommand(command.UserCreateCommandConfig{
		Repository: repo,
		ScopeGuard: scope.NopGuard(),
	})
	updateCmd := command.NewUserUpdateCommand(command.UserUpdateCommandConfig{
		Repository: repo,
		ScopeGuard: scope.NopGuard(),
	})
	tokenRepo := newMemoryTokenRepo()
	secureLinks := &stubSecureLinkManager{
		token:      "invite-token",
		expiration: time.Hour,
	}
	inviteCmd := command.NewUserInviteCommand(command.InviteCommandConfig{
		Repository:      repo,
		TokenRepository: tokenRepo,
		SecureLinks:     secureLinks,
		Clock:           fixedClock{t: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)},
		IDGen:           fixedIDGenerator{id: uuid.MustParse("2b3f0b8e-2c2d-4ce0-8b5e-f72b48f0c11d")},
		ScopeGuard:      scope.NopGuard(),
	})

	svc := NewUserService(UserServiceConfig{
		Guard:         guard,
		AuthRepo:      repo,
		Create:        createCmd,
		Update:        updateCmd,
		Invite:        inviteCmd,
		Lifecycle:     lifecycle,
		BulkLifecycle: bulkLifecycle,
	})

	ctx := newTestCrudContext(context.Background())
	direct, err := svc.Create(ctx, &auth.User{Email: "direct@example.com"})
	require.NoError(t, err)
	require.Equal(t, auth.UserStatusActive, direct.Status)

	ctxInvite := newTestCrudContext(context.Background())
	ctxInvite.queries["mode"] = "invite"
	inviteUser, err := svc.Create(ctxInvite, &auth.User{Email: "invite@example.com"})
	require.NoError(t, err)
	require.Equal(t, auth.UserStatusPending, inviteUser.Status)
	require.Contains(t, inviteUser.Metadata, "invite")

	ctxCreateInvite := newTestCrudContext(context.Background())
	createdInvite, err := svc.Create(ctxCreateInvite, &auth.User{
		Email: "create-invite@example.com",
		Metadata: map[string]any{
			"create_mode": "create_invite",
			"source":      "console",
		},
	})
	require.NoError(t, err)
	require.Equal(t, auth.UserStatusPending, createdInvite.Status)
	require.Contains(t, createdInvite.Metadata, "invite")
	require.Equal(t, "console", createdInvite.Metadata["source"])

	err = svc.Delete(ctx, &auth.User{ID: direct.ID})
	require.NoError(t, err)
	require.Equal(t, types.LifecycleStateArchived, repo.users[direct.ID].Status)

	extraOne, _ := svc.Create(ctx, &auth.User{Email: "bulk-one@example.com"})
	extraTwo, _ := svc.Create(ctx, &auth.User{Email: "bulk-two@example.com"})
	err = svc.DeleteBatch(ctx, []*auth.User{{ID: extraOne.ID}, {ID: extraTwo.ID}})
	require.NoError(t, err)
	require.Equal(t, types.LifecycleStateArchived, repo.users[extraOne.ID].Status)
	require.Equal(t, types.LifecycleStateArchived, repo.users[extraTwo.ID].Status)
}

type integrationAuthRepo struct {
	users map[uuid.UUID]*types.AuthUser
}

func newIntegrationAuthRepo() *integrationAuthRepo {
	return &integrationAuthRepo{
		users: make(map[uuid.UUID]*types.AuthUser),
	}
}

func (r *integrationAuthRepo) GetByID(_ context.Context, id uuid.UUID) (*types.AuthUser, error) {
	if user, ok := r.users[id]; ok {
		copy := *user
		return &copy, nil
	}
	return nil, nil
}

func (r *integrationAuthRepo) GetByIdentifier(context.Context, string) (*types.AuthUser, error) {
	return nil, nil
}

func (r *integrationAuthRepo) Create(_ context.Context, input *types.AuthUser) (*types.AuthUser, error) {
	user := *input
	user.Raw = nil
	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}
	r.users[user.ID] = &user
	return &user, nil
}

func (r *integrationAuthRepo) Update(_ context.Context, input *types.AuthUser) (*types.AuthUser, error) {
	user := *input
	user.Raw = nil
	r.users[user.ID] = &user
	return &user, nil
}

func (r *integrationAuthRepo) UpdateStatus(_ context.Context, _ types.ActorRef, id uuid.UUID, next types.LifecycleState, _ ...types.TransitionOption) (*types.AuthUser, error) {
	user, ok := r.users[id]
	if !ok {
		return nil, nil
	}
	user.Status = next
	copy := *user
	return &copy, nil
}

func (r *integrationAuthRepo) AllowedTransitions(context.Context, uuid.UUID) ([]types.LifecycleTransition, error) {
	return nil, nil
}

func (r *integrationAuthRepo) ResetPassword(context.Context, uuid.UUID, string) error {
	return nil
}

type stubSecureLinkManager struct {
	token      string
	expiration time.Duration
}

func (s *stubSecureLinkManager) Generate(string, ...types.SecureLinkPayload) (string, error) {
	if s.token == "" {
		return "token", nil
	}
	return s.token, nil
}

func (s *stubSecureLinkManager) Validate(string) (map[string]any, error) {
	return map[string]any{}, nil
}

func (s *stubSecureLinkManager) GetAndValidate(fn func(string) string) (types.SecureLinkPayload, error) {
	if fn != nil {
		_ = fn("")
	}
	return types.SecureLinkPayload{}, nil
}

func (s *stubSecureLinkManager) GetExpiration() time.Duration {
	return s.expiration
}

type memoryTokenRepo struct {
	tokens map[string]*types.UserToken
}

func newMemoryTokenRepo() *memoryTokenRepo {
	return &memoryTokenRepo{tokens: map[string]*types.UserToken{}}
}

func (m *memoryTokenRepo) CreateToken(_ context.Context, token types.UserToken) (*types.UserToken, error) {
	copy := token
	if copy.ID == uuid.Nil {
		copy.ID = uuid.New()
	}
	m.tokens[copy.JTI] = &copy
	return &copy, nil
}

func (m *memoryTokenRepo) GetTokenByJTI(_ context.Context, _ types.UserTokenType, jti string) (*types.UserToken, error) {
	if token, ok := m.tokens[jti]; ok {
		return token, nil
	}
	return nil, errors.New("not found")
}

func (m *memoryTokenRepo) UpdateTokenStatus(_ context.Context, _ types.UserTokenType, jti string, status types.UserTokenStatus, usedAt time.Time) error {
	token, ok := m.tokens[jti]
	if !ok {
		return errors.New("not found")
	}
	token.Status = status
	if !usedAt.IsZero() {
		token.UsedAt = usedAt
	}
	return nil
}

type fixedIDGenerator struct {
	id uuid.UUID
}

func (f fixedIDGenerator) UUID() uuid.UUID {
	return f.id
}

type fixedClock struct {
	t time.Time
}

func (f fixedClock) Now() time.Time {
	return f.t
}
