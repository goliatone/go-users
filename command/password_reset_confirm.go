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

// Execute validates the securelink token, consumes it, and applies the reset.
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

	payloadMap, err := c.manager.Validate(input.Token)
	if err != nil {
		return err
	}
	payload := types.SecureLinkPayload(payloadMap)
	jti := payloadString(payload, "jti")
	if jti == "" {
		return ErrTokenJTIRequired
	}

	record, err := c.resets.GetResetByJTI(ctx, jti)
	if err != nil {
		return err
	}
	if record == nil {
		return ErrTokenNotFound
	}
	if record.Status == types.PasswordResetStatusChanged || !record.UsedAt.IsZero() {
		return ErrTokenAlreadyUsed
	}
	if record.Status == types.PasswordResetStatusExpired {
		return ErrTokenExpired
	}

	expiresAt := record.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = payloadTime(payload, "expires_at")
	}
	if !expiresAt.IsZero() && now(c.clock).After(expiresAt) {
		_ = c.resets.UpdateResetStatus(ctx, jti, types.PasswordResetStatusExpired, time.Time{})
		return ErrTokenExpired
	}

	payloadUserID := payloadUUID(payload, "user_id")
	if payloadUserID != uuid.Nil && record.UserID != uuid.Nil && payloadUserID != record.UserID {
		return ErrTokenUserMismatch
	}

	if c.enforcer != nil {
		if err := c.enforcer(ctx, payload, input.Scope); err != nil {
			return err
		}
	}

	consumedAt := now(c.clock)
	if err := c.resets.ConsumeReset(ctx, jti, consumedAt); err != nil {
		if repository.IsSQLExpectedCountViolation(err) {
			latest, lookupErr := c.resets.GetResetByJTI(ctx, jti)
			if lookupErr == nil {
				if latest == nil {
					return ErrTokenNotFound
				}
				if !latest.ExpiresAt.IsZero() && consumedAt.After(latest.ExpiresAt) {
					return ErrTokenExpired
				}
				if latest.Status == types.PasswordResetStatusExpired {
					return ErrTokenExpired
				}
				if latest.Status == types.PasswordResetStatusChanged || !latest.UsedAt.IsZero() {
					return ErrTokenAlreadyUsed
				}
			}
			return ErrTokenAlreadyUsed
		}
		if repository.IsRecordNotFound(err) {
			return ErrTokenNotFound
		}
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
	if err := c.resets.UpdateResetStatus(ctx, jti, types.PasswordResetStatusChanged, usedAt); err != nil {
		return err
	}

	if input.Result != nil {
		input.Result.User = result.User
	}
	return nil
}
