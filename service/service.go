package service

import (
	"context"
	"time"

	featuregate "github.com/goliatone/go-featuregate/gate"
	"github.com/goliatone/go-users/activity"
	"github.com/goliatone/go-users/command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/preferences"
	"github.com/goliatone/go-users/query"
	"github.com/goliatone/go-users/scope"
)

// Service is the entry point for go-users. It wires repositories, registries,
// hooks, and command/query facades supplied by the host application.
type Service struct {
	cfg            Config
	commands       Commands
	queries        Queries
	inventoryRepo  types.UserInventoryRepository
	activityRepo   types.ActivityRepository
	profileRepo    types.ProfileRepository
	preferenceRepo types.PreferenceRepository
	prefResolver   PreferenceResolver
	scopeGuard     scope.Guard
}

// Commands exposes the service command handlers.
type Commands struct {
	UserLifecycleTransition  *command.UserLifecycleTransitionCommand
	BulkUserTransition       *command.BulkUserTransitionCommand
	BulkUserImport           *command.BulkUserImportCommand
	UserCreate               *command.UserCreateCommand
	UserBootstrapPassword    *command.UserBootstrapPasswordCommand
	UserUpdate               *command.UserUpdateCommand
	UserInvite               *command.UserInviteCommand
	UserRegistrationRequest  *command.UserRegistrationRequestCommand
	UserTokenValidate        *command.UserTokenValidateCommand
	UserTokenConsume         *command.UserTokenConsumeCommand
	UserPasswordResetRequest *command.UserPasswordResetRequestCommand
	UserPasswordResetConfirm *command.UserPasswordResetConfirmCommand
	UserPasswordReset        *command.UserPasswordResetCommand
	CreateRole               *command.CreateRoleCommand
	UpdateRole               *command.UpdateRoleCommand
	DeleteRole               *command.DeleteRoleCommand
	AssignRole               *command.AssignRoleCommand
	UnassignRole             *command.UnassignRoleCommand
	LogActivity              *command.ActivityLogCommand
	ProfileUpsert            *command.ProfileUpsertCommand
	PreferenceUpsert         *command.PreferenceUpsertCommand
	PreferenceDelete         *command.PreferenceDeleteCommand
	PreferenceUpsertMany     *command.PreferenceUpsertManyCommand
	PreferenceDeleteMany     *command.PreferenceDeleteManyCommand
}

// Queries exposes read-model helpers.
type Queries struct {
	UserInventory   *query.UserInventoryQuery
	RoleList        *query.RoleListQuery
	RoleDetail      *query.RoleDetailQuery
	RoleAssignments *query.RoleAssignmentsQuery
	ActivityFeed    *query.ActivityFeedQuery
	ActivityStats   *query.ActivityStatsQuery
	ProfileDetail   *query.ProfileQuery
	Preferences     *query.PreferenceQuery
}

// Config captures all required dependencies so callers can provide their own
// instances (bun.DB, cached repositories, hooks, etc.).
type Config struct {
	AuthRepository                  types.AuthRepository
	InventoryRepository             types.UserInventoryRepository
	ActivityRepository              types.ActivityRepository
	RoleRegistry                    types.RoleRegistry
	ActivitySink                    types.ActivitySink
	ActivityEnricher                activity.ActivityEnricher
	ActivityEnrichmentStore         activity.ActivityEnrichmentStore
	ActivityEnrichmentErrorStrategy activity.EnrichmentErrorStrategy
	ActivityEnrichmentErrorHandler  activity.EnrichmentErrorHandler
	EnrichmentScope                 activity.EnrichmentScope
	EnrichmentWriteMode             activity.EnrichmentWriteMode
	EnrichmentEnabled               bool
	EnrichmentJobSchedule           string
	SessionIDKey                    string
	Hooks                           types.Hooks
	Clock                           types.Clock
	IDGenerator                     types.IDGenerator
	Logger                          types.Logger
	TransitionPolicy                types.TransitionPolicy
	InviteTokenTTL                  time.Duration
	SecureLinkManager               types.SecureLinkManager
	UserTokenRepository             types.UserTokenRepository
	PasswordResetRepository         types.PasswordResetRepository
	InviteLinkRoute                 string
	RegistrationLinkRoute           string
	PasswordResetLinkRoute          string
	TokenScopeEnforcer              types.ScopeEnforcer
	ProfileRepository               types.ProfileRepository
	PreferenceRepository            types.PreferenceRepository
	PreferenceResolver              PreferenceResolver
	ScopeResolver                   types.ScopeResolver
	AuthorizationPolicy             types.AuthorizationPolicy
	FeatureGate                     featuregate.FeatureGate

	// RequireScopeAuthorization enables strict guard configuration and readiness.
	RequireScopeAuthorization bool
	RoleGrantValidator        types.RoleGrantValidator
	// RequireRoleGrantValidation enables mandatory candidate-grant validation.
	RequireRoleGrantValidation bool
}

