package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/goliatone/go-users/command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// The host owns these authoritative Group/membership/grant stores. No session
// snapshot or library-owned Group state is substituted for these reads.
type liveAuthorizationHost struct {
	*ownershipHost
	groups, members map[uuid.UUID]bool
	grants          map[uuid.UUID]map[types.PolicyAction]bool
	unavailable     error
}

func newLiveAuthorizationHost() *liveAuthorizationHost {
	h := &liveAuthorizationHost{ownershipHost: newOwnershipHost(), groups: map[uuid.UUID]bool{}, members: map[uuid.UUID]bool{}, grants: map[uuid.UUID]map[types.PolicyAction]bool{}}
	h.users[h.actor.ID] = &mtUser{user: &types.AuthUser{ID: h.actor.ID, Status: types.LifecycleStateActive}}
	h.groups[h.groupA], h.members[h.groupA] = true, true
	h.grants[h.groupA] = map[types.PolicyAction]bool{types.PolicyActionUsersWrite: true, types.PolicyActionProfilesRead: true}
	return h
}

func (h *liveAuthorizationHost) authorize(ctx context.Context, check types.PolicyCheck) error {
	if h.unavailable != nil {
		return h.unavailable
	}
	principal, err := h.GetByID(ctx, check.Actor.ID)
	if err != nil || principal == nil || principal.Status != types.LifecycleStateActive {
		return types.ErrUnauthorizedScope
	}
	group := check.Scope.Label("group")
	if !h.groups[group] || !h.members[group] || !h.grants[group][check.Action] {
		return types.ErrUnauthorizedScope
	}
	return h.policy(check, true)
}

func (h *liveAuthorizationHost) runtime(t *testing.T) *service.Service {
	t.Helper()
	cfg := strictTestConfig()
	cfg.AuthRepository, cfg.InventoryRepository = h, h
	cfg.AuthorizationPolicy = types.AuthorizationPolicyFunc(h.authorize)
	svc, err := service.NewStrict(cfg)
	require.NoError(t, err)
	return svc
}

func TestHostCurrentStatePolicyDeniesDisabledUnknownAndUnavailableState(t *testing.T) {
	outage := errors.New("authoritative membership store unavailable")
	cases := []struct {
		name   string
		change func(*liveAuthorizationHost, *command.UserUpdateInput)
		want   error
	}{
		{"disabled account", func(h *liveAuthorizationHost, _ *command.UserUpdateInput) {
			h.users[h.actor.ID].user.Status = types.LifecycleStateDisabled
		}, types.ErrUnauthorizedScope},
		{"disabled Group", func(h *liveAuthorizationHost, _ *command.UserUpdateInput) { h.groups[h.groupA] = false }, types.ErrUnauthorizedScope},
		{"revoked membership", func(h *liveAuthorizationHost, _ *command.UserUpdateInput) { h.members[h.groupA] = false }, types.ErrUnauthorizedScope},
		{"revoked grant", func(h *liveAuthorizationHost, _ *command.UserUpdateInput) { delete(h.grants, h.groupA) }, types.ErrUnauthorizedScope},
		{"unknown principal", func(h *liveAuthorizationHost, in *command.UserUpdateInput) { in.Actor.ID = uuid.New() }, types.ErrUnauthorizedScope},
		{"unknown Group", func(_ *liveAuthorizationHost, in *command.UserUpdateInput) {
			in.Scope = in.Scope.WithLabel("group", uuid.New())
		}, types.ErrUnauthorizedScope},
		{"store outage", func(h *liveAuthorizationHost, _ *command.UserUpdateInput) { h.unavailable = outage }, outage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newLiveAuthorizationHost()
			svc := h.runtime(t)
			input := command.UserUpdateInput{User: &types.AuthUser{ID: h.userA, Email: "allowed@example.com"}, Actor: h.actor, Scope: types.ScopeFilter{}.WithLabel("group", h.groupA)}
			ctx := context.Background()
			require.NoError(t, svc.Commands().UserUpdate.Execute(ctx, input))
			// Keep the same caller/session; only authoritative state changes.
			tc.change(h, &input)
			input.User = &types.AuthUser{ID: h.userA, Email: "blocked@example.com"}
			require.ErrorIs(t, svc.Commands().UserUpdate.Execute(ctx, input), tc.want)
			require.Equal(t, "allowed@example.com", h.users[h.userA].user.Email)
		})
	}
}

func TestHostMembershipRejoinDoesNotRestoreRevokedGrant(t *testing.T) {
	h := newLiveAuthorizationHost()
	svc := h.runtime(t)
	ctx := context.Background()
	input := command.UserUpdateInput{User: &types.AuthUser{ID: h.userA, Email: "allowed@example.com"}, Actor: h.actor, Scope: types.ScopeFilter{}.WithLabel("group", h.groupA)}
	require.NoError(t, svc.Commands().UserUpdate.Execute(ctx, input))
	h.members[h.groupA] = false
	delete(h.grants, h.groupA)
	require.ErrorIs(t, svc.Commands().UserUpdate.Execute(ctx, input), types.ErrUnauthorizedScope)
	h.members[h.groupA] = true
	input.User = &types.AuthUser{ID: h.userA, Email: "should-not-return@example.com"}
	require.ErrorIs(t, svc.Commands().UserUpdate.Execute(ctx, input), types.ErrUnauthorizedScope)
	require.Equal(t, "allowed@example.com", h.users[h.userA].user.Email)
	h.grants[h.groupA] = map[types.PolicyAction]bool{types.PolicyActionUsersWrite: true}
	require.NoError(t, svc.Commands().UserUpdate.Execute(ctx, input), "an explicit new grant restores permission")
}

func TestStrictWiringDoesNotSupplyHostCurrentMembershipState(t *testing.T) {
	h := newLiveAuthorizationHost()
	// This intentionally omits live-state checks, reproducing the integration hazard.
	svc := h.ownershipHost.runtime(t, true)
	h.users[h.actor.ID].user.Status = types.LifecycleStateDisabled
	h.members[h.groupA] = false
	err := svc.Commands().UserUpdate.Execute(context.Background(), command.UserUpdateInput{User: &types.AuthUser{ID: h.userA, Email: "stale@example.com"}, Actor: h.actor, Scope: types.ScopeFilter{}.WithLabel("group", h.groupA)})
	require.NoError(t, err)
	require.Equal(t, "stale@example.com", h.users[h.userA].user.Email, "only the host can supply current Group/account predicates")
}
