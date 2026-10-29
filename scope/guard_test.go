package scope_test

import (
	"context"
	"errors"
	"testing"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestLegacyGuardPermissiveDependencies(t *testing.T) {
	ctx := context.Background()
	requested := types.ScopeFilter{}.WithLabel("group", uuid.New())
	for _, g := range []scope.Guard{scope.NewGuard(nil, nil), scope.Ensure(nil), scope.NopGuard()} {
		result, err := g.Enforce(ctx, types.ActorRef{}, requested, "resource:read", uuid.New())
		require.NoError(t, err)
		require.Equal(t, requested, result)
		require.ErrorIs(t, scope.ValidateStrict(g), scope.ErrStrictGuardRequired)
		result.Labels["group"] = uuid.New()
		require.NotEqual(t, result.Label("group"), requested.Label("group"))
	}
	require.ErrorIs(t, scope.ValidateStrict(nil), scope.ErrStrictGuardRequired)
}

func TestStrictGuardRejectsMissingDependencies(t *testing.T) {
	resolver := types.PassthroughScopeResolver{}
	policy := types.AllowAllAuthorizationPolicy{}
	var nilResolver types.ScopeResolverFunc
	var nilPolicy types.AuthorizationPolicyFunc
	var pointerResolver *types.PassthroughScopeResolver
	var pointerPolicy *types.AllowAllAuthorizationPolicy
	cases := []struct {
		name     string
		resolver types.ScopeResolver
		policy   types.AuthorizationPolicy
		want     error
	}{
		{"both", nil, nil, scope.ErrScopeResolverRequired},
		{"resolver", nil, policy, scope.ErrScopeResolverRequired},
		{"policy", resolver, nil, scope.ErrAuthorizationPolicyRequired},
		{"nil resolver function", nilResolver, policy, scope.ErrScopeResolverRequired},
		{"nil policy function", resolver, nilPolicy, scope.ErrAuthorizationPolicyRequired},
		{"nil resolver pointer", pointerResolver, policy, scope.ErrScopeResolverRequired},
		{"nil policy pointer", resolver, pointerPolicy, scope.ErrAuthorizationPolicyRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, err := scope.NewStrictGuard(tc.resolver, tc.policy)
			require.ErrorIs(t, err, tc.want)
			require.ErrorIs(t, scope.ValidateStrict(g), tc.want)
			// Ignoring a startup error must not produce a permissive guard.
			result, err := scope.Ensure(g).Enforce(context.Background(), types.ActorRef{}, types.ScopeFilter{}, "resource:write", uuid.New())
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, types.ScopeFilter{}, result)
		})
	}
	var nilGuard *scope.StrictGuard
	for _, g := range []scope.Guard{nilGuard, &scope.StrictGuard{}} {
		require.Error(t, scope.ValidateStrict(g))
		_, err := g.Enforce(context.Background(), types.ActorRef{}, types.ScopeFilter{}, "resource:write", uuid.New())
		require.Error(t, err)
	}
}

