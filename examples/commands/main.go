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

type demoEnv struct {
	svc   *users.Service
	actor types.ActorRef
}

func main() {
	ctx := context.Background()
	env := newDemoEnv()
	if err := env.svc.HealthCheck(ctx); err != nil {
		panic(err)
	}
	inviteResult := inviteAndActivate(ctx, env)
	printInventory(ctx, env)
	role := createAndAssignRole(ctx, env, inviteResult.User.ID)
	printAssignments(ctx, env, inviteResult.User, role.ID)
	printActivity(ctx, env)
}

func newDemoEnv() demoEnv {
	repo := memory.NewAuthRepository()
	activityStore := memory.NewActivityStore()
	roleRegistry := memory.NewRoleRegistry()
	profileRepo := memory.NewProfileRepository()
	preferenceRepo := memory.NewPreferenceRepository()
	tokenRepo := memory.NewUserTokenRepository()
	secureLinks := memory.NewSecureLinkManager()

	return demoEnv{
		svc: users.New(users.Config{
			AuthRepository:       repo,
			InventoryRepository:  repo,
			RoleRegistry:         roleRegistry,
			ActivitySink:         activityStore,
			ActivityRepository:   activityStore,
			ProfileRepository:    profileRepo,
			PreferenceRepository: preferenceRepo,
			UserTokenRepository:  tokenRepo,
			SecureLinkManager:    secureLinks,
			Hooks: types.Hooks{
				AfterLifecycle: func(_ context.Context, event types.LifecycleEvent) {
					log.Printf("[hook] lifecycle %s -> %s\n", event.FromState, event.ToState)
				},
			},
			Logger: types.NopLogger{},
		}),
		actor: types.ActorRef{ID: uuid.New(), Type: "system"},
	}
}

func inviteAndActivate(ctx context.Context, env demoEnv) *command.UserInviteResult {
	inviteResult := &command.UserInviteResult{}
	err := env.svc.Commands().UserInvite.Execute(ctx, command.UserInviteInput{
		Email:  "sample@example.com",
		Actor:  env.actor,
		Result: inviteResult,
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("Invited user %s with token %s\n", inviteResult.User.Email, inviteResult.Token)

	err = env.svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
		UserID: inviteResult.User.ID,
		Target: types.LifecycleStateActive,
		Actor:  env.actor,
		Reason: "demo activation",
	})
	if err != nil {
		panic(err)
	}
	return inviteResult
}

func printInventory(ctx context.Context, env demoEnv) {
	page, err := env.svc.Queries().UserInventory.Query(ctx, types.UserInventoryFilter{
		Actor:      env.actor,
		Pagination: types.Pagination{Limit: 10},
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("Inventory query returned %d users (total=%d)\n", len(page.Users), page.Total)
}

func createAndAssignRole(ctx context.Context, env demoEnv, userID uuid.UUID) *types.RoleDefinition {
	role := &types.RoleDefinition{}
	err := env.svc.Commands().CreateRole.Execute(ctx, command.CreateRoleInput{
		Name:   "Editors",
		Actor:  env.actor,
		Result: role,
	})
	if err != nil {
		panic(err)
	}
	err = env.svc.Commands().AssignRole.Execute(ctx, command.AssignRoleInput{
		UserID: userID,
		RoleID: role.ID,
		Actor:  env.actor,
	})
	if err != nil {
		panic(err)
	}
	return role
}

func printAssignments(ctx context.Context, env demoEnv, user *types.AuthUser, roleID uuid.UUID) {
	assignments, err := env.svc.Queries().RoleAssignments.Query(ctx, types.RoleAssignmentFilter{
		Actor:  env.actor,
		Scope:  types.ScopeFilter{},
		UserID: user.ID,
		RoleID: roleID,
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("Assignments for %s: %d\n", user.Email, len(assignments))
}

func printActivity(ctx context.Context, env demoEnv) {
	feed, err := env.svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
		Actor:      env.actor,
		Scope:      types.ScopeFilter{},
		Pagination: types.Pagination{Limit: 10},
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("Recent activity entries: %d\n", len(feed.Records))
}
