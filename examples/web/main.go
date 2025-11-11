package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/goliatone/go-auth"
	"github.com/goliatone/go-auth/middleware/csrf"
	authrepo "github.com/goliatone/go-auth/repository"
	cfs "github.com/goliatone/go-composite-fs"
	gconfig "github.com/goliatone/go-config/config"
	"github.com/goliatone/go-errors"
	"github.com/goliatone/go-logger/glog"
	"github.com/goliatone/go-persistence-bun"
	"github.com/goliatone/go-print"
	"github.com/goliatone/go-router"
	mflash "github.com/goliatone/go-router/middleware/flash"
	users "github.com/goliatone/go-users"
	"github.com/goliatone/go-users/activity"
	goauth "github.com/goliatone/go-users/adapter/goauth"
	"github.com/goliatone/go-users/examples/web/config"
	"github.com/goliatone/go-users/pkg/schema"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/preferences"
	"github.com/goliatone/go-users/profile"
	"github.com/goliatone/go-users/registry"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
)

const (
	tenantMetadataKey    = "tenant_id"
	workspaceMetadataKey = "workspace_id"
)

var (
	tenantOpsID         = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	tenantCommerceID    = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	workspaceOpsID      = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	workspaceCommerceID = uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
)

//go:embed public
var assetsFS embed.FS

type App struct {
	config         *gconfig.Container[*config.BaseConfig]
	bunDB          *bun.DB
	auth           auth.Authenticator
	auther         auth.HTTPAuthenticator
	repo           auth.RepositoryManager
	srv            router.Server[*fiber.App]
	logger         *glog.BaseLogger
	users          *users.Service
	schemaNotifier *schema.Notifier
	schemaRegistry *schema.Registry
	schemaFeed     *schemaFeed
	activityRepo   *activity.Repository
	profileRepo    *profile.Repository
	preferenceRepo *preferences.Repository
	roleRegistry   *registry.RoleRegistry
	authRepo       types.AuthRepository
	workspaceDir   *workspaceDirectory
	demoUsers      map[string]uuid.UUID
	demoScopes     map[string]types.ScopeFilter
}

func (a *App) Config() *config.BaseConfig {
	return a.config.Raw()
}

func (a *App) SetRepository(repo auth.RepositoryManager) {
	a.repo = repo
}

func (a *App) SetDB(db *bun.DB) {
	a.bunDB = db
}

func (a *App) SetLogger(lgr *glog.BaseLogger) *App {
	a.logger = lgr
	return a
}

func (a *App) GetLogger(name string) glog.Logger {
	return a.logger.GetLogger(name)
}

func (a *App) SetHTTPServer(srv router.Server[*fiber.App]) {
	a.srv = srv
}

func (a *App) SetAuthenticator(auth auth.Authenticator) {
	a.auth = auth
}

func (a *App) SetHTTPAuth(auther auth.HTTPAuthenticator) {
	a.auther = auther
}

func (a *App) SetUserService(svc *users.Service) {
	a.users = svc
}

func (a *App) registerSchemaProvider(provider router.MetadataProvider) {
	if a.schemaRegistry == nil || provider == nil {
		return
	}
	a.schemaRegistry.Register(provider)
}

