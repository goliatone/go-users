package main

import (
	"context"
	"fmt"
	"log"

	users "github.com/goliatone/go-users"
	"github.com/goliatone/go-users/command"
	"github.com/goliatone/go-users/examples/internal/memory"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/query"
	"github.com/google/uuid"
)

func main() {
	ctx := context.Background()
	app := newAdminApp()

	app.seedTenant("ops", ctx, seedingConfig{
		ActorKey: "ops-admin",
		Email:    "maya.ops@example.com",
		RoleName: "ops.editors",
	})

	app.seedTenant("commerce", ctx, seedingConfig{
		ActorKey: "commerce-admin",
		Email:    "leon@commerce.example.com",
		RoleName: "commerce.moderators",
	})

	app.renderDashboard(ctx)
}

type adminApp struct {
	svc          *users.Service
	cms          *cmsBridge
	tenants      map[string]uuid.UUID
	actors       map[string]types.ActorRef
	actorTenants map[string]string
	actorOrder   []string
	users        map[string]uuid.UUID
	tenantDir    *tenantDirectory
}

func newAdminApp() *adminApp {
	repo := memory.NewAuthRepository()
	roleRegistry := memory.NewRoleRegistry()
	activityStore := memory.NewActivityStore()
	profileRepo := memory.NewProfileRepository()
	preferenceRepo := memory.NewPreferenceRepository()

	cms := newCMSBridge()
	directory := newTenantDirectory()

	tenants := map[string]uuid.UUID{
		"ops":      uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		"commerce": uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
	}
	actors := map[string]types.ActorRef{
		"ops-admin": {
			ID:   uuid.New(),
			Type: "admin",
		},
		"commerce-admin": {
			ID:   uuid.New(),
			Type: "admin",
		},
	}
	actorTenants := map[string]string{
		"ops-admin":      "ops",
		"commerce-admin": "commerce",
	}
	actorOrder := []string{"ops-admin", "commerce-admin"}
	directory.Set(actors["ops-admin"].ID, tenants[actorTenants["ops-admin"]])
	directory.Set(actors["commerce-admin"].ID, tenants[actorTenants["commerce-admin"]])

	svc := users.New(users.Config{
		AuthRepository:       repo,
		InventoryRepository:  repo,
		ActivitySink:         activityStore,
		ActivityRepository:   activityStore,
		RoleRegistry:         roleRegistry,
		ProfileRepository:    profileRepo,
		PreferenceRepository: preferenceRepo,
		Hooks: types.Hooks{
			AfterActivity:         cms.ForwardActivity,
			AfterPreferenceChange: cms.ForwardPreference,
		},
		ScopeResolver:       directory.Resolver(),
		AuthorizationPolicy: directory.Policy(),
	})

	if err := svc.HealthCheck(context.Background()); err != nil {
		log.Fatalf("service not ready: %v", err)
	}

	return &adminApp{
		svc:          svc,
		cms:          cms,
		tenants:      tenants,
		actors:       actors,
		actorTenants: actorTenants,
		actorOrder:   actorOrder,
		users:        make(map[string]uuid.UUID),
		tenantDir:    directory,
	}
}

type seedingConfig struct {
	ActorKey string
	Email    string
	RoleName string
}

func (a *adminApp) seedTenant(tenantKey string, ctx context.Context, cfg seedingConfig) {
	actor := a.actors[cfg.ActorKey]
	scope := types.ScopeFilter{TenantID: a.tenants[tenantKey]}

	invite := &command.UserInviteResult{}
	err := a.svc.Commands().UserInvite.Execute(ctx, command.UserInviteInput{
		Email:  cfg.Email,
		Actor:  actor,
		Scope:  scope,
		Result: invite,
	})
	must("invite user", err)

	err = a.svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
		UserID: invite.User.ID,
		Target: types.LifecycleStateActive,
		Actor:  actor,
		Scope:  scope,
		Reason: "seed demo",
	})
	must("activate user", err)

	role := &types.RoleDefinition{}
	err = a.svc.Commands().CreateRole.Execute(ctx, command.CreateRoleInput{
		Name:        cfg.RoleName,
		Description: fmt.Sprintf("%s admin widget role", tenantKey),
		Permissions: []string{"widget.user_stats", "widget.recent_activity"},
		Actor:       actor,
		Scope:       scope,
		Result:      role,
	})
	must("create role", err)

	err = a.svc.Commands().AssignRole.Execute(ctx, command.AssignRoleInput{
		UserID: invite.User.ID,
		RoleID: role.ID,
		Actor:  actor,
		Scope:  scope,
	})
	must("assign role", err)

	display := fmt.Sprintf("%s lead", tenantKey)
	err = a.svc.Commands().ProfileUpsert.Execute(ctx, command.ProfileUpsertInput{
		UserID: invite.User.ID,
		Actor:  actor,
		Scope:  scope,
		Patch: types.ProfilePatch{
			DisplayName: &display,
			Locale:      strPtr("en-US"),
			Timezone:    strPtr("America/New_York"),
			Contact: map[string]any{
				"email": cfg.Email,
				"slack": fmt.Sprintf("#%s-team", tenantKey),
			},
		},
	})
	must("upsert profile", err)

	err = a.svc.Commands().PreferenceUpsert.Execute(ctx, command.PreferenceUpsertInput{
		UserID: invite.User.ID,
		Actor:  actor,
		Scope:  scope,
		Level:  types.PreferenceLevelUser,
		Key:    "widgets.dashboard.layout",
		Value: map[string]any{
			"primary":   "user_stats",
			"secondary": "recent_activity",
		},
	})
	must("upsert preference", err)

	err = a.svc.Commands().LogActivity.Execute(ctx, command.ActivityLogInput{
		Record: types.ActivityRecord{
			ActorID:    actor.ID,
			Verb:       "admin.dashboard.refreshed",
			Channel:    "dashboards",
			ObjectType: "dashboard",
			ObjectID:   tenantKey,
			TenantID:   scope.TenantID,
			OrgID:      scope.OrgID,
			Data: map[string]any{
				"widget_count": 2,
			},
		},
	})
	must("log dashboard refresh", err)

	a.users[tenantKey] = invite.User.ID
}

