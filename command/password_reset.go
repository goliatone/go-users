package command

import (
	"context"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

// UserPasswordResetInput resets a user's password hash.
type UserPasswordResetInput struct {
	UserID          uuid.UUID
	NewPasswordHash string
	Actor           types.ActorRef
	Scope           types.ScopeFilter
	Result          *UserPasswordResetResult
}

// UserPasswordResetResult surfaces auditing metadata.
type UserPasswordResetResult struct {
	User *types.AuthUser
}

// UserPasswordResetCommand wraps the AuthRepository password reset helper.
type UserPasswordResetCommand struct {
	repo   types.AuthRepository
	clock  types.Clock
	sink   types.ActivitySink
	hooks  types.Hooks
	logger types.Logger
	guard  scope.Guard
}

// PasswordResetCommandConfig wires the reset handler.
type PasswordResetCommandConfig struct {
	Repository types.AuthRepository
	Clock      types.Clock
	Activity   types.ActivitySink
	Hooks      types.Hooks
	Logger     types.Logger
	ScopeGuard scope.Guard
}

// NewUserPasswordResetCommand builds the handler.
func NewUserPasswordResetCommand(cfg PasswordResetCommandConfig) *UserPasswordResetCommand {
	return &UserPasswordResetCommand{
		repo:   cfg.Repository,
		clock:  safeClock(cfg.Clock),
		sink:   safeActivitySink(cfg.Activity),
		hooks:  safeHooks(cfg.Hooks),
		logger: safeLogger(cfg.Logger),
		guard:  safeScopeGuard(cfg.ScopeGuard),
	}
}

var _ gocommand.Commander[UserPasswordResetInput] = (*UserPasswordResetCommand)(nil)

// Execute resets the user's password hash and logs audit metadata.
func (c *UserPasswordResetCommand) Execute(ctx context.Context, input UserPasswordResetInput) error {
	if input.UserID == uuid.Nil {
		return ErrLifecycleUserIDRequired
	}
	if input.Actor.ID == uuid.Nil {
		return ErrActorRequired
	}
	if input.NewPasswordHash == "" {
		return ErrPasswordHashRequired
	}

	scope, err := c.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionUsersWrite, input.UserID)
	if err != nil {
		return err
	}

	user, err := c.repo.GetByID(ctx, input.UserID)
	if err != nil {
		return err
	}
	if err := c.repo.ResetPassword(ctx, input.UserID, input.NewPasswordHash); err != nil {
		return err
	}

	record := types.ActivityRecord{
		UserID:     input.UserID,
		ActorID:    input.Actor.ID,
		Verb:       "user.password.reset",
		ObjectType: "user",
		ObjectID:   input.UserID.String(),
		Channel:    "password",
		TenantID:   scope.TenantID,
		OrgID:      scope.OrgID,
		Data: map[string]any{
			"user_email": user.Email,
		},
		OccurredAt: now(c.clock),
	}
	logActivity(ctx, c.sink, record)
	emitActivityHook(ctx, c.hooks, record)
	if input.Result != nil {
		input.Result.User = user
	}
	return nil
}
