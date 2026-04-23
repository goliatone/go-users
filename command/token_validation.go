package command

import (
	"context"
	"strings"
	"time"

	gocommand "github.com/goliatone/go-command"
	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

type tokenValidator struct {
	tokens   types.UserTokenRepository
	manager  types.SecureLinkManager
	clock    types.Clock
	enforcer types.ScopeEnforcer
}

func newTokenValidator(clock types.Clock, tokens types.UserTokenRepository, manager types.SecureLinkManager, enforcer types.ScopeEnforcer) tokenValidator {
	return tokenValidator{
		tokens:   tokens,
		manager:  manager,
		clock:    safeClock(clock),
		enforcer: enforcer,
	}
}

func (v tokenValidator) validate(ctx context.Context, token string, tokenType types.UserTokenType, scope types.ScopeFilter) (types.SecureLinkPayload, *types.UserToken, error) {
	if err := v.validateInputs(token, tokenType); err != nil {
		return nil, nil, err
	}
	payload, err := v.validatePayload(token)
	if err != nil {
		return nil, nil, err
	}
	jti := payloadString(payload, "jti")
	if jti == "" {
		return nil, nil, ErrTokenJTIRequired
	}
	record, err := v.loadUsableToken(ctx, tokenType, jti, payload)
	if err != nil {
		return nil, nil, err
	}
	if err := v.enforceTokenScope(ctx, payload, scope); err != nil {
		return nil, nil, err
	}
	return payload, record, nil
}

func (v tokenValidator) validateInputs(token string, tokenType types.UserTokenType) error {
	if v.manager == nil {
		return types.ErrMissingSecureLinkManager
	}
	if v.tokens == nil {
		return types.ErrMissingUserTokenRepository
	}
	if strings.TrimSpace(token) == "" {
		return ErrTokenRequired
	}
	if tokenType == "" {
		return ErrTokenTypeRequired
	}
	return nil
}

func (v tokenValidator) validatePayload(token string) (types.SecureLinkPayload, error) {
	payloadMap, err := v.manager.Validate(token)
	if err != nil {
		return nil, err
	}
	return types.SecureLinkPayload(payloadMap), nil
}

func (v tokenValidator) loadUsableToken(ctx context.Context, tokenType types.UserTokenType, jti string, payload types.SecureLinkPayload) (*types.UserToken, error) {
	record, err := v.tokens.GetTokenByJTI(ctx, tokenType, jti)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrTokenNotFound
	}
	if record.Status == types.UserTokenStatusUsed || !record.UsedAt.IsZero() {
		return nil, ErrTokenAlreadyUsed
	}
	if record.Status == types.UserTokenStatusExpired {
		return nil, ErrTokenExpired
	}

	expiresAt := record.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = payloadTime(payload, "expires_at")
	}
	if !expiresAt.IsZero() && now(v.clock).After(expiresAt) {
		_ = v.tokens.UpdateTokenStatus(ctx, tokenType, jti, types.UserTokenStatusExpired, time.Time{})
		return nil, ErrTokenExpired
	}
	payloadUserID := payloadUUID(payload, "user_id")
	if payloadUserID != uuid.Nil && record.UserID != uuid.Nil && payloadUserID != record.UserID {
		return nil, ErrTokenUserMismatch
	}
	return record, nil
}

func (v tokenValidator) enforceTokenScope(ctx context.Context, payload types.SecureLinkPayload, scope types.ScopeFilter) error {
	if v.enforcer != nil {
		if err := v.enforcer(ctx, payload, scope); err != nil {
			return err
		}
	}
	return nil
}

// UserTokenValidateInput validates an onboarding token without consuming it.
type UserTokenValidateInput struct {
	Token     string
	TokenType types.UserTokenType
	Scope     types.ScopeFilter
	Result    *UserTokenValidateResult
}

// Type implements gocommand.Message.
func (UserTokenValidateInput) Type() string {
	return "command.user.token.validate"
}

// Validate implements gocommand.Message.
func (input UserTokenValidateInput) Validate() error {
	if strings.TrimSpace(input.Token) == "" {
		return ErrTokenRequired
	}
	if input.TokenType == "" {
		return ErrTokenTypeRequired
	}
	return nil
}

// UserTokenValidateResult exposes the decoded payload and token record.
type UserTokenValidateResult struct {
	Token   *types.UserToken
	Payload types.SecureLinkPayload
}

// UserTokenValidateCommand verifies securelink tokens against stored metadata.
type UserTokenValidateCommand struct {
	validator tokenValidator
}

// TokenValidateConfig holds dependencies for validation.
type TokenValidateConfig struct {
	TokenRepository types.UserTokenRepository
	SecureLinks     types.SecureLinkManager
	Clock           types.Clock
	ScopeEnforcer   types.ScopeEnforcer
}

// NewUserTokenValidateCommand constructs the validation handler.
func NewUserTokenValidateCommand(cfg TokenValidateConfig) *UserTokenValidateCommand {
	return &UserTokenValidateCommand{
		validator: newTokenValidator(cfg.Clock, cfg.TokenRepository, cfg.SecureLinks, cfg.ScopeEnforcer),
	}
}

var _ gocommand.Commander[UserTokenValidateInput] = (*UserTokenValidateCommand)(nil)

// Execute validates the token and returns the payload.
func (c *UserTokenValidateCommand) Execute(ctx context.Context, input UserTokenValidateInput) error {
	if err := input.Validate(); err != nil {
		return err
	}
	payload, record, err := c.validator.validate(ctx, input.Token, input.TokenType, input.Scope)
	if err != nil {
		return err
	}
	if input.Result != nil {
		input.Result.Token = record
		input.Result.Payload = payload
	}
	return nil
}

