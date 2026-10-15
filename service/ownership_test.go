package service_test

import (
	"context"
	"sort"
	"testing"

	"github.com/goliatone/go-users/command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/query"
	"github.com/goliatone/go-users/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// This fixture implements application-owned Group predicates. It demonstrates
// composition of public APIs, not built-in Group storage or SQL isolation.
type ownershipHost struct {
	*mtAuthRepo
	owners                               map[uuid.UUID]uuid.UUID
	actor                                types.ActorRef
	groupA, groupB, userA, userA2, userB uuid.UUID
}

func newOwnershipHost() *ownershipHost {
	h := &ownershipHost{mtAuthRepo: newMTAuthRepo(), owners: map[uuid.UUID]uuid.UUID{}, actor: types.ActorRef{ID: uuid.New()}, groupA: uuid.New(), groupB: uuid.New()}
	h.userA, h.userA2, h.userB = h.seedUser(uuid.Nil), h.seedUser(uuid.Nil), h.seedUser(uuid.Nil)
	h.owners[h.userA], h.owners[h.userA2], h.owners[h.userB] = h.groupA, h.groupA, h.groupB
	return h
}

func (h *ownershipHost) policy(check types.PolicyCheck, ownership bool) error {
	if check.Actor.ID != h.actor.ID || check.Scope.Label("group") != h.groupA {
		return types.ErrUnauthorizedScope
	}
	if ownership && check.TargetID != uuid.Nil && h.owners[check.TargetID] != h.groupA {
		return types.ErrUnauthorizedScope
	}
	// Generic creation cannot attach trusted Group ownership; require a host
	// service rather than trusting user-supplied metadata.
	if ownership && check.Action == types.PolicyActionUsersWrite && check.TargetID == uuid.Nil {
		return types.ErrUnauthorizedScope
	}
	return nil
}

func (h *ownershipHost) runtime(t *testing.T, ownership bool) *service.Service {
	t.Helper()
	cfg := strictTestConfig()
	cfg.AuthRepository, cfg.InventoryRepository = h, h
	cfg.AuthorizationPolicy = types.AuthorizationPolicyFunc(func(_ context.Context, check types.PolicyCheck) error { return h.policy(check, ownership) })
	svc, err := service.NewStrict(cfg)
	require.NoError(t, err)
	return svc
}

func (h *ownershipHost) ListUsers(_ context.Context, filter types.UserInventoryFilter) (types.UserInventoryPage, error) {
	group := filter.Scope.Label("group")
	if group == uuid.Nil {
		return types.UserInventoryPage{}, types.ErrUnauthorizedScope
	}
	var users []types.AuthUser
	for id, entry := range h.users {
		if h.owners[id] == group {
			users = append(users, *entry.user)
		}
	}
	sort.Slice(users, func(i, j int) bool { return users[i].ID.String() < users[j].ID.String() })
	total := len(users)
	start := min(filter.Pagination.Offset, total)
	end := min(start+filter.Pagination.Limit, total)
	return types.UserInventoryPage{Users: users[start:end], Total: total, NextOffset: end, HasMore: end < total}, nil
}

func TestStrictWiringDoesNotSupplyHostResourceOwnership(t *testing.T) {
	h := newOwnershipHost()
	svc := h.runtime(t, false) // Membership-only policy deliberately omits ownership.
	err := svc.Commands().UserLifecycleTransition.Execute(context.Background(), command.UserLifecycleTransitionInput{
		UserID: h.userB, Target: types.LifecycleStateSuspended, Actor: h.actor, Scope: types.ScopeFilter{}.WithLabel("group", h.groupA),
	})
	require.NoError(t, err)
	require.Equal(t, types.LifecycleStateSuspended, h.users[h.userB].user.Status, "strict wiring alone cannot discover host ownership")
}

func TestHostOwnershipPolicyProtectsDirectAndBulkCommands(t *testing.T) {
	h := newOwnershipHost()
	svc := h.runtime(t, true)
	ctx := context.Background() // Background/job caller with no HTTP middleware.
	scope := types.ScopeFilter{}.WithLabel("group", h.groupA)
	err := svc.Commands().UserUpdate.Execute(ctx, command.UserUpdateInput{User: &types.AuthUser{ID: h.userB, Email: "changed@example.com"}, Actor: h.actor, Scope: scope})
	require.ErrorIs(t, err, types.ErrUnauthorizedScope)
	require.Empty(t, h.users[h.userB].user.Email)
	_, err = svc.Queries().ProfileDetail.Query(ctx, query.ProfileQueryInput{UserID: h.userB, Actor: h.actor, Scope: scope})
	require.ErrorIs(t, err, types.ErrUnauthorizedScope)
	err = svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{UserID: h.userB, Target: types.LifecycleStateArchived, Actor: h.actor, Scope: scope})
	require.ErrorIs(t, err, types.ErrUnauthorizedScope)
	err = svc.Commands().UserCreate.Execute(ctx, command.UserCreateInput{User: &types.AuthUser{Email: "spoof@example.com", Metadata: map[string]any{"group_id": h.groupB.String()}}, Actor: h.actor, Scope: scope})
	require.ErrorIs(t, err, types.ErrUnauthorizedScope)
	require.Len(t, h.users, 3)
	var results []command.BulkUserTransitionResult
	err = svc.Commands().BulkUserTransition.Execute(ctx, command.BulkUserTransitionInput{UserIDs: []uuid.UUID{h.userA, h.userB}, Target: types.LifecycleStateSuspended, Actor: h.actor, Scope: scope, Results: &results})
	require.ErrorIs(t, err, types.ErrUnauthorizedScope)
	require.Len(t, results, 2)
	require.NoError(t, results[0].Err)
	require.ErrorIs(t, results[1].Err, types.ErrUnauthorizedScope)
	require.Equal(t, types.LifecycleStateSuspended, h.users[h.userA].user.Status)
	require.Equal(t, types.LifecycleStateActive, h.users[h.userB].user.Status, "mixed batches preserve the existing per-target/partial-success contract")
}

func TestHostOwnershipPredicatesApplyBeforeCountsAndExportPagination(t *testing.T) {
	h := newOwnershipHost()
	svc := h.runtime(t, true)
	filter := types.UserInventoryFilter{Actor: h.actor, Scope: types.ScopeFilter{}.WithLabel("group", h.groupA), Pagination: types.Pagination{Limit: 1}}
	var exported []uuid.UUID
	for {
		page, err := svc.Queries().UserInventory.Query(context.Background(), filter)
		require.NoError(t, err)
		require.Equal(t, 2, page.Total)
		for _, user := range page.Users {
			require.Equal(t, h.groupA, h.owners[user.ID])
			exported = append(exported, user.ID)
		}
		if !page.HasMore {
			break
		}
		filter.Pagination.Offset = page.NextOffset
	}
	require.ElementsMatch(t, []uuid.UUID{h.userA, h.userA2}, exported)
}
