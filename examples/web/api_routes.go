package main

import (
	auth "github.com/goliatone/go-auth"
	"github.com/goliatone/go-crud"
	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-users/activity"
	"github.com/goliatone/go-users/crudguard"
	"github.com/goliatone/go-users/crudsvc"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/preferences"
	"github.com/goliatone/go-users/profile"
	"github.com/goliatone/go-users/registry"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

// RegisterAPIRoutes mounts all go-crud controllers under /api
func RegisterAPIRoutes(app *App) {
	api := app.srv.Router().Group("/api")
	apiAdapter := crud.NewGoRouterAdapter(api)
	logger := app.GetLogger("api")
	emitter := crudsvc.SinkEmitter{Sink: app.users.ActivitySink()}
	commands := app.users.Commands()
	queries := app.users.Queries()
	scopeGuard := app.users.ScopeGuard()

	activityGuard := mustGuardAdapter(app, scopeGuard, types.PolicyActionActivityRead, types.PolicyActionActivityWrite, "activity")
	activityService := crudsvc.NewActivityService(crudsvc.ActivityServiceConfig{
		Guard:      activityGuard,
		LogCommand: commands.LogActivity,
		FeedQuery:  queries.ActivityFeed,
	}, crudsvc.WithLogger(&loggerAdapter{app.GetLogger("svc:activity")}))
	activityController := crud.NewController(createActivityRepository(app),
		crud.WithService(activityService),
		crud.WithRouteConfig[*activity.LogEntry](crud.RouteConfig{
			Operations: map[crud.CrudOperation]crud.RouteOptions{
				crud.OpUpdate:      {Enabled: crud.BoolPtr(false)},
				crud.OpDelete:      {Enabled: crud.BoolPtr(false)},
				crud.OpCreateBatch: {Enabled: crud.BoolPtr(false)},
				crud.OpUpdateBatch: {Enabled: crud.BoolPtr(false)},
				crud.OpDeleteBatch: {Enabled: crud.BoolPtr(false)},
			},
		}),
	)
	activityController.RegisterRoutes(apiAdapter)
	app.registerSchemaProvider(activityController)

	profileRepo := createProfileRepository(app)
	profileController := crud.NewController(profileRepo)
	profileController.RegisterRoutes(apiAdapter)
	app.registerSchemaProvider(profileController)

	preferenceGuard := mustGuardAdapter(app, scopeGuard, types.PolicyActionPreferencesRead, types.PolicyActionPreferencesWrite, "preferences")
	preferenceService := crudsvc.NewPreferenceService(crudsvc.PreferenceServiceConfig{
		Guard:  preferenceGuard,
		Repo:   app.preferenceRepo,
		Store:  app.preferenceRepo,
		Upsert: commands.PreferenceUpsert,
		Delete: commands.PreferenceDelete,
	}, crudsvc.WithActivityEmitter(emitter), crudsvc.WithLogger(&loggerAdapter{app.GetLogger("svc:preferences")}))
	preferenceController := crud.NewController(createPreferenceRepository(app),
		crud.WithService(preferenceService),
	)
	preferenceController.RegisterRoutes(apiAdapter)
	app.registerSchemaProvider(preferenceController)

	roleGuard := mustGuardAdapter(app, scopeGuard, types.PolicyActionRolesRead, types.PolicyActionRolesWrite, "roles")
	roleService := crudsvc.NewRoleService(crudsvc.RoleServiceConfig{
		Guard:  roleGuard,
		Create: commands.CreateRole,
		Update: commands.UpdateRole,
		Delete: commands.DeleteRole,
		List:   queries.RoleList,
		Detail: queries.RoleDetail,
	}, crudsvc.WithActivityEmitter(emitter), crudsvc.WithLogger(&loggerAdapter{app.GetLogger("svc:roles")}))
	roleController := crud.NewController(createRoleRepository(app),
		crud.WithService(roleService),
	)
	roleController.RegisterRoutes(apiAdapter)
	app.registerSchemaProvider(roleController)

	assignmentRepo := createAssignmentRepository(app)
	assignmentController := crud.NewController(assignmentRepo)
	assignmentController.RegisterRoutes(apiAdapter)
	app.registerSchemaProvider(assignmentController)

	userGuard := mustGuardAdapter(app, scopeGuard, types.PolicyActionUsersRead, types.PolicyActionUsersWrite, "users")
	userService := crudsvc.NewUserService(crudsvc.UserServiceConfig{
		Guard:     userGuard,
		Inventory: queries.UserInventory,
		AuthRepo:  app.authRepo,
	}, crudsvc.WithLogger(&loggerAdapter{app.GetLogger("svc:users")}))
	userController := crud.NewController(createUserRepository(app),
		crud.WithService(userService),
		crud.WithRouteConfig[*auth.User](crud.RouteConfig{
			Operations: map[crud.CrudOperation]crud.RouteOptions{
				crud.OpCreate:      {Enabled: crud.BoolPtr(false)},
				crud.OpUpdate:      {Enabled: crud.BoolPtr(false)},
				crud.OpDelete:      {Enabled: crud.BoolPtr(false)},
				crud.OpCreateBatch: {Enabled: crud.BoolPtr(false)},
				crud.OpUpdateBatch: {Enabled: crud.BoolPtr(false)},
				crud.OpDeleteBatch: {Enabled: crud.BoolPtr(false)},
			},
		}),
	)
	userController.RegisterRoutes(apiAdapter)
	app.registerSchemaProvider(userController)

	if app.schemaRegistry != nil {
		// Exports the go-users CRUD catalog for go-admin/go-cms ingestion
		app.srv.Router().Get("/admin/schemas", app.schemaRegistry.Handler())
	}

	logger.Info("API routes registered", "prefix", "/api")
}

func createActivityRepository(app *App) repository.Repository[*activity.LogEntry] {
	handlers := repository.ModelHandlers[*activity.LogEntry]{
		NewRecord: func() *activity.LogEntry {
			return &activity.LogEntry{}
		},
		GetID: func(r *activity.LogEntry) uuid.UUID {
			return r.ID
		},
		SetID: func(r *activity.LogEntry, id uuid.UUID) {
			r.ID = id
		},
		GetIdentifier: func() string {
			return "verb"
		},
	}
	return repository.NewRepository(app.bunDB, handlers)
}

func createProfileRepository(app *App) repository.Repository[*profile.Record] {
	handlers := repository.ModelHandlers[*profile.Record]{
		NewRecord: func() *profile.Record {
			return &profile.Record{}
		},
		GetID: func(r *profile.Record) uuid.UUID {
			return r.UserID
		},
		SetID: func(r *profile.Record, id uuid.UUID) {
			r.UserID = id
		},
		GetIdentifier: func() string {
			return "display_name"
		},
	}
	return repository.NewRepository(app.bunDB, handlers)
}

func createPreferenceRepository(app *App) repository.Repository[*preferences.Record] {
	handlers := repository.ModelHandlers[*preferences.Record]{
		NewRecord: func() *preferences.Record {
			return &preferences.Record{}
		},
		GetID: func(r *preferences.Record) uuid.UUID {
			return r.ID
		},
		SetID: func(r *preferences.Record, id uuid.UUID) {
			r.ID = id
		},
		GetIdentifier: func() string {
			return "key"
		},
	}
	return repository.NewRepository(app.bunDB, handlers)
}

func createRoleRepository(app *App) repository.Repository[*registry.CustomRole] {
	handlers := repository.ModelHandlers[*registry.CustomRole]{
		NewRecord: func() *registry.CustomRole {
			return &registry.CustomRole{}
		},
		GetID: func(r *registry.CustomRole) uuid.UUID {
			return r.ID
		},
		SetID: func(r *registry.CustomRole, id uuid.UUID) {
			r.ID = id
		},
		GetIdentifier: func() string {
			return "name"
		},
	}
	return repository.NewRepository(app.bunDB, handlers)
}

func createAssignmentRepository(app *App) repository.Repository[*registry.RoleAssignment] {
	handlers := repository.ModelHandlers[*registry.RoleAssignment]{
		NewRecord: func() *registry.RoleAssignment {
			return &registry.RoleAssignment{}
		},
		GetID: func(r *registry.RoleAssignment) uuid.UUID {
			return r.RoleID
		},
		SetID: func(r *registry.RoleAssignment, id uuid.UUID) {
			r.RoleID = id
		},
		GetIdentifier: func() string {
			return "user_id"
		},
	}
	return repository.NewRepository(app.bunDB, handlers)
}

func createUserRepository(app *App) repository.Repository[*auth.User] {
	handlers := repository.ModelHandlers[*auth.User]{
		NewRecord: func() *auth.User {
			return &auth.User{}
		},
		GetID: func(u *auth.User) uuid.UUID {
			return u.ID
		},
		SetID: func(u *auth.User, id uuid.UUID) {
			u.ID = id
		},
		GetIdentifier: func() string {
			return "email"
		},
	}
	return repository.NewRepository(app.bunDB, handlers)
}

func mustGuardAdapter(app *App, guard scope.Guard, read, write types.PolicyAction, name string) *crudguard.Adapter {
	adapter, err := crudguard.NewAdapter(crudguard.Config{
		Guard:     guard,
		Logger:    &loggerAdapter{app.GetLogger("guard:" + name)},
		PolicyMap: crudguard.DefaultPolicyMap(read, write),
	})
	if err != nil {
		app.GetLogger("guard").Error("failed to build guard adapter", err, "resource", name)
		return nil
	}
	return adapter
}
