package command

import (
	"context"
	"strings"
	"time"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

const defaultPasswordResetTTL = 1 * time.Hour

// UserPasswordResetRequestInput issues a password reset securelink token.
type UserPasswordResetRequestInput struct {
	Identifier string
	UserID     uuid.UUID
	Actor      types.ActorRef
	Scope      types.ScopeFilter
	Metadata   map[string]any
	Result     *UserPasswordResetRequestResult
}

// Type implements gocommand.Message.
func (UserPasswordResetRequestInput) Type() string {
	return "command.user.password_reset.request"
}

// Validate implements gocommand.Message.
func (input UserPasswordResetRequestInput) Validate() error {
	if input.UserID == uuid.Nil && strings.TrimSpace(input.Identifier) == "" {
		return ErrResetIdentifierRequired
	}
	return nil
}

// UserPasswordResetRequestResult exposes the created token details.
type UserPasswordResetRequestResult struct {
	User      *types.AuthUser
	Token     string
	ExpiresAt time.Time
}

// UserPasswordResetRequestCommand issues securelink reset tokens and persists lifecycle data.
type UserPasswordResetRequestCommand struct {
	repo     types.AuthRepository
	resets   types.PasswordResetRepository
	manager  types.SecureLinkManager
	clock    types.Clock
	idGen    types.IDGenerator
	sink     types.ActivitySink
	hooks    types.Hooks
	logger   types.Logger
	tokenTTL time.Duration
	route    string
}

// PasswordResetRequestConfig holds dependencies for reset issuance.
type PasswordResetRequestConfig struct {
	Repository      types.AuthRepository
	ResetRepository types.PasswordResetRepository
	SecureLinks     types.SecureLinkManager
	Clock           types.Clock
	IDGen           types.IDGenerator
	Activity        types.ActivitySink
	Hooks           types.Hooks
	Logger          types.Logger
	TokenTTL        time.Duration
	Route           string
}

// NewUserPasswordResetRequestCommand constructs the request handler.
func NewUserPasswordResetRequestCommand(cfg PasswordResetRequestConfig) *UserPasswordResetRequestCommand {
	ttl := cfg.TokenTTL
	if ttl == 0 && cfg.SecureLinks != nil {
		ttl = cfg.SecureLinks.GetExpiration()
	}
	if ttl == 0 {
		ttl = defaultPasswordResetTTL
	}
	idGen := cfg.IDGen
	if idGen == nil {
		idGen = types.UUIDGenerator{}
	}
	route := strings.TrimSpace(cfg.Route)
	if route == "" {
		route = SecureLinkRoutePasswordReset
	}
	return &UserPasswordResetRequestCommand{
		repo:     cfg.Repository,
		resets:   cfg.ResetRepository,
		manager:  cfg.SecureLinks,
		clock:    safeClock(cfg.Clock),
		idGen:    idGen,
		sink:     safeActivitySink(cfg.Activity),
		hooks:    safeHooks(cfg.Hooks),
		logger:   safeLogger(cfg.Logger),
		tokenTTL: ttl,
		route:    route,
	}
}

var _ gocommand.Commander[UserPasswordResetRequestInput] = (*UserPasswordResetRequestCommand)(nil)

// Execute issues a password reset token and records lifecycle metadata.
func (c *UserPasswordResetRequestCommand) Execute(ctx context.Context, input UserPasswordResetRequestInput) error {
	if c.repo == nil {
		return types.ErrMissingAuthRepository
	}
	if c.manager == nil {
		return types.ErrMissingSecureLinkManager
	}
	if c.resets == nil {
		return types.ErrMissingPasswordResetRepository
	}
	if err := input.Validate(); err != nil {
		return err
	}

	var user *types.AuthUser
	var err error
	if input.UserID != uuid.Nil {
		user, err = c.repo.GetByID(ctx, input.UserID)
	} else {
		user, err = c.repo.GetByIdentifier(ctx, strings.TrimSpace(input.Identifier))
	}
	if err != nil {
		return err
	}
	if user == nil {
		return ErrUserNotFound
	}

	issuedAt := now(c.clock)
	expiresAt := issuedAt.Add(c.tokenTTL)
	jti := c.idGen.UUID().String()

	payload := buildSecureLinkPayload(
		SecureLinkActionPasswordReset,
		user,
		input.Scope,
		jti,
		issuedAt,
		expiresAt,
		secureLinkSourceDefault,
	)
	token, err := c.manager.Generate(c.route, payload)
	if err != nil {
		return err
	}

	if _, err := c.resets.CreateReset(ctx, types.PasswordResetRecord{
		UserID:    user.ID,
		Email:     user.Email,
		Status:    types.PasswordResetStatusRequested,
		JTI:       jti,
		IssuedAt:  issuedAt,
		ExpiresAt: expiresAt,
		Scope:     input.Scope,
	}); err != nil {
		return err
	}

	actor := input.Actor
	if actor.ID == uuid.Nil {
		actor = types.ActorRef{ID: user.ID, Type: "user"}
	}
	data := map[string]any{
		"user_email": user.Email,
		"jti":        jti,
		"expires_at": expiresAt,
	}
	if len(input.Metadata) > 0 {
		data["metadata"] = cloneMap(input.Metadata)
	}
	record := types.ActivityRecord{
		UserID:     user.ID,
		ActorID:    actor.ID,
		Verb:       "user.password.reset.requested",
		ObjectType: "user",
		ObjectID:   user.ID.String(),
		Channel:    "password",
		TenantID:   input.Scope.TenantID,
		OrgID:      input.Scope.OrgID,
		Data:       data,
		OccurredAt: issuedAt,
	}
	logActivity(ctx, c.sink, record)
	emitActivityHook(ctx, c.hooks, record)

	if input.Result != nil {
		*input.Result = UserPasswordResetRequestResult{
			User:      user,
			Token:     token,
			ExpiresAt: expiresAt,
		}
	}

	return nil
}
