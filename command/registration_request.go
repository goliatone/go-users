package command

import (
	"context"
	"strings"
	"time"

	gocommand "github.com/goliatone/go-command"
	featuregate "github.com/goliatone/go-featuregate/gate"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

const defaultRegistrationTTL = 72 * time.Hour

// UserRegistrationRequestInput carries the data required to request self-registration.
type UserRegistrationRequestInput struct {
	Email     string
	Username  string
	FirstName string
	LastName  string
	Role      string
	Metadata  map[string]any
	Actor     types.ActorRef
	Scope     types.ScopeFilter
	Result    *UserRegistrationRequestResult
}

// Type implements gocommand.Message.
func (UserRegistrationRequestInput) Type() string {
	return "command.user.registration.request"
}

// Validate implements gocommand.Message.
func (input UserRegistrationRequestInput) Validate() error {
	switch {
	case strings.TrimSpace(input.Email) == "":
		return ErrInviteEmailRequired
	default:
		return nil
	}
}

// UserRegistrationRequestResult exposes the creation output and registration token details.
type UserRegistrationRequestResult struct {
	User      *types.AuthUser
	Token     string
	ExpiresAt time.Time
}

// UserRegistrationRequestCommand creates pending users and records registration metadata.
type UserRegistrationRequestCommand struct {
	repo        types.AuthRepository
	tokens      types.UserTokenRepository
	manager     types.SecureLinkManager
	clock       types.Clock
	idGen       types.IDGenerator
	sink        types.ActivitySink
	hooks       types.Hooks
	logger      types.Logger
	tokenTTL    time.Duration
	guard       scope.Guard
	featureGate featuregate.FeatureGate
	route       string
}

// RegistrationRequestConfig holds dependencies for the registration flow.
type RegistrationRequestConfig struct {
	Repository      types.AuthRepository
	TokenRepository types.UserTokenRepository
	SecureLinks     types.SecureLinkManager
	Clock           types.Clock
	IDGen           types.IDGenerator
	Activity        types.ActivitySink
	Hooks           types.Hooks
	Logger          types.Logger
	TokenTTL        time.Duration
	ScopeGuard      scope.Guard
	FeatureGate     featuregate.FeatureGate
	Route           string
}

// NewUserRegistrationRequestCommand constructs the registration handler.
func NewUserRegistrationRequestCommand(cfg RegistrationRequestConfig) *UserRegistrationRequestCommand {
	runtime := newSecureLinkRuntime(secureLinkRuntimeConfig{
		SecureLinks:  cfg.SecureLinks,
		Clock:        cfg.Clock,
		IDGen:        cfg.IDGen,
		Activity:     cfg.Activity,
		Hooks:        cfg.Hooks,
		Logger:       cfg.Logger,
		TokenTTL:     cfg.TokenTTL,
		DefaultTTL:   defaultRegistrationTTL,
		ScopeGuard:   cfg.ScopeGuard,
		Route:        cfg.Route,
		DefaultRoute: SecureLinkRouteRegister,
	})
	cmd := &UserRegistrationRequestCommand{
		repo:        cfg.Repository,
		tokens:      cfg.TokenRepository,
		featureGate: cfg.FeatureGate,
	}
	cmd.applyRuntime(runtime)
	return cmd
}

var _ gocommand.Commander[UserRegistrationRequestInput] = (*UserRegistrationRequestCommand)(nil)

// Execute creates the pending user record and registers registration metadata.
func (c *UserRegistrationRequestCommand) Execute(ctx context.Context, input UserRegistrationRequestInput) error {
	if c.repo == nil {
		return types.ErrMissingAuthRepository
	}
	if c.manager == nil {
		return types.ErrMissingSecureLinkManager
	}
	if c.tokens == nil {
		return types.ErrMissingUserTokenRepository
	}
	if err := input.Validate(); err != nil {
		return err
	}
	scope, err := c.registrationScope(ctx, input)
	if err != nil {
		return err
	}
	if featureErr := c.ensureRegistrationEnabled(ctx, scope); featureErr != nil {
		return featureErr
	}
	created, err := c.createRegistrationUser(ctx, input)
	if err != nil {
		return err
	}
	token, jti, issuedAt, expiresAt, err := c.issueRegistrationLink(created, scope)
	if err != nil {
		return err
	}
	if recordErr := c.recordRegistrationToken(ctx, created.ID, jti, issuedAt, expiresAt); recordErr != nil {
		return recordErr
	}
	created, err = c.updateRegistrationMetadata(ctx, created, input.Actor, scope, jti, issuedAt, expiresAt)
	if err != nil {
		return err
	}
	c.emitRegistrationActivity(ctx, created, input.Actor, scope, jti, issuedAt, expiresAt)
	setRegistrationResult(input.Result, created, token, expiresAt)
	return nil
}

func (c *UserRegistrationRequestCommand) registrationScope(ctx context.Context, input UserRegistrationRequestInput) (types.ScopeFilter, error) {
	if input.Actor.ID == uuid.Nil {
		return input.Scope.Clone(), nil
	}
	return c.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionUsersWrite, uuid.Nil)
}