func TestStrictGuardResolvesThenAuthorizes(t *testing.T) {
	ctx := context.Background()
	actor := types.ActorRef{ID: uuid.New()}
	requested := types.ScopeFilter{}.WithLabel("group", uuid.New())
	resolved := types.ScopeFilter{TenantID: uuid.New()}.WithLabel("group", uuid.New())
	target := uuid.New()
	var calls []string
	resolver := types.ScopeResolverFunc(func(gotCtx context.Context, gotActor types.ActorRef, got types.ScopeFilter) (types.ScopeFilter, error) {
		require.Equal(t, ctx, gotCtx)
		require.Equal(t, actor, gotActor)
		require.Equal(t, requested, got)
		calls = append(calls, "resolve")
		return resolved, nil
	})
	policy := types.AuthorizationPolicyFunc(func(_ context.Context, check types.PolicyCheck) error {
		calls = append(calls, "authorize")
		require.Equal(t, actor, check.Actor)
		require.Equal(t, target, check.TargetID)
		require.Equal(t, types.PolicyAction("resource:read"), check.Action)
		require.Equal(t, resolved, check.Scope)
		return types.ErrUnauthorizedScope
	})
	g, err := scope.NewStrictGuard(resolver, policy)
	require.NoError(t, err)
	require.NoError(t, scope.ValidateStrict(g))
	result, err := g.Enforce(ctx, actor, requested, "resource:read", target)
	require.ErrorIs(t, err, types.ErrUnauthorizedScope)
	require.Equal(t, types.ScopeFilter{}, result)
	require.Equal(t, []string{"resolve", "authorize"}, calls)

	failure := errors.New("resolution failed")
	g, err = scope.NewStrictGuard(types.ScopeResolverFunc(func(context.Context, types.ActorRef, types.ScopeFilter) (types.ScopeFilter, error) {
		return types.ScopeFilter{}, failure
	}), policy)
	require.NoError(t, err)
	_, err = g.Enforce(ctx, actor, requested, "resource:read", target)
	require.ErrorIs(t, err, failure)
	require.Equal(t, []string{"resolve", "authorize"}, calls, "resolver error stops policy")
}

func TestStrictGuardRejectsBlankActionBeforeDependenciesRun(t *testing.T) {
	resolverCalls, policyCalls := 0, 0
	resolver := types.ScopeResolverFunc(func(_ context.Context, _ types.ActorRef, requested types.ScopeFilter) (types.ScopeFilter, error) {
		resolverCalls++
		return requested, nil
	})
	policy := types.AuthorizationPolicyFunc(func(context.Context, types.PolicyCheck) error {
		policyCalls++
		return types.ErrUnauthorizedScope
	})
	strict, err := scope.NewStrictGuard(resolver, policy)
	require.NoError(t, err)
	requested := types.ScopeFilter{}.WithLabel("group", uuid.New())
	for _, action := range []types.PolicyAction{"", " ", "\t\r\n"} {
		result, enforceErr := strict.Enforce(context.Background(), types.ActorRef{ID: uuid.New()}, requested, action, uuid.New())
		require.ErrorIs(t, enforceErr, scope.ErrPolicyActionRequired)
		require.Equal(t, types.ScopeFilter{}, result)
	}
	require.Zero(t, resolverCalls)
	require.Zero(t, policyCalls)
	_, err = strict.Enforce(context.Background(), types.ActorRef{ID: uuid.New()}, requested, "custom:write", uuid.New())
	require.ErrorIs(t, err, types.ErrUnauthorizedScope)
	require.Equal(t, 1, resolverCalls)
	require.Equal(t, 1, policyCalls, "nonblank actions cannot skip policy evaluation")
	_, err = scope.NewGuard(resolver, policy).Enforce(context.Background(), types.ActorRef{}, requested, "", uuid.Nil)
	require.NoError(t, err, "legacy empty actions preserve compatibility")
	require.Equal(t, 2, resolverCalls)
	require.Equal(t, 1, policyCalls)
}

func TestStrictGuardAllowsAndReturnsDetachedResolvedScope(t *testing.T) {
	resolved := types.ScopeFilter{}.WithLabel("group", uuid.New())
	strict, err := scope.NewStrictGuard(types.ScopeResolverFunc(func(context.Context, types.ActorRef, types.ScopeFilter) (types.ScopeFilter, error) {
		return resolved, nil
	}), types.AllowAllAuthorizationPolicy{})
	require.NoError(t, err)
	result, err := strict.Enforce(context.Background(), types.ActorRef{ID: uuid.New()}, types.ScopeFilter{}, "custom:read", uuid.New())
	require.NoError(t, err)
	require.Equal(t, resolved, result)
	result.Labels["group"] = uuid.New()
	require.NotEqual(t, resolved.Label("group"), result.Label("group"))
}
