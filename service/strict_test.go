package service_test

import (
	"context"
	"testing"

	users "github.com/goliatone/go-users"
	"github.com/goliatone/go-users/command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/goliatone/go-users/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func strictTestConfig() service.Config {
	auth := newMTAuthRepo()
	activity := newMTActivityStore()
	return service.Config{
		AuthRepository: auth, InventoryRepository: auth,
		RoleRegistry: newMTRoleRegistry(), ActivitySink: activity, ActivityRepository: activity,
		ProfileRepository: newMTProfileRepo(), PreferenceRepository: newMTPreferenceRepo(),
		ScopeResolver: types.PassthroughScopeResolver{}, AuthorizationPolicy: types.AllowAllAuthorizationPolicy{},
	}
}

func TestStrictServiceConfigurationAndReadiness(t *testing.T) {
	ctx := context.Background()
	cfg := strictTestConfig()
	cfg.RequireScopeAuthorization = true
	svc := service.New(cfg)
	require.True(t, svc.Ready())
	require.NoError(t, svc.HealthCheck(ctx))
	require.NoError(t, scope.ValidateStrict(svc.ScopeGuard()))

	for _, missing := range []string{"resolver", "policy"} {
		t.Run(missing, func(t *testing.T) {
			invalid := cfg
			want := scope.ErrScopeResolverRequired
			if missing == "resolver" {
				invalid.ScopeResolver = nil
			} else {
				invalid.AuthorizationPolicy = nil
				want = scope.ErrAuthorizationPolicyRequired
			}
			failed, err := users.NewStrict(invalid)
			require.Nil(t, failed)
			require.ErrorIs(t, err, want)
			misconfigured := service.New(invalid)
			require.False(t, misconfigured.Ready())
			require.ErrorIs(t, misconfigured.HealthCheck(ctx), want)
			// Execute despite failed readiness: the registry must remain untouched.
			actor := types.ActorRef{ID: uuid.New()}
			err = misconfigured.Commands().CreateRole.Execute(ctx, command.CreateRoleInput{Name: "blocked", Actor: actor})
			require.ErrorIs(t, err, want)
			page, err := invalid.RoleRegistry.ListRoles(ctx, types.RoleFilter{})
			require.NoError(t, err)
			require.Empty(t, page.Roles)
		})
	}
}

func TestStrictServiceConstructorAndLegacyCompatibility(t *testing.T) {
	cfg := strictTestConfig()
	svc, err := users.NewStrict(cfg)
	require.NoError(t, err)
	require.True(t, svc.Ready())
	require.NoError(t, scope.ValidateStrict(svc.ScopeGuard()))
	cfg.ScopeResolver, cfg.AuthorizationPolicy = nil, nil
	legacy := users.New(cfg)
	require.True(t, legacy.Ready())
	require.NoError(t, legacy.HealthCheck(context.Background()))
	require.ErrorIs(t, scope.ValidateStrict(legacy.ScopeGuard()), scope.ErrStrictGuardRequired)
	require.NoError(t, legacy.Commands().CreateRole.Execute(context.Background(), command.CreateRoleInput{
		Name: "legacy", Actor: types.ActorRef{ID: uuid.New()},
	}))
	var nilSvc *service.Service
	require.False(t, nilSvc.Ready())
	require.ErrorIs(t, nilSvc.HealthCheck(context.Background()), types.ErrServiceNotReady)
}

func TestStrictServiceAuthorizesCommandsAndQueriesBeforeRegistryAccess(t *testing.T) {
	ctx := context.Background()
	cfg := strictTestConfig()
	catalog := types.ScopeFilter{TenantID: uuid.New()}
	actor := types.ActorRef{ID: uuid.New()}
	allow := false
	var actions []types.PolicyAction
	cfg.ScopeResolver = types.ScopeResolverFunc(func(context.Context, types.ActorRef, types.ScopeFilter) (types.ScopeFilter, error) {
		return catalog, nil
	})
	cfg.AuthorizationPolicy = types.AuthorizationPolicyFunc(func(_ context.Context, check types.PolicyCheck) error {
		actions = append(actions, check.Action)
		require.Equal(t, catalog, check.Scope)
		require.Equal(t, actor, check.Actor)
		if !allow {
			return types.ErrUnauthorizedScope
		}
		return nil
	})
	svc, err := users.NewStrict(cfg)
	require.NoError(t, err)
	require.True(t, svc.Ready())
	input := command.CreateRoleInput{Name: "strict", Actor: actor}
	require.ErrorIs(t, svc.Commands().CreateRole.Execute(ctx, input), types.ErrUnauthorizedScope)
	_, err = svc.Queries().RoleList.Query(ctx, types.RoleFilter{Actor: actor})
	require.ErrorIs(t, err, types.ErrUnauthorizedScope)
	page, err := cfg.RoleRegistry.ListRoles(ctx, types.RoleFilter{Scope: catalog})
	require.NoError(t, err)
	require.Empty(t, page.Roles, "denied command does not mutate registry")
	_, err = svc.ScopeGuard().Enforce(ctx, actor, catalog, "", uuid.New())
	require.ErrorIs(t, err, scope.ErrPolicyActionRequired)
	require.Len(t, actions, 2, "blank action fails before policy")
	allow = true
	require.NoError(t, svc.Commands().CreateRole.Execute(ctx, input))
	page, err = svc.Queries().RoleList.Query(ctx, types.RoleFilter{Actor: actor})
	require.NoError(t, err)
	require.Len(t, page.Roles, 1)
	require.Equal(t, catalog, page.Roles[0].Scope)
	require.Equal(t, []types.PolicyAction{types.PolicyActionRolesWrite, types.PolicyActionRolesRead, types.PolicyActionRolesWrite, types.PolicyActionRolesRead}, actions)
}