// PreferenceResolver resolves scoped preferences for queries.
type PreferenceResolver interface {
	Resolve(ctx context.Context, input preferences.ResolveInput) (types.PreferenceSnapshot, error)
}

// New constructs a Service from the supplied configuration.
func New(cfg Config) *Service {
	norm := normalizeConfig(cfg)
	invRepo := norm.InventoryRepository
	if invRepo == nil {
		if cast, ok := norm.AuthRepository.(types.UserInventoryRepository); ok {
			invRepo = cast
		}
	}
	actRepo := norm.ActivityRepository
	if actRepo == nil {
		if sinkRepo, ok := norm.ActivitySink.(types.ActivityRepository); ok {
			actRepo = sinkRepo
		}
	}
	prefResolver := norm.PreferenceResolver
	if prefResolver == nil && norm.PreferenceRepository != nil {
		if resolver, err := preferences.NewResolver(preferences.ResolverConfig{
			Repository: norm.PreferenceRepository,
		}); err == nil {
			prefResolver = resolver
		} else if norm.Logger != nil {
			norm.Logger.Error("go-users: preference resolver initialization failed", err)
		}
	}

	scopeGuard := scope.NewGuard(norm.ScopeResolver, norm.AuthorizationPolicy)
	if norm.RequireScopeAuthorization {
		// An invalid StrictGuard denies enforcement. Ready/HealthCheck expose its
		// validation error even when callers use this compatibility constructor.
		strict, _ := scope.NewStrictGuard(norm.ScopeResolver, norm.AuthorizationPolicy)
		scopeGuard = strict
	}

	s := &Service{
		cfg:            norm,
		inventoryRepo:  invRepo,
		activityRepo:   actRepo,
		profileRepo:    norm.ProfileRepository,
		preferenceRepo: norm.PreferenceRepository,
		prefResolver:   prefResolver,
		scopeGuard:     scopeGuard,
	}
	s.commands = s.buildCommands()
	s.queries = s.buildQueries()
	return s
}

// NewStrict validates scope authorization at startup. Other dependencies retain
// their existing Ready/HealthCheck checks and may support partial service use.
func NewStrict(cfg Config) (*Service, error) {
	cfg.RequireScopeAuthorization = true
	s := New(cfg)
	if err := scope.ValidateStrict(s.scopeGuard); err != nil {
		return nil, err
	}
	if err := s.roleAssignmentConfig().Validate(); err != nil {
		return nil, err
	}
	return s, nil
}

func normalizeConfig(cfg Config) Config {
	if cfg.Clock == nil {
		cfg.Clock = types.SystemClock{}
	}
	if cfg.IDGenerator == nil {
		cfg.IDGenerator = types.UUIDGenerator{}
	}
	if cfg.Logger == nil {
		cfg.Logger = types.NopLogger{}
	}
	if cfg.TransitionPolicy == nil {
		cfg.TransitionPolicy = types.DefaultTransitionPolicy()
	}
	return cfg
}

// Commands returns the command facade.
func (s *Service) Commands() Commands {
	return s.commands
}

// Queries returns the query facade.
func (s *Service) Queries() Queries {
	return s.queries
}

