package service_test

import (
	"context"
	"slices"
	"testing"
	"time"

	auth "github.com/goliatone/go-auth"
	goauth "github.com/goliatone/go-users/adapter/goauth"
	"github.com/goliatone/go-users/command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/registry"
	"github.com/goliatone/go-users/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type scopedPolicyCheckKey struct{}

type scopedPolicyFixture struct {
	host       *liveAuthorizationHost
	registry   *registry.RoleRegistry
	service    *service.Service
	versions   *auth.InMemoryAuthorizationVersionStore
	exceptions map[uuid.UUID]bool
	context    context.Context
	claims     *auth.JWTClaims
	reader     uuid.UUID
}

func newScopedPolicyFixture(t *testing.T) *scopedPolicyFixture {
	t.Helper()
	f := &scopedPolicyFixture{host: newLiveAuthorizationHost(), registry: grantRegistry(t), versions: auth.NewInMemoryAuthorizationVersionStore(nil), exceptions: map[uuid.UUID]bool{}}
	h := f.host
	h.groups[h.groupB], h.members[h.groupB] = true, true
	principal, err := auth.NewAuthenticatedPrincipal(auth.AuthenticatedPrincipalInput{ApplicationSubject: h.actor.ID.String(), Provider: "host", ProviderSubject: "linked", LocalSessionID: "existing-session"})
	require.NoError(t, err)
	_, err = f.versions.AdvanceAuthorizationVersion(context.Background(), principal.ApplicationSubject(), "", "initial", func(context.Context) error { return nil })
	require.NoError(t, err)
	editor, err := f.registry.CreateRole(context.Background(), types.RoleMutation{Name: "editor", Permissions: []string{string(types.PolicyActionUsersRead), string(types.PolicyActionUsersWrite)}})
	require.NoError(t, err)
	reader, err := f.registry.CreateRole(context.Background(), types.RoleMutation{Name: "reader", Permissions: []string{string(types.PolicyActionUsersRead)}})
	require.NoError(t, err)
	f.reader = reader.ID
	require.NoError(t, f.registry.AssignRole(context.Background(), h.actor.ID, editor.ID, types.ScopeFilter{}.WithLabel("group", h.groupA), h.actor.ID))
	require.NoError(t, f.registry.AssignRole(context.Background(), h.actor.ID, reader.ID, types.ScopeFilter{}.WithLabel("group", h.groupB), h.actor.ID))
	resolver, err := goauth.NewScopedPermissionsResolver(goauth.ScopedPermissionsResolverConfig{
		RequestResolver: func(ctx context.Context) (goauth.ScopedPermissionsRequest, error) {
			check, ok := ctx.Value(scopedPolicyCheckKey{}).(types.PolicyCheck)
			if !ok || check.Actor.ID.String() != principal.ApplicationSubject() {
				return goauth.ScopedPermissionsRequest{}, types.ErrUnauthorizedScope
			}
			account, err := h.GetByID(ctx, check.Actor.ID)
			if err != nil || account == nil || account.Status != types.LifecycleStateActive {
				return goauth.ScopedPermissionsRequest{}, types.ErrUnauthorizedScope
			}
			group := check.Scope.Label("group")
			if !h.groups[group] || (!h.members[group] && !f.exceptions[group]) {
				return goauth.ScopedPermissionsRequest{}, types.ErrUnauthorizedScope
			}
			if check.TargetID != uuid.Nil && h.owners[check.TargetID] != group {
				return goauth.ScopedPermissionsRequest{}, types.ErrUnauthorizedScope
			}
			version, err := f.versions.CurrentAuthorizationVersion(ctx, principal.ApplicationSubject(), "")
			if err != nil {
				return goauth.ScopedPermissionsRequest{}, err
			}
			return goauth.ScopedPermissionsRequest{Current: auth.CurrentPermissionsRequest{Principal: principal, Version: version.Version, Role: "member"}, Scope: check.Scope}, nil
		},
		Cache: auth.CachedPermissionsResolverConfig{TTL: time.Hour, Resolver: func(ctx context.Context) ([]string, error) {
			request, ok := goauth.ScopedPermissionsRequestFromContext(ctx)
			if !ok {
				return nil, types.ErrUnauthorizedScope
			}
			assignments, err := f.registry.ListAssignments(ctx, types.RoleAssignmentFilter{UserID: h.actor.ID, Scope: request.Scope})
			if err != nil {
				return nil, err
			}
			var permissions []string
			for _, assignment := range assignments {
				role, err := f.registry.GetRole(ctx, assignment.RoleID, types.ScopeFilter{TenantID: request.Scope.TenantID, OrgID: request.Scope.OrgID})
				if err != nil {
					return nil, err
				}
				permissions = append(permissions, role.Permissions...)
			}
			// This exception is selected by authoritative host policy; claim
			// roles never supply it. Its changes advance the same version.
			if f.exceptions[request.Scope.Label("group")] {
				permissions = append(permissions, string(types.PolicyActionUsersRead), string(types.PolicyActionUsersWrite))
			}
			return permissions, nil
		}},
	})
	require.NoError(t, err)
	cfg := strictTestConfig()
	cfg.AuthRepository, cfg.InventoryRepository = h, h
	cfg.RoleRegistry = f.registry
	cfg.AuthorizationPolicy = types.AuthorizationPolicyFunc(func(ctx context.Context, check types.PolicyCheck) error {
		permissions, err := resolver.ResolvePermissions(context.WithValue(ctx, scopedPolicyCheckKey{}, check))
		if err != nil {
			return err
		}
		if !slices.Contains(permissions, string(check.Action)) {
			return types.ErrUnauthorizedScope
		}
		return nil
	})
	f.service, err = service.NewStrict(cfg)
	require.NoError(t, err)
	f.claims = &auth.JWTClaims{UID: h.actor.ID.String(), UserRole: "admin", Resources: map[string]string{}}
	f.context = auth.WithResolvedPermissionsCache(auth.WithClaimsContext(context.Background(), f.claims))
	return f
}

func TestStrictScopedPermissionsDoNotInheritGlobalClaimFallback(t *testing.T) {
	f := newScopedPolicyFixture(t)
	h := f.host
	require.True(t, f.claims.CanEdit("group:"+h.groupB.String()), "the legacy helper deliberately falls back to global admin")
	update := func(user, group uuid.UUID) error {
		return f.service.Commands().UserUpdate.Execute(f.context, command.UserUpdateInput{User: &types.AuthUser{ID: user, Email: "updated@example.com"}, Actor: h.actor, Scope: types.ScopeFilter{}.WithLabel("group", group)})
	}
	require.NoError(t, update(h.userA, h.groupA))
	require.ErrorIs(t, update(h.userB, h.groupB), types.ErrUnauthorizedScope, "B reader cannot borrow A editor permissions or global claim privileges")
	require.Empty(t, h.users[h.userB].user.Email)
	page, err := f.service.Queries().UserInventory.Query(f.context, types.UserInventoryFilter{Actor: h.actor, Scope: types.ScopeFilter{}.WithLabel("group", h.groupB)})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	_, err = f.versions.AdvanceAuthorizationVersion(f.context, h.actor.ID.String(), "", "revoke-B", func(ctx context.Context) error {
		return f.registry.UnassignRole(ctx, h.actor.ID, f.reader, types.ScopeFilter{}.WithLabel("group", h.groupB), h.actor.ID)
	})
	require.NoError(t, err)
	_, err = f.service.Queries().UserInventory.Query(f.context, types.UserInventoryFilter{Actor: h.actor, Scope: types.ScopeFilter{}.WithLabel("group", h.groupB)})
	require.ErrorIs(t, err, types.ErrUnauthorizedScope, "version advancement defeats the warmed request-local and cross-request reader entry")
	require.NoError(t, update(h.userA, h.groupA), "B revocation preserves A's independent grant")
	require.True(t, f.claims.CanEdit("group:"+h.groupB.String()), "strict integration does not modify legacy claims")
}

func TestStrictScopedAdministrativeExceptionRequiresExplicitHostPolicy(t *testing.T) {
	f := newScopedPolicyFixture(t)
	h := f.host
	h.members[h.groupB] = false
	update := command.UserUpdateInput{User: &types.AuthUser{ID: h.userB, Email: "explicit-admin@example.com"}, Actor: h.actor, Scope: types.ScopeFilter{}.WithLabel("group", h.groupB)}
	require.True(t, f.claims.CanEdit("unassigned-group"))
	require.ErrorIs(t, f.service.Commands().UserUpdate.Execute(f.context, update), types.ErrUnauthorizedScope)
	_, err := f.versions.AdvanceAuthorizationVersion(f.context, h.actor.ID.String(), "", "approve-admin", func(context.Context) error { f.exceptions[h.groupB] = true; return nil })
	require.NoError(t, err)
	require.NoError(t, f.service.Commands().UserUpdate.Execute(f.context, update), "a separately selected host administrative exception can cross membership boundaries")
	require.Equal(t, "explicit-admin@example.com", h.users[h.userB].user.Email)
	_, err = f.versions.AdvanceAuthorizationVersion(f.context, h.actor.ID.String(), "", "remove-admin", func(context.Context) error { delete(f.exceptions, h.groupB); return nil })
	require.NoError(t, err)
	require.ErrorIs(t, f.service.Commands().UserUpdate.Execute(f.context, update), types.ErrUnauthorizedScope)
}