func main() {
	lgr := glog.NewLogger(
		glog.WithLoggerTypePretty(),
		glog.WithLevel(glog.Trace),
		glog.WithName("app"),
		glog.WithAddSource(false),
		glog.WithRichErrorHandler(errors.ToSlogAttributes),
	)

	cfg := gconfig.New(&config.BaseConfig{
		Server: config.ServerConfig{
			Host: "localhost",
			Port: "8978",
		},
		Auth: config.AuthConfig{
			SigningKey:            "changeme-secret-key-please-use-env-var",
			SigningMethod:         "HS256",
			ContextKey:            "user",
			TokenExpiration:       3600,
			ExtendedTokenDuration: 86400,
			TokenLookup:           "cookie:auth_token",
			AuthScheme:            "Bearer",
			Issuer:                "go-users-web",
			RejectedRouteKey:      "rejected_route",
			RejectedRouteDefault:  "/auth/login",
		},
		Persistence: config.PersistenceConfig{
			Debug:          true,
			Driver:         "sqlite",
			Server:         "file::memory:?cache=shared",
			PingTimeout:    5 * time.Second,
			OtelIdentifier: "go-users-web",
		},
	}).WithLogger(lgr.GetLogger("config"))

	ctx := context.Background()
	if err := cfg.Load(ctx); err != nil {
		panic(err)
	}

	fmt.Println("============")
	fmt.Println(print.MaybeHighlightJSON(cfg.Raw()))
	fmt.Println("============")

	notifier := schema.NewNotifier()
	feed := newSchemaFeed(10)
	app := &App{
		config:         cfg,
		logger:         lgr,
		schemaNotifier: notifier,
		schemaRegistry: schema.NewRegistry(
			schema.WithPublisher(notifier),
			schema.WithInfo(router.OpenAPIInfo{
				Title:       "go-users Admin Schemas",
				Version:     "1.0.0",
				Description: "Aggregated CRUD schemas served by go-users",
			}),
			schema.WithTags("admin", "schemas"),
		),
		schemaFeed:   feed,
		workspaceDir: newWorkspaceDirectory(tenantOpsID, workspaceOpsID),
		demoUsers:    make(map[string]uuid.UUID),
		demoScopes:   make(map[string]types.ScopeFilter),
	}
	app.schemaNotifier.Register(func(_ context.Context, actorID uuid.UUID, _ map[string]any) {
		app.GetLogger("schema").Info("schema refresh requested", "actor_id", actorID)
	})
	if app.schemaRegistry != nil {
		app.schemaRegistry.Subscribe(func(_ context.Context, snap schema.Snapshot) {
			if app.schemaFeed != nil {
				app.schemaFeed.Append(snap)
			}
			app.GetLogger("schema").Info("schema registry updated", "resources", snap.ResourceNames)
		})
	}

	if err := WithPersistence(ctx, app); err != nil {
		panic(err)
	}

	if err := WithHTTPServer(ctx, app); err != nil {
		panic(err)
	}

	if err := WithHTTPAuth(ctx, app); err != nil {
		panic(err)
	}

	if err := WithUserService(ctx, app); err != nil {
		panic(err)
	}
	if err := seedActivityData(ctx, app); err != nil {
		panic(err)
	}

	// Register routes - these are defined in api_routes.go and web_routes.go
	RegisterAPIRoutes(app)
	RegisterWebRoutes(app)

	serverCfg := app.Config().GetServer()
	addr := fmt.Sprintf("%s:%s", serverCfg.Host, serverCfg.Port)
	log.Printf("Starting server on http://%s\n", addr)
	app.srv.Serve(addr)

	WaitExitSignal()
}

func renderWithGlobals(ctx router.Context, name string, data router.ViewContext) error {
	viewData := auth.MergeTemplateData(ctx, data)
	if _, ok := viewData[csrf.DefaultTemplateHelpersKey]; !ok {
		viewData[csrf.DefaultTemplateHelpersKey] = auth.TemplateHelpersWithRouter(ctx, auth.TemplateUserKey)
	}
	return ctx.Render(name, viewData)
}