// Ready reports whether the service has the required dependencies wired in.
func (s *Service) Ready() bool {
	return s != nil &&
		s.roleAssignmentConfig().Validate() == nil &&
		(!s.cfg.RequireScopeAuthorization || scope.ValidateStrict(s.scopeGuard) == nil) &&
		s.cfg.AuthRepository != nil &&
		s.cfg.RoleRegistry != nil &&
		s.cfg.ActivitySink != nil &&
		s.inventoryRepo != nil &&
		s.activityRepo != nil &&
		s.profileRepo != nil &&
		s.preferenceRepo != nil &&
		s.prefResolver != nil
}

// HealthCheck exercises the registered dependencies to ensure the service can
// be used by upstream transports (REST/gRPC/jobs). Future implementations will
// ping the repositories/hooks; for now we just surface missing config.
func (s *Service) HealthCheck(ctx context.Context) error {
	if s != nil {
		if err := s.roleAssignmentConfig().Validate(); err != nil {
			return err
		}
	}
	if s != nil && s.cfg.RequireScopeAuthorization {
		if err := scope.ValidateStrict(s.scopeGuard); err != nil {
			return err
		}
	}
	if !s.Ready() {
		return types.ErrServiceNotReady
	}
	if s.cfg.AuthRepository == nil {
		return types.ErrMissingAuthRepository
	}
	if s.cfg.RoleRegistry == nil {
		return types.ErrMissingRoleRegistry
	}
	if s.cfg.ActivitySink == nil {
		return types.ErrMissingActivitySink
	}
	if s.inventoryRepo == nil {
		return types.ErrMissingInventoryRepository
	}
	if s.activityRepo == nil {
		return types.ErrMissingActivityRepository
	}
	if s.profileRepo == nil {
		return types.ErrMissingProfileRepository
	}
	if s.preferenceRepo == nil {
		return types.ErrMissingPreferenceRepository
	}
	if s.prefResolver == nil {
		return types.ErrMissingPreferenceResolver
	}
	return nil
}

// ScopeGuard exposes the guard instance used internally so transports can reuse
// the same resolver/policy combination for HTTP adapters.
func (s *Service) ScopeGuard() scope.Guard {
	if s == nil {
		return scope.NopGuard()
	}
	return scope.Ensure(s.scopeGuard)
}

// ActivitySink returns the configured sink so transports can emit activity
// records for auxiliary workflows (e.g. CRUD controllers).
func (s *Service) ActivitySink() types.ActivitySink {
	if s == nil {
		return nil
	}
	return s.cfg.ActivitySink
}

func (s *Service) buildCommands() Commands {
	lifecycle := s.newLifecycleCommand()
	userCreate := s.newUserCreateCommand()
	userPasswordReset := s.newPasswordResetCommand()
	cmds := Commands{
		UserLifecycleTransition: lifecycle,
		BulkUserTransition:      command.NewBulkUserTransitionCommand(lifecycle),
		BulkUserImport:          command.NewBulkUserImportCommand(userCreate),
		UserCreate:              userCreate,
		UserBootstrapPassword: command.NewUserBootstrapPasswordCommand(command.BootstrapPasswordCommandConfig{
			Repository: s.cfg.AuthRepository,
			Create:     userCreate,
			Reset:      userPasswordReset,
			Clock:      s.cfg.Clock,
		}),
		UserUpdate: command.NewUserUpdateCommand(command.UserUpdateCommandConfig{
			Repository: s.cfg.AuthRepository,
			Policy:     s.cfg.TransitionPolicy,
			Clock:      s.cfg.Clock,
			Activity:   s.cfg.ActivitySink,
			Hooks:      s.cfg.Hooks,
			Logger:     s.cfg.Logger,
			ScopeGuard: s.scopeGuard,
		}),
		UserPasswordReset: userPasswordReset,
	}
	s.attachSecureLinkCommands(&cmds, userPasswordReset)
	s.attachRoleCommands(&cmds)
	s.attachActivityProfilePreferenceCommands(&cmds)
	return cmds
}