// UserTokenConsumeInput validates and consumes an onboarding token.
type UserTokenConsumeInput struct {
	Token     string
	TokenType types.UserTokenType
	Scope     types.ScopeFilter
	Result    *UserTokenConsumeResult
}

// Type implements gocommand.Message.
func (UserTokenConsumeInput) Type() string {
	return "command.user.token.consume"
}

// Validate implements gocommand.Message.
func (input UserTokenConsumeInput) Validate() error {
	if strings.TrimSpace(input.Token) == "" {
		return ErrTokenRequired
	}
	if input.TokenType == "" {
		return ErrTokenTypeRequired
	}
	return nil
}

// UserTokenConsumeResult exposes the consumed token metadata.
type UserTokenConsumeResult struct {
	Token   *types.UserToken
	Payload types.SecureLinkPayload
}

// UserTokenConsumeCommand validates tokens and marks them consumed.
type UserTokenConsumeCommand struct {
	validator tokenValidator
	tokens    types.UserTokenRepository
	clock     types.Clock
	sink      types.ActivitySink
	hooks     types.Hooks
}

// TokenConsumeConfig holds dependencies for consumption.
type TokenConsumeConfig struct {
	TokenRepository types.UserTokenRepository
	SecureLinks     types.SecureLinkManager
	Clock           types.Clock
	ScopeEnforcer   types.ScopeEnforcer
	Activity        types.ActivitySink
	Hooks           types.Hooks
}

// NewUserTokenConsumeCommand constructs the consumption handler.
func NewUserTokenConsumeCommand(cfg TokenConsumeConfig) *UserTokenConsumeCommand {
	clock := safeClock(cfg.Clock)
	return &UserTokenConsumeCommand{
		validator: newTokenValidator(clock, cfg.TokenRepository, cfg.SecureLinks, cfg.ScopeEnforcer),
		tokens:    cfg.TokenRepository,
		clock:     clock,
		sink:      safeActivitySink(cfg.Activity),
		hooks:     safeHooks(cfg.Hooks),
	}
}

var _ gocommand.Commander[UserTokenConsumeInput] = (*UserTokenConsumeCommand)(nil)

// Execute validates the token, records consumption, and logs activity.
func (c *UserTokenConsumeCommand) Execute(ctx context.Context, input UserTokenConsumeInput) error {
	if err := input.Validate(); err != nil {
		return err
	}
	payload, record, err := c.validator.validate(ctx, input.Token, input.TokenType, input.Scope)
	if err != nil {
		return err
	}
	if record == nil {
		return ErrTokenNotFound
	}

	usedAt := now(c.clock)
	if err := c.tokens.UpdateTokenStatus(ctx, input.TokenType, record.JTI, types.UserTokenStatusUsed, usedAt); err != nil {
		return c.tokenConsumeError(ctx, input.TokenType, record.JTI, usedAt, err)
	}
	record.Status = types.UserTokenStatusUsed
	record.UsedAt = usedAt

	verb, channel := tokenConsumeActivity(input.TokenType)
	scope := scopeFromPayload(payload)
	recordActivity := types.ActivityRecord{
		UserID:     record.UserID,
		ActorID:    record.UserID,
		Verb:       verb,
		ObjectType: "user",
		ObjectID:   record.UserID.String(),
		Channel:    channel,
		TenantID:   scope.TenantID,
		OrgID:      scope.OrgID,
		Data: map[string]any{
			"token_type": string(input.TokenType),
			"jti":        record.JTI,
			"expires_at": record.ExpiresAt,
			"email":      payloadString(payload, "email"),
		},
		OccurredAt: usedAt,
	}
	logActivity(ctx, c.sink, recordActivity)
	emitActivityHook(ctx, c.hooks, recordActivity)

	if input.Result != nil {
		input.Result.Token = record
		input.Result.Payload = payload
	}
	return nil
}

func (c *UserTokenConsumeCommand) tokenConsumeError(ctx context.Context, tokenType types.UserTokenType, jti string, usedAt time.Time, err error) error {
	if repository.IsRecordNotFound(err) {
		return ErrTokenNotFound
	}
	if !repository.IsSQLExpectedCountViolation(err) {
		return err
	}
	latest, lookupErr := c.tokens.GetTokenByJTI(ctx, tokenType, jti)
	if lookupErr != nil {
		return ErrTokenAlreadyUsed
	}
	return userTokenConflictError(latest, usedAt)
}

func userTokenConflictError(record *types.UserToken, usedAt time.Time) error {
	if record == nil {
		return ErrTokenNotFound
	}
	if !record.ExpiresAt.IsZero() && usedAt.After(record.ExpiresAt) {
		return ErrTokenExpired
	}
	if record.Status == types.UserTokenStatusExpired {
		return ErrTokenExpired
	}
	if record.Status == types.UserTokenStatusUsed || !record.UsedAt.IsZero() {
		return ErrTokenAlreadyUsed
	}
	return ErrTokenAlreadyUsed
}

func tokenConsumeActivity(tokenType types.UserTokenType) (string, string) {
	switch tokenType {
	case types.UserTokenInvite:
		return "user.invite.consumed", "invites"
	case types.UserTokenRegistration:
		return "user.registration.completed", "registration"
	default:
		return "user.token.consumed", "tokens"
	}
}