func WithHTTPServer(ctx context.Context, app *App) error {
	vcfg := router.NewSimpleViewConfig("./views").
		WithExt(".html").
		WithDebug(true).
		WithReload(true).
		WithAssets("./public", "/css", "/js").
		WithFunctions(auth.TemplateHelpers())

	// Set up composite filesystem for templates
	// Note: Auth templates (login, register, password_reset) are copied from go-auth
	// to ./views for easier customization. Consider symlinking or using composite FS
	// with proper path resolution in the future.
	vcfg.TemplateFS = []fs.FS{
		cfs.NewCompositeFS(
			os.DirFS("./views"),
		),
	}

	engine, err := router.InitializeViewEngine(vcfg, app.GetLogger("views"))
	if err != nil {
		return err
	}

	srv := router.NewFiberAdapter(func(a *fiber.App) *fiber.App {
		return fiber.New(fiber.Config{
			UnescapePath:      true,
			EnablePrintRoutes: true,
			StrictRouting:     false,
			PassLocalsToViews: true,
			Views:             engine,
		})
	})

	// Register static files from ./public directory
	srv.Router().Static("/", "./public")

	srv.Router().WithLogger(app.GetLogger("router"))

	// key := sha256.Sum256([]byte(app.Config().GetAuth().GetSigningKey()))
	// srv.Router().Use(csrf.New(csrf.Config{
	// 	SecureKey: key[:],
	// }))

	// csrf.RegisterRoutes(srv.Router())

	srv.Router().Use(mflash.New(mflash.ConfigDefault))

	srv.Router().Get("/", renderHome(app))

	app.SetHTTPServer(srv)

	return nil
}

func WithPersistence(ctx context.Context, app *App) error {
	cfg := app.config.Raw().GetPersistence()
	// Get DSN from Server field (for SQLite, this is the file path or :memory:)
	dsn := cfg.GetServer()
	if dsn == "" {
		dsn = "file::memory:?cache=shared"
	}

	db, err := sql.Open(sqliteshim.ShimName, dsn)
	if err != nil {
		log.Fatal(err)
		return err
	}

	// Register all models
	persistence.RegisterModel((*auth.User)(nil))
	persistence.RegisterModel((*auth.PasswordReset)(nil))
	persistence.RegisterModel((*activity.LogEntry)(nil))
	persistence.RegisterModel((*profile.Record)(nil))
	persistence.RegisterModel((*preferences.Record)(nil))
	persistence.RegisterModel((*registry.CustomRole)(nil))
	persistence.RegisterModel((*registry.RoleAssignment)(nil))

	dialect := sqlitedialect.New()
	bunClient, err2 := persistence.New(cfg, db, dialect)
	if err2 != nil {
		log.Fatal(err2)
		return err2
	}
	// Use err to avoid "no new variables" error
	err = err2
	if err != nil {
		log.Fatal(err)
		return err
	}

	bunClient.SetLogger(app.GetLogger("persistence"))

	// Register dialect-aware migrations (supports both PostgreSQL and SQLite)
	// Create a sub-FS rooted at data/sql/migrations so the loader can find the files
	migrationsFS, err := fs.Sub(users.MigrationsFS, "data/sql/migrations")
	if err != nil {
		return err
	}

	bunClient.RegisterDialectMigrations(
		migrationsFS,
		persistence.WithDialectSourceLabel("."),
		persistence.WithValidationTargets("postgres", "sqlite"),
	)

	// Optional: Validate that both dialects have complete migration sets
	if err := bunClient.ValidateDialects(ctx); err != nil {
		log.Printf("Warning: dialect validation failed: %v", err)
	}

	if err := bunClient.Migrate(ctx); err != nil {
		return err
	}

	if report := bunClient.Report(); report != nil && !report.IsZero() {
		fmt.Printf("report: %s\n", report.String())
	}

	app.SetDB(bunClient.DB())
	repoManager := authrepo.NewRepositoryManager(bunClient.DB())
	app.SetRepository(repoManager)

	if err := seedDemoUsers(ctx, app); err != nil {
		return err
	}

	return nil
}