func (c *UserRegistrationRequestCommand) ensureRegistrationEnabled(ctx context.Context, scope types.ScopeFilter) error {
	enabled, err := featureEnabled(ctx, c.featureGate, featuregate.FeatureUsersSignup, scope, uuid.Nil)
	if err != nil {
		return err
	}
	if !enabled {
		return ErrSignupDisabled
	}
	return nil
}

func (c *UserRegistrationRequestCommand) createRegistrationUser(ctx context.Context, input UserRegistrationRequestInput) (*types.AuthUser, error) {
	return c.repo.Create(ctx, &types.AuthUser{
		Email:     strings.TrimSpace(input.Email),
		Username:  strings.TrimSpace(input.Username),
		FirstName: strings.TrimSpace(input.FirstName),
		LastName:  strings.TrimSpace(input.LastName),
		Role:      input.Role,
		Status:    types.LifecycleStatePending,
		Metadata:  cloneMap(input.Metadata),
	})
}

func (c *UserRegistrationRequestCommand) issueRegistrationLink(user *types.AuthUser, scope types.ScopeFilter) (string, string, time.Time, time.Time, error) {
	issuedAt := now(c.clock)
	expiresAt := issuedAt.Add(c.tokenTTL)
	jti := c.idGen.UUID().String()
	payload := buildSecureLinkPayload(SecureLinkActionRegister, user, scope, jti, issuedAt, expiresAt, secureLinkSourceDefault)
	token, err := c.manager.Generate(c.route, payload)
	return token, jti, issuedAt, expiresAt, err
}

func (c *UserRegistrationRequestCommand) recordRegistrationToken(ctx context.Context, userID uuid.UUID, jti string, issuedAt, expiresAt time.Time) error {
	_, err := c.tokens.CreateToken(ctx, types.UserToken{
		UserID:    userID,
		Type:      types.UserTokenRegistration,
		JTI:       jti,
		Status:    types.UserTokenStatusIssued,
		IssuedAt:  issuedAt,
		ExpiresAt: expiresAt,
	})
	return err
}

func (c *UserRegistrationRequestCommand) updateRegistrationMetadata(ctx context.Context, user *types.AuthUser, actor types.ActorRef, scope types.ScopeFilter, jti string, issuedAt, expiresAt time.Time) (*types.AuthUser, error) {
	attachTokenMetadata(user, "registration", tokenMetadata(jti, issuedAt, expiresAt, actor, scope))
	updated, err := c.repo.Update(ctx, user)
	if err != nil {
		return nil, err
	}
	if updated != nil {
		return updated, nil
	}
	return user, nil
}

func (c *UserRegistrationRequestCommand) emitRegistrationActivity(ctx context.Context, user *types.AuthUser, actor types.ActorRef, scope types.ScopeFilter, jti string, issuedAt, expiresAt time.Time) {
	if actor.ID == uuid.Nil {
		actor = types.ActorRef{ID: user.ID, Type: "user"}
	}
	record := types.ActivityRecord{
		UserID:     user.ID,
		ActorID:    actor.ID,
		Verb:       "user.registration.requested",
		ObjectType: "user",
		ObjectID:   user.ID.String(),
		Channel:    "registration",
		TenantID:   scope.TenantID,
		OrgID:      scope.OrgID,
		Data: map[string]any{
			"email":      user.Email,
			"role":       user.Role,
			"jti":        jti,
			"expires_at": expiresAt,
		},
		OccurredAt: issuedAt,
	}
	logActivity(ctx, c.sink, record)
	emitActivityHook(ctx, c.hooks, record)
}

func setRegistrationResult(result *UserRegistrationRequestResult, user *types.AuthUser, token string, expiresAt time.Time) {
	if result != nil {
		*result = UserRegistrationRequestResult{
			User:      user,
			Token:     token,
			ExpiresAt: expiresAt,
		}
	}
}

func (c *UserRegistrationRequestCommand) applyRuntime(runtime secureLinkRuntime) {
	c.manager = runtime.manager
	c.clock = runtime.clock
	c.idGen = runtime.idGen
	c.sink = runtime.sink
	c.hooks = runtime.hooks
	c.logger = runtime.logger
	c.tokenTTL = runtime.tokenTTL
	c.guard = runtime.guard
	c.route = runtime.route
}
