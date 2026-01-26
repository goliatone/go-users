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
	ttl := cfg.TokenTTL
	if ttl == 0 && cfg.SecureLinks != nil {
		ttl = cfg.SecureLinks.GetExpiration()
	}
	if ttl == 0 {
		ttl = defaultInviteTTL
	}
	idGen := cfg.IDGen
	if idGen == nil {
		idGen = types.UUIDGenerator{}
	}
	route := strings.TrimSpace(cfg.Route)
	if route == "" {
		route = SecureLinkRouteInviteAccept
	}
	return &UserInviteCommand{
		repo:        cfg.Repository,
		tokens:      cfg.TokenRepository,
		manager:     cfg.SecureLinks,
		clock:       safeClock(cfg.Clock),
		idGen:       idGen,
		sink:        safeActivitySink(cfg.Activity),
		hooks:       safeHooks(cfg.Hooks),
		logger:      safeLogger(cfg.Logger),
		tokenTTL:    ttl,
		guard:       safeScopeGuard(cfg.ScopeGuard),
		featureGate: cfg.FeatureGate,
		route:       route,
	}
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
	if enabled, err := featureEnabled(ctx, c.featureGate, featureUsersInvite, scope, uuid.Nil); err != nil {
		return err
	} else if !enabled {
		return ErrInviteDisabled
	}

	metadata := cloneMap(input.Metadata)
	user := &types.AuthUser{
		Email:     strings.TrimSpace(input.Email),
		Username:  strings.TrimSpace(input.Username),
		FirstName: strings.TrimSpace(input.FirstName),
		LastName:  strings.TrimSpace(input.LastName),
		Role:      input.Role,
		Status:    types.LifecycleStatePending,
		Metadata:  metadata,
	}
	created, err := c.repo.Create(ctx, user)
	if err != nil {
		return err
	}

	issuedAt := now(c.clock)
	expiresAt := issuedAt.Add(c.tokenTTL)
	jti := c.idGen.UUID().String()

	payload := buildSecureLinkPayload(
		SecureLinkActionInvite,
		created,
		scope,
		jti,
		issuedAt,
		expiresAt,
		secureLinkSourceDefault,
	)
	token, err := c.manager.Generate(c.route, payload)
	if err != nil {
		return err
	}

	if _, err := c.tokens.CreateToken(ctx, types.UserToken{
		UserID:    created.ID,
		Type:      types.UserTokenInvite,
		JTI:       jti,
		Status:    types.UserTokenStatusIssued,
		IssuedAt:  issuedAt,
		ExpiresAt: expiresAt,
	}); err != nil {
		return err
	}

	attachTokenMetadata(created, "invite", tokenMetadata(jti, issuedAt, expiresAt, input.Actor, scope))
	if updated, err := c.repo.Update(ctx, created); err != nil {
		return err
	} else if updated != nil {
		created = updated
	}

	record := types.ActivityRecord{
		UserID:     created.ID,
		ActorID:    input.Actor.ID,
		Verb:       "user.invite",
		ObjectType: "user",
		ObjectID:   created.ID.String(),
		Channel:    "invites",
		TenantID:   scope.TenantID,
		OrgID:      scope.OrgID,
		Data: map[string]any{
			"email":      created.Email,
			"role":       created.Role,
			"jti":        jti,
			"expires_at": expiresAt,
		},
		OccurredAt: issuedAt,
	}
	logActivity(ctx, c.sink, record)
	emitActivityHook(ctx, c.hooks, record)

	if input.Result != nil {
		*input.Result = UserInviteResult{
			User:      created,
			Token:     token,
			ExpiresAt: expiresAt,
		}
	}

	return nil
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
