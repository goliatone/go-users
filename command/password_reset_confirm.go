package command

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	gocommand "github.com/goliatone/go-command"
	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-users/pkg/types"
)

// UserPasswordResetConfirmInput validates and consumes a reset token.
type UserPasswordResetConfirmInput struct {
	Token           string
	NewPasswordHash string
	Scope           types.ScopeFilter
	Result          *UserPasswordResetConfirmResult
}

// Type implements gocommand.Message.
func (UserPasswordResetConfirmInput) Type() string {
	return "command.user.password_reset.confirm"
}

// Validate implements gocommand.Message.
func (input UserPasswordResetConfirmInput) Validate() error {
	switch {
	case strings.TrimSpace(input.Token) == "":
		return ErrTokenRequired
	case strings.TrimSpace(input.NewPasswordHash) == "":
		return ErrPasswordHashRequired
	default:
		return nil
	}
}

// UserPasswordResetConfirmResult exposes the reset user.
type UserPasswordResetConfirmResult struct {
	User *types.AuthUser
}

// UserPasswordResetConfirmCommand validates tokens and applies password resets.
type UserPasswordResetConfirmCommand struct {
	manager  types.SecureLinkManager
	resets   types.PasswordResetRepository
	resetCmd *UserPasswordResetCommand
	clock    types.Clock
	enforcer types.ScopeEnforcer
	logger   types.Logger
}

// PasswordResetConfirmConfig holds dependencies for reset confirmation.
type PasswordResetConfirmConfig struct {
	ResetRepository types.PasswordResetRepository
	SecureLinks     types.SecureLinkManager
	ResetCommand    *UserPasswordResetCommand
	Clock           types.Clock
	ScopeEnforcer   types.ScopeEnforcer
	Logger          types.Logger
}

// NewUserPasswordResetConfirmCommand constructs the confirmation handler.
func NewUserPasswordResetConfirmCommand(cfg PasswordResetConfirmConfig) *UserPasswordResetConfirmCommand {
	return &UserPasswordResetConfirmCommand{
		manager:  cfg.SecureLinks,
		resets:   cfg.ResetRepository,
		resetCmd: cfg.ResetCommand,
		clock:    safeClock(cfg.Clock),
		enforcer: cfg.ScopeEnforcer,
		logger:   safeLogger(cfg.Logger),
	}
}

var _ gocommand.Commander[UserPasswordResetConfirmInput] = (*UserPasswordResetConfirmCommand)(nil)

// Execute validates the securelink token, applies the reset, and then consumes it.
func (c *UserPasswordResetConfirmCommand) Execute(ctx context.Context, input UserPasswordResetConfirmInput) error {
	if c.manager == nil {
		return types.ErrMissingSecureLinkManager
	}
	if c.resets == nil {
		return types.ErrMissingPasswordResetRepository
	}
	if c.resetCmd == nil {
		return ErrResetCommandRequired
	}
	if err := input.Validate(); err != nil {
		return err
	}

	payload, jti, record, expiresAt, err := c.resolveResetToken(ctx, input.Token)
	if err != nil {
		return err
	}
	if err := c.enforceResetScope(ctx, payload, input.Scope); err != nil {
		return err
	}

	resetScope := scopeFromPayload(payload)
	if resetScope.TenantID == uuid.Nil && resetScope.OrgID == uuid.Nil {
		resetScope = input.Scope
	}
	result := &UserPasswordResetResult{}
	if err := c.resetCmd.Execute(ctx, UserPasswordResetInput{
		UserID:          record.UserID,
		NewPasswordHash: strings.TrimSpace(input.NewPasswordHash),
		TokenJTI:        jti,
		TokenExpiresAt:  expiresAt,
		Actor:           types.ActorRef{ID: record.UserID, Type: "user"},
		Scope:           resetScope,
		Result:          result,
	}); err != nil {
		return err
	}

	usedAt := now(c.clock)
	if err := c.resets.ConsumeReset(ctx, jti, usedAt); err != nil {
		return c.passwordResetConsumeError(ctx, jti, usedAt, err)
	}
	if err := c.resets.UpdateResetStatus(ctx, jti, types.PasswordResetStatusChanged, usedAt); err != nil {
		return err
	}

	if input.Result != nil {
		input.Result.User = result.User
	}
	return nil
}

func (c *UserPasswordResetConfirmCommand) resolveResetToken(ctx context.Context, token string) (types.SecureLinkPayload, string, *types.PasswordResetRecord, time.Time, error) {
	payloadMap, err := c.manager.Validate(token)
	if err != nil {
		return nil, "", nil, time.Time{}, err
	}
	payload := types.SecureLinkPayload(payloadMap)
	jti := payloadString(payload, "jti")
	if jti == "" {
		return nil, "", nil, time.Time{}, ErrTokenJTIRequired
	}
	record, err := c.resets.GetResetByJTI(ctx, jti)
	if err != nil {
		return nil, "", nil, time.Time{}, err
	}
	expiresAt, err := c.validateResetRecord(ctx, jti, payload, record)
	if err != nil {
		return nil, "", nil, time.Time{}, err
	}
	return payload, jti, record, expiresAt, nil
}

func (c *UserPasswordResetConfirmCommand) validateResetRecord(ctx context.Context, jti string, payload types.SecureLinkPayload, record *types.PasswordResetRecord) (time.Time, error) {
	if record == nil {
		return time.Time{}, ErrTokenNotFound
	}
	if record.Status == types.PasswordResetStatusChanged || !record.UsedAt.IsZero() {
		return time.Time{}, ErrTokenAlreadyUsed
	}
	if record.Status == types.PasswordResetStatusExpired {
		return time.Time{}, ErrTokenExpired
	}
	expiresAt := record.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = payloadTime(payload, "expires_at")
	}
	if !expiresAt.IsZero() && now(c.clock).After(expiresAt) {
		_ = c.resets.UpdateResetStatus(ctx, jti, types.PasswordResetStatusExpired, time.Time{})
		return time.Time{}, ErrTokenExpired
	}
	payloadUserID := payloadUUID(payload, "user_id")
	if payloadUserID != uuid.Nil && record.UserID != uuid.Nil && payloadUserID != record.UserID {
		return time.Time{}, ErrTokenUserMismatch
	}
	return expiresAt, nil
}

func (c *UserPasswordResetConfirmCommand) enforceResetScope(ctx context.Context, payload types.SecureLinkPayload, scope types.ScopeFilter) error {
	if c.enforcer == nil {
		return nil
	}
	return c.enforcer(ctx, payload, scope)
}

func (c *UserPasswordResetConfirmCommand) passwordResetConsumeError(ctx context.Context, jti string, consumedAt time.Time, err error) error {
	if repository.IsRecordNotFound(err) {
		return ErrTokenNotFound
	}
	if !repository.IsSQLExpectedCountViolation(err) {
		return err
	}
	latest, lookupErr := c.resets.GetResetByJTI(ctx, jti)
	if lookupErr != nil {
		return ErrTokenAlreadyUsed
	}
	return passwordResetConflictError(latest, consumedAt)
}

func passwordResetConflictError(record *types.PasswordResetRecord, consumedAt time.Time) error {
	if record == nil {
		return ErrTokenNotFound
	}
	if !record.ExpiresAt.IsZero() && consumedAt.After(record.ExpiresAt) {
		return ErrTokenExpired
	}
	if record.Status == types.PasswordResetStatusExpired {
		return ErrTokenExpired
	}
	if record.Status == types.PasswordResetStatusChanged || !record.UsedAt.IsZero() {
		return ErrTokenAlreadyUsed
	}
	return ErrTokenAlreadyUsed
}