func (s *Service) newLifecycleCommand() *command.UserLifecycleTransitionCommand {
	return command.NewUserLifecycleTransitionCommand(command.LifecycleCommandConfig{
		Repository: s.cfg.AuthRepository,
		Policy:     s.cfg.TransitionPolicy,
		Clock:      s.cfg.Clock,
		Logger:     s.cfg.Logger,
		Hooks:      s.cfg.Hooks,
		Activity:   s.cfg.ActivitySink,
		ScopeGuard: s.scopeGuard,
	})
}

func (s *Service) newUserCreateCommand() *command.UserCreateCommand {
	return command.NewUserCreateCommand(command.UserCreateCommandConfig{
		Repository: s.cfg.AuthRepository,
		Clock:      s.cfg.Clock,
		Activity:   s.cfg.ActivitySink,
		Hooks:      s.cfg.Hooks,
		Logger:     s.cfg.Logger,
		ScopeGuard: s.scopeGuard,
	})
}

func (s *Service) newPasswordResetCommand() *command.UserPasswordResetCommand {
	return command.NewUserPasswordResetCommand(command.PasswordResetCommandConfig{
		Repository: s.cfg.AuthRepository,
		Clock:      s.cfg.Clock,
		Activity:   s.cfg.ActivitySink,
		Hooks:      s.cfg.Hooks,
		Logger:     s.cfg.Logger,
		ScopeGuard: s.scopeGuard,
	})
}

func (s *Service) attachSecureLinkCommands(cmds *Commands, reset *command.UserPasswordResetCommand) {
	cmds.UserInvite = command.NewUserInviteCommand(command.InviteCommandConfig{
		Repository:      s.cfg.AuthRepository,
		TokenRepository: s.cfg.UserTokenRepository,
		SecureLinks:     s.cfg.SecureLinkManager,
		Clock:           s.cfg.Clock,
		IDGen:           s.cfg.IDGenerator,
		Activity:        s.cfg.ActivitySink,
		Hooks:           s.cfg.Hooks,
		Logger:          s.cfg.Logger,
		TokenTTL:        s.cfg.InviteTokenTTL,
		ScopeGuard:      s.scopeGuard,
		FeatureGate:     s.cfg.FeatureGate,
		Route:           s.cfg.InviteLinkRoute,
	})
	cmds.UserRegistrationRequest = command.NewUserRegistrationRequestCommand(command.RegistrationRequestConfig{
		Repository:      s.cfg.AuthRepository,
		TokenRepository: s.cfg.UserTokenRepository,
		SecureLinks:     s.cfg.SecureLinkManager,
		Clock:           s.cfg.Clock,
		IDGen:           s.cfg.IDGenerator,
		Activity:        s.cfg.ActivitySink,
		Hooks:           s.cfg.Hooks,
		Logger:          s.cfg.Logger,
		TokenTTL:        s.cfg.InviteTokenTTL,
		ScopeGuard:      s.scopeGuard,
		FeatureGate:     s.cfg.FeatureGate,
		Route:           s.cfg.RegistrationLinkRoute,
	})
	cmds.UserTokenValidate = command.NewUserTokenValidateCommand(command.TokenValidateConfig{
		TokenRepository: s.cfg.UserTokenRepository,
		SecureLinks:     s.cfg.SecureLinkManager,
		Clock:           s.cfg.Clock,
		ScopeEnforcer:   s.cfg.TokenScopeEnforcer,
	})
	cmds.UserTokenConsume = command.NewUserTokenConsumeCommand(command.TokenConsumeConfig{
		Logger:          s.cfg.Logger,
		TokenRepository: s.cfg.UserTokenRepository,
		SecureLinks:     s.cfg.SecureLinkManager,
		Clock:           s.cfg.Clock,
		ScopeEnforcer:   s.cfg.TokenScopeEnforcer,
		Activity:        s.cfg.ActivitySink,
		Hooks:           s.cfg.Hooks,
	})
	cmds.UserPasswordResetRequest = command.NewUserPasswordResetRequestCommand(command.PasswordResetRequestConfig{
		Repository:      s.cfg.AuthRepository,
		ResetRepository: s.cfg.PasswordResetRepository,
		SecureLinks:     s.cfg.SecureLinkManager,
		Clock:           s.cfg.Clock,
		IDGen:           s.cfg.IDGenerator,
		Activity:        s.cfg.ActivitySink,
		Hooks:           s.cfg.Hooks,
		Logger:          s.cfg.Logger,
		FeatureGate:     s.cfg.FeatureGate,
		Route:           s.cfg.PasswordResetLinkRoute,
	})
	cmds.UserPasswordResetConfirm = command.NewUserPasswordResetConfirmCommand(command.PasswordResetConfirmConfig{
		ResetRepository: s.cfg.PasswordResetRepository,
		SecureLinks:     s.cfg.SecureLinkManager,
		ResetCommand:    reset,
		Clock:           s.cfg.Clock,
		ScopeEnforcer:   s.cfg.TokenScopeEnforcer,
		Logger:          s.cfg.Logger,
	})
}

