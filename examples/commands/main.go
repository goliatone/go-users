package main

import (
	"context"
	"fmt"
	"log"

	users "github.com/goliatone/go-users"
	"github.com/goliatone/go-users/command"
	"github.com/goliatone/go-users/examples/internal/memory"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

func main() {
	ctx := context.Background()
	repo := memory.NewAuthRepository()
	activityStore := memory.NewActivityStore()
	roleRegistry := memory.NewRoleRegistry()
	profileRepo := memory.NewProfileRepository()
	preferenceRepo := memory.NewPreferenceRepository()

	svc := users.New(users.Config{
		AuthRepository:       repo,
		InventoryRepository:  repo,
		RoleRegistry:         roleRegistry,
		ActivitySink:         activityStore,
		ActivityRepository:   activityStore,
		ProfileRepository:    profileRepo,
		PreferenceRepository: preferenceRepo,
		Hooks: types.Hooks{
			AfterLifecycle: func(_ context.Context, event types.LifecycleEvent) {
				log.Printf("[hook] lifecycle %s -> %s\n", event.FromState, event.ToState)
			},
		},
		Logger: types.NopLogger{},
	})

	if err := svc.HealthCheck(ctx); err != nil {
		panic(err)
	}

	actor := types.ActorRef{ID: uuid.New(), Type: "system"}
	inviteResult := &command.UserInviteResult{}
	err := svc.Commands().UserInvite.Execute(ctx, command.UserInviteInput{
		Email:  "sample@example.com",
		Actor:  actor,
		Result: inviteResult,
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("Invited user %s with token %s\n", inviteResult.User.Email, inviteResult.Token)

	err = svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
		UserID: inviteResult.User.ID,
		Target: types.LifecycleStateActive,
		Actor:  actor,
		Reason: "demo activation",
	})
	if err != nil {
		panic(err)
	}

	page, err := svc.Queries().UserInventory.Query(ctx, types.UserInventoryFilter{
		Actor:      actor,
		Pagination: types.Pagination{Limit: 10},
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("Inventory query returned %d users (total=%d)\n", len(page.Users), page.Total)

	role := &types.RoleDefinition{}
	err = svc.Commands().CreateRole.Execute(ctx, command.CreateRoleInput{
		Name:   "Editors",
		Actor:  actor,
		Result: role,
	})
	if err != nil {
		panic(err)
	}
	err = svc.Commands().AssignRole.Execute(ctx, command.AssignRoleInput{
		UserID: inviteResult.User.ID,
		RoleID: role.ID,
		Actor:  actor,
	})
	if err != nil {
		panic(err)
	}
	assignments, err := svc.Queries().RoleAssignments.Query(ctx, types.RoleAssignmentFilter{
		Actor:  actor,
		Scope:  types.ScopeFilter{},
		UserID: inviteResult.User.ID,
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("Assignments for %s: %d\n", inviteResult.User.Email, len(assignments))

	feed, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
		Actor:      actor,
		Scope:      types.ScopeFilter{},
		Pagination: types.Pagination{Limit: 10},
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("Recent activity entries: %d\n", len(feed.Records))
}