func WithHTTPAuth(ctx context.Context, app *App) error {
	cfg := app.Config().GetAuth()

	repo := auth.NewRepositoryManager(app.bunDB)

	if err := repo.Validate(); err != nil {
		return err
	}

	// Create a wrapper that implements UserTracker interface
	userTracker := &userTrackerAdapter{users: repo.Users()}

	userProvider := auth.NewUserProvider(userTracker)
	userProvider.WithLogger(app.GetLogger("auth:prv"))

	authenticator := auth.NewAuthenticator(userProvider, cfg)
	authenticator.WithLogger(app.GetLogger("auth:authz"))

	app.SetAuthenticator(authenticator)

	httpAuth, err := auth.NewHTTPAuthenticator(authenticator, cfg)
	if err != nil {
		return err
	}

	httpAuth.WithLogger(app.GetLogger("auth:http"))

	app.SetHTTPAuth(httpAuth)

	auth.RegisterAuthRoutes(app.srv.Router().Group("/"),
		func(ac *auth.AuthController) *auth.AuthController {
			ac.Debug = true
			ac.Auther = httpAuth
			ac.Repo = repo
			ac.WithLogger(app.GetLogger("auth:ctrl"))
			return ac
		})

	return nil
}

func WithUserService(ctx context.Context, app *App) error {
	activityRepo, err := activity.NewRepository(activity.RepositoryConfig{
		DB: app.bunDB,
	})
	if err != nil {
		return err
	}

	profileRepo, err := profile.NewRepository(profile.RepositoryConfig{
		DB: app.bunDB,
	})
	if err != nil {
		return err
	}

	preferenceRepo, err := preferences.NewRepository(preferences.RepositoryConfig{
		DB: app.bunDB,
	})
	if err != nil {
		return err
	}

	roleRegistry, err := registry.NewRoleRegistry(registry.RoleRegistryConfig{
		DB: app.bunDB,
	})
	if err != nil {
		return err
	}

	app.activityRepo = activityRepo
	app.profileRepo = profileRepo
	app.preferenceRepo = preferenceRepo
	app.roleRegistry = roleRegistry

	// Create repository adapters
	authRepoAdapter := &authRepositoryAdapter{
		repo:   app.repo,
		scopes: app.workspaceDir,
	}
	inventoryRepoAdapter := &inventoryRepositoryAdapter{
		repo:   app.repo,
		scopes: app.workspaceDir,
	}
	app.authRepo = authRepoAdapter

	svc := users.New(users.Config{
		AuthRepository:       authRepoAdapter,
		InventoryRepository:  inventoryRepoAdapter,
		ActivityRepository:   activityRepo,
		ActivitySink:         activityRepo,
		ProfileRepository:    profileRepo,
		PreferenceRepository: preferenceRepo,
		RoleRegistry:         roleRegistry,
		ScopeResolver:        app.workspaceDir.Resolver(),
		AuthorizationPolicy:  app.workspaceDir.Policy(),
		Hooks: types.Hooks{
			AfterLifecycle: func(_ context.Context, event types.LifecycleEvent) {
				app.GetLogger("hooks").Info("lifecycle transition",
					"from", event.FromState,
					"to", event.ToState,
					"user_id", event.UserID)
			},
		},
		Logger: &loggerAdapter{app.GetLogger("users")},
	})

	if err := svc.HealthCheck(ctx); err != nil {
		return err
	}

	app.SetUserService(svc)

	// TODO: Set up validation listeners if needed
	// See pkg/telemetry/validation for implementation

	return nil
}

// loggerAdapter adapts glog.Logger to types.Logger
type loggerAdapter struct {
	l glog.Logger
}

func (a *loggerAdapter) Debug(msg string, args ...any) {
	a.l.Debug(msg, args...)
}

func (a *loggerAdapter) Info(msg string, args ...any) {
	a.l.Info(msg, args...)
}

func (a *loggerAdapter) Warn(msg string, args ...any) {
	a.l.Warn(msg, args...)
}

func (a *loggerAdapter) Error(msg string, err error, args ...any) {
	if err != nil {
		args = append([]any{"error", err}, args...)
	}
	a.l.Error(msg, args...)
}

// userTrackerAdapter adapts auth.Users to auth.UserTracker interface
type userTrackerAdapter struct {
	users auth.Users
}