func (s *Service) attachRoleCommands(cmds *Commands) {
	cmds.CreateRole = command.NewCreateRoleCommand(s.cfg.RoleRegistry, s.scopeGuard)
	cmds.UpdateRole = command.NewUpdateRoleCommand(s.cfg.RoleRegistry, s.scopeGuard)
	cmds.DeleteRole = command.NewDeleteRoleCommand(s.cfg.RoleRegistry, s.scopeGuard)
	cmds.AssignRole = command.NewAssignRoleCommandWithConfig(s.roleAssignmentConfig())
	cmds.UnassignRole = command.NewUnassignRoleCommandWithConfig(s.roleAssignmentConfig())
}

func (s *Service) roleAssignmentConfig() command.RoleAssignmentCommandConfig {
	return command.RoleAssignmentCommandConfig{
		Registry: s.cfg.RoleRegistry, ScopeGuard: s.scopeGuard,
		GrantValidator: s.cfg.RoleGrantValidator, RequireGrantValidation: s.cfg.RequireRoleGrantValidation,
	}
}

func (s *Service) attachActivityProfilePreferenceCommands(cmds *Commands) {
	prefCfg := command.PreferenceCommandConfig{
		Repository: s.cfg.PreferenceRepository,
		Hooks:      s.cfg.Hooks,
		Clock:      s.cfg.Clock,
		ScopeGuard: s.scopeGuard,
	}
	cmds.LogActivity = command.NewActivityLogCommand(command.ActivityLogConfig{
		Sink:  s.cfg.ActivitySink,
		Hooks: s.cfg.Hooks,
		Clock: s.cfg.Clock,
	})
	cmds.ProfileUpsert = command.NewProfileUpsertCommand(command.ProfileCommandConfig{
		Repository: s.cfg.ProfileRepository,
		Hooks:      s.cfg.Hooks,
		Clock:      s.cfg.Clock,
		ScopeGuard: s.scopeGuard,
	})
	cmds.PreferenceUpsert = command.NewPreferenceUpsertCommand(prefCfg)
	cmds.PreferenceDelete = command.NewPreferenceDeleteCommand(prefCfg)
	cmds.PreferenceUpsertMany = command.NewPreferenceUpsertManyCommand(prefCfg)
	cmds.PreferenceDeleteMany = command.NewPreferenceDeleteManyCommand(prefCfg)
}

func (s *Service) buildQueries() Queries {
	return Queries{
		UserInventory:   query.NewUserInventoryQuery(s.inventoryRepo, s.cfg.Logger, s.scopeGuard),
		RoleList:        query.NewRoleListQuery(s.cfg.RoleRegistry, s.scopeGuard),
		RoleDetail:      query.NewRoleDetailQuery(s.cfg.RoleRegistry, s.scopeGuard),
		RoleAssignments: query.NewRoleAssignmentsQuery(s.cfg.RoleRegistry, s.scopeGuard),
		ActivityFeed:    query.NewActivityFeedQuery(s.activityRepo, s.scopeGuard),
		ActivityStats:   query.NewActivityStatsQuery(s.activityRepo, s.scopeGuard),
		ProfileDetail:   query.NewProfileQuery(s.profileRepo, s.scopeGuard),
		Preferences:     query.NewPreferenceQuery(s.prefResolver, s.scopeGuard),
	}
}