func (a *adminApp) renderDashboard(ctx context.Context) {
	fmt.Println("== go-admin dashboard widgets ==")
	for _, actorKey := range a.actorOrder {
		tenantKey := a.actorTenants[actorKey]
		actor := a.actors[actorKey]
		scope := types.ScopeFilter{TenantID: a.tenants[tenantKey]}
		fmt.Printf("-- Widgets for tenant %s (actor %s)\n", tenantKey, actorKey)

		stats, err := a.svc.Queries().ActivityStats.Query(ctx, types.ActivityStatsFilter{
			Actor: actor,
			Scope: scope,
		})
		must("stats widget", err)
		fmt.Printf("user_stats: %+v\n", stats.ByVerb)

		feed, err := a.svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
			Actor:      actor,
			Scope:      scope,
			Pagination: types.Pagination{Limit: 5},
		})
		must("activity widget", err)
		for _, record := range feed.Records {
			fmt.Printf("%s → %s (%s)\n", record.Verb, record.ObjectID, record.Channel)
		}

		if userID := a.users[tenantKey]; userID != uuid.Nil {
			snapshot, err := a.svc.Queries().Preferences.Query(ctx, query.PreferenceQueryInput{
				Actor:  actor,
				Scope:  scope,
				UserID: userID,
				Keys:   []string{"widgets.dashboard.layout"},
			})
			if err == nil && len(snapshot.Effective) > 0 {
				fmt.Printf("preference effective keys: %v\n", keysOf(snapshot.Effective))
			}
		}
	}

	fmt.Println("== cms/go-cms bridge ==")
	for _, evt := range a.cms.Events() {
		fmt.Println(evt)
	}
}

func must(action string, err error) {
	if err != nil {
		log.Fatalf("%s failed: %v", action, err)
	}
}

func strPtr(s string) *string { return &s }

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

type cmsBridge struct {
	events []string
}

func newCMSBridge() *cmsBridge {
	return &cmsBridge{events: make([]string, 0, 8)}
}

func (c *cmsBridge) ForwardActivity(_ context.Context, record types.ActivityRecord) {
	entry := fmt.Sprintf("[activity:%s] %s %s", record.Channel, record.Verb, record.ObjectID)
	c.events = append(c.events, entry)
}

func (c *cmsBridge) ForwardPreference(_ context.Context, evt types.PreferenceEvent) {
	entry := fmt.Sprintf("[preferences] %s %s %s", evt.Action, evt.UserID, evt.Key)
	c.events = append(c.events, entry)
}

func (c *cmsBridge) Events() []string {
	return append([]string{}, c.events...)
}

type tenantDirectory struct {
	mapping map[uuid.UUID]uuid.UUID
}

func newTenantDirectory() *tenantDirectory {
	return &tenantDirectory{mapping: make(map[uuid.UUID]uuid.UUID)}
}

func (d *tenantDirectory) Set(actorID, tenantID uuid.UUID) {
	d.mapping[actorID] = tenantID
}

func (d *tenantDirectory) Lookup(actorID uuid.UUID) uuid.UUID {
	return d.mapping[actorID]
}

func (d *tenantDirectory) Resolver() types.ScopeResolver {
	return types.ScopeResolverFunc(func(_ context.Context, actor types.ActorRef, requested types.ScopeFilter) (types.ScopeFilter, error) {
		if requested.TenantID == uuid.Nil {
			requested.TenantID = d.mapping[actor.ID]
		}
		return requested, nil
	})
}

func (d *tenantDirectory) Policy() types.AuthorizationPolicy {
	return types.AuthorizationPolicyFunc(func(_ context.Context, check types.PolicyCheck) error {
		if tenant := d.mapping[check.Actor.ID]; tenant != uuid.Nil && tenant != check.Scope.TenantID {
			return types.ErrUnauthorizedScope
		}
		return nil
	})
}
