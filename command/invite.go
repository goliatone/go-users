package command

import (
	"context"
	"maps"
	"strings"
	"time"

	gocommand "github.com/goliatone/go-command"
	featuregate "github.com/goliatone/go-featuregate/gate"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

const defaultInviteTTL = 72 * time.Hour

// UserInviteInput carries the data required to invite a new user.
type UserInviteInput struct {
	Email     string
	Username  string
	FirstName string
	LastName  string
	Role      string
	Metadata  map[string]any
	Actor     types.ActorRef
	Scope     types.ScopeFilter
	Result    *UserInviteResult
}

// Type implements gocommand.Message.
func (UserInviteInput) Type() string {
	return "command.user.invite"
}

// Validate implements gocommand.Message.
func (input UserInviteInput) Validate() error {
	switch {
	case strings.TrimSpace(input.Email) == "":
		return ErrInviteEmailRequired
	case input.Actor.ID == uuid.Nil:
		return ErrActorRequired
	default:
		return nil
	}
}

// UserInviteResult exposes the creation output and invite token details.
type UserInviteResult struct {
	User      *types.AuthUser
	Token     string
	ExpiresAt time.Time
}

// UserInviteCommand creates pending users and records invite metadata.
type UserInviteCommand struct {
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

// InviteCommandConfig holds dependencies for the invite flow.
type InviteCommandConfig struct {
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

// NewUserInviteCommand constructs the invite handler.
func NewUserInviteCommand(cfg InviteCommandConfig) *UserInviteCommand {
	runtime := newSecureLinkRuntime(secureLinkRuntimeConfig{
		SecureLinks:  cfg.SecureLinks,
		Clock:        cfg.Clock,
		IDGen:        cfg.IDGen,
		Activity:     cfg.Activity,
		Hooks:        cfg.Hooks,
		Logger:       cfg.Logger,
		TokenTTL:     cfg.TokenTTL,
		DefaultTTL:   defaultInviteTTL,
		ScopeGuard:   cfg.ScopeGuard,
		Route:        cfg.Route,
		DefaultRoute: SecureLinkRouteInviteAccept,
	})
	cmd := &UserInviteCommand{
		repo:        cfg.Repository,
		tokens:      cfg.TokenRepository,
		featureGate: cfg.FeatureGate,
	}
	cmd.applyRuntime(runtime)
	return cmd
}

var _ gocommand.Commander[UserInviteInput] = (*UserInviteCommand)(nil)

// Execute creates the pending user record and registers invite metadata.
func (c *UserInviteCommand) Execute(ctx context.Context, input UserInviteInput) error {
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
	scope, err := c.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionUsersWrite, uuid.Nil)
	if err != nil {
		return err
	}
	if enabled, featureErr := featureEnabled(ctx, c.featureGate, featureUsersInvite, scope, uuid.Nil); featureErr != nil {
		return featureErr
	} else if !enabled {
		return ErrInviteDisabled
	}

	created, err := c.createInviteUser(ctx, input)
	if err != nil {
		return err
	}

	token, jti, issuedAt, expiresAt, err := c.issueInviteLink(created, scope)
	if err != nil {
		return err
	}
	if tokenErr := c.recordInviteToken(ctx, created.ID, jti, issuedAt, expiresAt); tokenErr != nil {
		return tokenErr
	}

	created, err = c.updateInviteMetadata(ctx, created, input.Actor, scope, jti, issuedAt, expiresAt)
	if err != nil {
		return err
	}
	c.emitInviteActivity(ctx, created, input.Actor, scope, jti, issuedAt, expiresAt)
	setInviteResult(input.Result, created, token, expiresAt)
	return nil
}

func (c *UserInviteCommand) createInviteUser(ctx context.Context, input UserInviteInput) (*types.AuthUser, error) {
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

func (c *UserInviteCommand) applyRuntime(runtime secureLinkRuntime) {
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

func (c *UserInviteCommand) issueInviteLink(user *types.AuthUser, scope types.ScopeFilter) (string, string, time.Time, time.Time, error) {
	issuedAt := now(c.clock)
	expiresAt := issuedAt.Add(c.tokenTTL)
	jti := c.idGen.UUID().String()
	payload := buildSecureLinkPayload(SecureLinkActionInvite, user, scope, jti, issuedAt, expiresAt, secureLinkSourceDefault)
	token, err := c.manager.Generate(c.route, payload)
	return token, jti, issuedAt, expiresAt, err
}

func (c *UserInviteCommand) recordInviteToken(ctx context.Context, userID uuid.UUID, jti string, issuedAt, expiresAt time.Time) error {
	_, err := c.tokens.CreateToken(ctx, types.UserToken{
		UserID:    userID,
		Type:      types.UserTokenInvite,
		JTI:       jti,
		Status:    types.UserTokenStatusIssued,
		IssuedAt:  issuedAt,
		ExpiresAt: expiresAt,
	})
	return err
}

func (c *UserInviteCommand) updateInviteMetadata(ctx context.Context, user *types.AuthUser, actor types.ActorRef, scope types.ScopeFilter, jti string, issuedAt, expiresAt time.Time) (*types.AuthUser, error) {
	attachTokenMetadata(user, "invite", tokenMetadata(jti, issuedAt, expiresAt, actor, scope))
	updated, err := c.repo.Update(ctx, user)
	if err != nil {
		return nil, err
	}
	if updated != nil {
		return updated, nil
	}
	return user, nil
}

func (c *UserInviteCommand) emitInviteActivity(ctx context.Context, user *types.AuthUser, actor types.ActorRef, scope types.ScopeFilter, jti string, issuedAt, expiresAt time.Time) {
	record := types.ActivityRecord{
		UserID:     user.ID,
		ActorID:    actor.ID,
		Verb:       "user.invite",
		ObjectType: "user",
		ObjectID:   user.ID.String(),
		Channel:    "invites",
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

func setInviteResult(result *UserInviteResult, user *types.AuthUser, token string, expiresAt time.Time) {
	if result != nil {
		*result = UserInviteResult{
			User:      user,
			Token:     token,
			ExpiresAt: expiresAt,
		}
	}
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(src))
	maps.Copy(out, src)
	return out
}
