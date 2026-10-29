package crudguard

import (
	"context"
	auth "github.com/goliatone/go-auth"
	"github.com/goliatone/go-crud"
	goerrors "github.com/goliatone/go-errors"
	"github.com/google/uuid"
	"testing"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/stretchr/testify/require"
)

func TestStrictAdapterRequiresValidatedStrictGuard(t *testing.T) {
	for _, guard := range []scope.Guard{nil, scope.NopGuard(), scope.NewGuard(nil, nil), &stubGuard{}, &scope.StrictGuard{}} {
		adapter, err := NewAdapter(Config{Guard: guard, RequireScopeAuthorization: true, FallbackAction: types.PolicyActionUsersRead})
		require.Error(t, err)
		require.Nil(t, adapter)
	}
	guard, err := scope.NewStrictGuard(types.PassthroughScopeResolver{}, types.AllowAllAuthorizationPolicy{})
	require.NoError(t, err)
	_, err = NewAdapter(Config{Guard: guard, RequireScopeAuthorization: true, FallbackAction: types.PolicyActionUsersRead})
	require.NoError(t, err)
	_, err = NewAdapter(Config{Guard: scope.NopGuard(), FallbackAction: types.PolicyActionUsersRead})
	require.NoError(t, err, "legacy adapters remain compatible")
}

func TestStrictAdapterRejectsBlankMappingsAtStartup(t *testing.T) {
	guard, err := scope.NewStrictGuard(types.PassthroughScopeResolver{}, types.AllowAllAuthorizationPolicy{})
	require.NoError(t, err)
	cases := []struct {
		name     string
		mapping  map[crud.CrudOperation]types.PolicyAction
		fallback types.PolicyAction
		cause    error
	}{
		{"missing map and fallback", nil, "", nil},
		{"empty mapped action", map[crud.CrudOperation]types.PolicyAction{crud.OpRead: ""}, "", scope.ErrPolicyActionRequired},
		{"blank mapped action", map[crud.CrudOperation]types.PolicyAction{crud.OpRead: "\t \n"}, "", scope.ErrPolicyActionRequired},
		{"fallback cannot hide invalid mapping", map[crud.CrudOperation]types.PolicyAction{crud.OpRead: ""}, "custom:read", scope.ErrPolicyActionRequired},
		{"blank fallback", DefaultPolicyMap("custom:read", "custom:write"), " ", scope.ErrPolicyActionRequired},
		{"blank operation", map[crud.CrudOperation]types.PolicyAction{" ": "custom:write"}, "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter, err := NewAdapter(Config{Guard: guard, RequireScopeAuthorization: true, PolicyMap: tc.mapping, FallbackAction: tc.fallback})
			require.Nil(t, adapter)
			require.Error(t, err)
			if tc.cause != nil {
				require.ErrorIs(t, err, tc.cause)
			}
			var rich *goerrors.Error
			require.ErrorAs(t, err, &rich)
			require.Equal(t, goerrors.CategoryInternal, rich.Category)
			require.Equal(t, goerrors.CodeInternal, rich.Code)
			require.Equal(t, textCodeMissingPolicy, rich.TextCode)
		})
	}
}

func TestStrictAdapterMappedActionsReachPolicyAndUnmappedFail(t *testing.T) {
	var checks []types.PolicyCheck
	guard, err := scope.NewStrictGuard(types.PassthroughScopeResolver{}, types.AuthorizationPolicyFunc(func(_ context.Context, check types.PolicyCheck) error {
		checks = append(checks, check)
		return types.ErrUnauthorizedScope
	}))
	require.NoError(t, err)
	mapping := DefaultPolicyMap("custom:read", "custom:write")
	adapter, err := NewAdapter(Config{Guard: guard, RequireScopeAuthorization: true, PolicyMap: mapping})
	require.NoError(t, err)
	// Caller mutation after startup must not invalidate the checked mapping.
	mapping[crud.OpDelete] = ""
	actor := &auth.ActorContext{ActorID: uuid.NewString(), Role: "admin"}
	ctx := newStubCrudContext(auth.WithActorContext(context.Background(), actor))
	target := uuid.New()
	for _, op := range []crud.CrudOperation{crud.OpRead, crud.OpList, crud.OpCreate, crud.OpCreateBatch, crud.OpUpdate, crud.OpUpdateBatch, crud.OpDelete, crud.OpDeleteBatch} {
		_, enforceErr := adapter.Enforce(GuardInput{Context: ctx, Operation: op, TargetID: target})
		require.ErrorIs(t, enforceErr, types.ErrUnauthorizedScope)
		check := checks[len(checks)-1]
		require.Equal(t, target, check.TargetID)
		require.Equal(t, actor.ActorID, check.Actor.ID.String())
		expected := types.PolicyAction("custom:write")
		if op == crud.OpRead || op == crud.OpList {
			expected = "custom:read"
		}
		require.Equal(t, expected, check.Action)
	}
	for _, op := range []crud.CrudOperation{"", "\t ", "unmapped"} {
		_, enforceErr := adapter.Enforce(GuardInput{Context: ctx, Operation: op})
		var rich *goerrors.Error
		require.ErrorAs(t, enforceErr, &rich)
		require.Equal(t, textCodeMissingPolicy, rich.TextCode)
	}
	require.Len(t, checks, 8, "unmapped operations cannot reach authorization")
	fallback, err := NewAdapter(Config{Guard: guard, RequireScopeAuthorization: true, FallbackAction: "custom:fallback"})
	require.NoError(t, err)
	_, err = fallback.Enforce(GuardInput{Context: ctx, Operation: "custom"})
	require.ErrorIs(t, err, types.ErrUnauthorizedScope)
	require.Equal(t, types.PolicyAction("custom:fallback"), checks[8].Action)
	_, err = fallback.Enforce(GuardInput{Context: ctx, Operation: ""})
	require.Error(t, err)
	require.Len(t, checks, 9, "fallback does not authorize a blank operation")
}

func TestStrictAdapterConfigurationErrorsPreserveCause(t *testing.T) {
	_, err := NewAdapter(Config{Guard: scope.NopGuard(), RequireScopeAuthorization: true, FallbackAction: "custom:read"})
	require.ErrorIs(t, err, scope.ErrStrictGuardRequired)
	var rich *goerrors.Error
	require.ErrorAs(t, err, &rich)
	require.Equal(t, goerrors.CategoryInternal, rich.Category)
	require.Equal(t, goerrors.CodeInternal, rich.Code)
	require.Equal(t, textCodeScopeEnforcementFail, rich.TextCode)
}

func TestLegacyAdapterPreservesEmptyMappingFallback(t *testing.T) {
	adapter, err := NewAdapter(Config{
		Guard:          scope.NopGuard(),
		PolicyMap:      map[crud.CrudOperation]types.PolicyAction{crud.OpRead: ""},
		FallbackAction: "legacy:read",
	})
	require.NoError(t, err)
	actor := &auth.ActorContext{ActorID: uuid.NewString(), Role: "admin"}
	_, err = adapter.Enforce(GuardInput{Context: newStubCrudContext(auth.WithActorContext(context.Background(), actor)), Operation: crud.OpRead})
	require.NoError(t, err)
}