func (u *userTrackerAdapter) GetByIdentifier(ctx context.Context, identifier string) (*auth.User, error) {
	return u.users.GetByIdentifier(ctx, identifier)
}

func (u *userTrackerAdapter) TrackAttemptedLogin(ctx context.Context, user *auth.User) error {
	return u.users.TrackAttemptedLogin(ctx, user)
}

func (u *userTrackerAdapter) TrackSucccessfulLogin(ctx context.Context, user *auth.User) error {
	return u.users.TrackSucccessfulLogin(ctx, user)
}

// authRepositoryAdapter adapts auth.RepositoryManager to types.AuthRepository
type authRepositoryAdapter struct {
	repo   auth.RepositoryManager
	scopes *workspaceDirectory
}

func (a *authRepositoryAdapter) GetByID(ctx context.Context, id uuid.UUID) (*types.AuthUser, error) {
	user, err := a.repo.Users().GetByID(ctx, id.String())
	if err != nil {
		return nil, err
	}
	a.bindScope(user)
	return goauth.UserToDomain(user), nil
}

func (a *authRepositoryAdapter) GetByIdentifier(ctx context.Context, identifier string) (*types.AuthUser, error) {
	user, err := a.repo.Users().GetByIdentifier(ctx, identifier)
	if err != nil {
		return nil, err
	}
	a.bindScope(user)
	return goauth.UserToDomain(user), nil
}

func (a *authRepositoryAdapter) Create(ctx context.Context, input *types.AuthUser) (*types.AuthUser, error) {
	a.ensureScopeMetadata(input)
	created, err := a.repo.Users().Create(ctx, goauth.UserFromDomain(input))
	if err != nil {
		return nil, err
	}
	a.bindScope(created)
	return goauth.UserToDomain(created), nil
}

func (a *authRepositoryAdapter) Update(ctx context.Context, input *types.AuthUser) (*types.AuthUser, error) {
	a.ensureScopeMetadata(input)
	updated, err := a.repo.Users().Update(ctx, goauth.UserFromDomain(input))
	if err != nil {
		return nil, err
	}
	a.bindScope(updated)
	return goauth.UserToDomain(updated), nil
}

func (a *authRepositoryAdapter) UpdateStatus(ctx context.Context, actor types.ActorRef, id uuid.UUID, next types.LifecycleState, opts ...types.TransitionOption) (*types.AuthUser, error) {
	// For now, just update the status directly
	user, err := a.repo.Users().GetByID(ctx, id.String())
	if err != nil {
		return nil, err
	}
	user.Status = auth.UserStatus(string(next))
	updated, err := a.repo.Users().Update(ctx, user)
	if err != nil {
		return nil, err
	}
	a.bindScope(updated)
	return goauth.UserToDomain(updated), nil
}

func (a *authRepositoryAdapter) AllowedTransitions(ctx context.Context, id uuid.UUID) ([]types.LifecycleTransition, error) {
	user, err := a.repo.Users().GetByID(ctx, id.String())
	if err != nil {
		return nil, err
	}
	// Return basic transitions based on current status
	current := types.LifecycleState(user.Status)
	var transitions []types.LifecycleTransition

	switch current {
	case "active":
		transitions = []types.LifecycleTransition{
			{From: current, To: "suspended"},
			{From: current, To: "deactivated"},
		}
	case "suspended":
		transitions = []types.LifecycleTransition{
			{From: current, To: "active"},
			{From: current, To: "deactivated"},
		}
	case "deactivated":
		transitions = []types.LifecycleTransition{
			{From: current, To: "active"},
		}
	}

	return transitions, nil
}

func (a *authRepositoryAdapter) ResetPassword(ctx context.Context, id uuid.UUID, passwordHash string) error {
	user, err := a.repo.Users().GetByID(ctx, id.String())
	if err != nil {
		return err
	}
	user.PasswordHash = passwordHash
	_, err = a.repo.Users().Update(ctx, user)
	a.bindScope(user)
	return err
}

