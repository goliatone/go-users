package command

import (
	"context"
	"strings"
	"time"

	gocommand "github.com/goliatone/go-command"
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
	repo     types.AuthRepository
	tokens   types.UserTokenRepository
	manager  types.SecureLinkManager
	clock    types.Clock
	idGen    types.IDGenerator
	sink     types.ActivitySink
	hooks    types.Hooks
	logger   types.Logger
	tokenTTL time.Duration
	guard    scope.Guard
	route    string
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
	Route           string
}

// NewUserRegistrationRequestCommand constructs the registration handler.
func NewUserRegistrationRequestCommand(cfg RegistrationRequestConfig) *UserRegistrationRequestCommand {
	ttl := cfg.TokenTTL
	if ttl == 0 && cfg.SecureLinks != nil {
		ttl = cfg.SecureLinks.GetExpiration()
	}
	if ttl == 0 {
		ttl = defaultRegistrationTTL
	}
	idGen := cfg.IDGen
	if idGen == nil {
		idGen = types.UUIDGenerator{}
	}
	route := strings.TrimSpace(cfg.Route)
	if route == "" {
		route = SecureLinkRouteRegister
	}
	return &UserRegistrationRequestCommand{
		repo:     cfg.Repository,
		tokens:   cfg.TokenRepository,
		manager:  cfg.SecureLinks,
		clock:    safeClock(cfg.Clock),
		idGen:    idGen,
		sink:     safeActivitySink(cfg.Activity),
		hooks:    safeHooks(cfg.Hooks),
		logger:   safeLogger(cfg.Logger),
		tokenTTL: ttl,
		guard:    safeScopeGuard(cfg.ScopeGuard),
		route:    route,
	}
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
	scope := input.Scope.Clone()
	if input.Actor.ID != uuid.Nil {
		var err error
		scope, err = c.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionUsersWrite, uuid.Nil)
		if err != nil {
			return err
		}
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
		SecureLinkActionRegister,
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
		Type:      types.UserTokenRegistration,
		JTI:       jti,
		Status:    types.UserTokenStatusIssued,
		IssuedAt:  issuedAt,
		ExpiresAt: expiresAt,
	}); err != nil {
		return err
	}

	attachTokenMetadata(created, "registration", tokenMetadata(jti, issuedAt, expiresAt, input.Actor, scope))
	if updated, err := c.repo.Update(ctx, created); err != nil {
		return err
	} else if updated != nil {
		created = updated
	}

	actor := input.Actor
	if actor.ID == uuid.Nil {
		actor = types.ActorRef{ID: created.ID, Type: "user"}
	}
	record := types.ActivityRecord{
		UserID:     created.ID,
		ActorID:    actor.ID,
		Verb:       "user.registration.requested",
		ObjectType: "user",
		ObjectID:   created.ID.String(),
		Channel:    "registration",
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
		*input.Result = UserRegistrationRequestResult{
			User:      created,
			Token:     token,
			ExpiresAt: expiresAt,
		}
	}

	return nil
}