// inventoryRepositoryAdapter adapts auth.RepositoryManager to types.UserInventoryRepository
type inventoryRepositoryAdapter struct {
	repo   auth.RepositoryManager
	scopes *workspaceDirectory
}

func (i *inventoryRepositoryAdapter) ListUsers(ctx context.Context, filter types.UserInventoryFilter) (types.UserInventoryPage, error) {
	// For now, return a simple list
	users, _, err := i.repo.Users().List(ctx)
	if err != nil {
		return types.UserInventoryPage{}, err
	}

	var inventoryUsers []types.AuthUser
	tenantFilter := filter.Scope.TenantID
	workspaceFilter := filter.Scope.Label(workspaceScopeLabel)
	for _, user := range users {
		tenantID := metadataUUID(user.Metadata, tenantMetadataKey)
		workspaceID := metadataUUID(user.Metadata, workspaceMetadataKey)
		if tenantID == uuid.Nil && i.scopes != nil {
			tenantID = i.scopes.Scope(user.ID).TenantID
		}
		if workspaceID == uuid.Nil && i.scopes != nil {
			workspaceID = i.scopes.Scope(user.ID).Label(workspaceScopeLabel)
		}
		if tenantFilter != uuid.Nil && tenantID != tenantFilter {
			continue
		}
		if workspaceFilter != uuid.Nil && workspaceID != workspaceFilter {
			continue
		}
		inventoryUsers = append(inventoryUsers, types.AuthUser{
			ID:        user.ID,
			Email:     user.Email,
			Username:  user.Username,
			FirstName: user.FirstName,
			LastName:  user.LastName,
			Status:    types.LifecycleState(user.Status),
			Role:      string(user.Role),
			Metadata:  user.Metadata,
			CreatedAt: user.CreatedAt,
			UpdatedAt: user.UpdatedAt,
		})
		if i.scopes != nil {
			i.scopes.Ensure(user.ID, tenantID, workspaceID)
		}
	}

	return types.UserInventoryPage{
		Users: inventoryUsers,
		Total: len(inventoryUsers),
	}, nil
}

func WaitExitSignal() os.Signal {
	ch := make(chan os.Signal, 3)
	signal.Notify(ch,
		syscall.SIGINT,
		syscall.SIGQUIT,
		syscall.SIGTERM,
	)
	return <-ch
}

func (a *authRepositoryAdapter) bindScope(user *auth.User) {
	if a.scopes == nil || user == nil {
		return
	}
	tenantID := metadataUUID(user.Metadata, tenantMetadataKey)
	workspaceID := metadataUUID(user.Metadata, workspaceMetadataKey)
	a.scopes.Ensure(user.ID, tenantID, workspaceID)
}

func (a *authRepositoryAdapter) ensureScopeMetadata(user *types.AuthUser) {
	if a.scopes == nil || user == nil {
		return
	}
	if user.Metadata == nil {
		user.Metadata = make(map[string]any)
	}
	scope := a.scopes.Scope(user.ID)
	if metadataUUID(user.Metadata, tenantMetadataKey) == uuid.Nil && scope.TenantID != uuid.Nil {
		user.Metadata[tenantMetadataKey] = scope.TenantID.String()
	}
	if metadataUUID(user.Metadata, workspaceMetadataKey) == uuid.Nil && scope.Label(workspaceScopeLabel) != uuid.Nil {
		user.Metadata[workspaceMetadataKey] = scope.Label(workspaceScopeLabel).String()
	}
}

func metadataUUID(meta map[string]any, key string) uuid.UUID {
	if len(meta) == 0 {
		return uuid.Nil
	}
	value, ok := meta[key]
	if !ok {
		return uuid.Nil
	}
	switch v := value.(type) {
	case string:
		id, err := uuid.Parse(v)
		if err == nil {
			return id
		}
	case uuid.UUID:
		return v
	}
	return uuid.Nil
}
