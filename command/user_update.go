package command

import (
	"context"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

// UserUpdateInput captures the payload for user updates.
type UserUpdateInput struct {
	User   *types.AuthUser
	Actor  types.ActorRef
	Scope  types.ScopeFilter
	Result *types.AuthUser
}

// Type implements gocommand.Message.
func (UserUpdateInput) Type() string {
	return "command.user.update"
}

// Validate implements gocommand.Message.
func (input UserUpdateInput) Validate() error {
	switch {
	case input.User == nil:
		return ErrUserRequired
	case input.User.ID == uuid.Nil:
		return ErrUserIDRequired
	case input.Actor.ID == uuid.Nil:
		return ErrActorRequired
	default:
		return nil
	}
}

// UserUpdateCommand updates existing users while enforcing scopes.
type UserUpdateCommand struct {
	repo   types.AuthRepository
	policy types.TransitionPolicy
	clock  types.Clock
	sink   types.ActivitySink
	hooks  types.Hooks
	logger types.Logger
	guard  scope.Guard
}

// UserUpdateCommandConfig wires dependencies for the update command.
type UserUpdateCommandConfig struct {
	Repository types.AuthRepository
	Policy     types.TransitionPolicy
	Clock      types.Clock
	Activity   types.ActivitySink
	Hooks      types.Hooks
	Logger     types.Logger
	ScopeGuard scope.Guard
}

// NewUserUpdateCommand constructs the update handler.
func NewUserUpdateCommand(cfg UserUpdateCommandConfig) *UserUpdateCommand {
	policy := cfg.Policy
	if policy == nil {
		policy = types.DefaultTransitionPolicy()
	}
	return &UserUpdateCommand{
		repo:   cfg.Repository,
		policy: policy,
		clock:  safeClock(cfg.Clock),
		sink:   safeActivitySink(cfg.Activity),
		hooks:  safeHooks(cfg.Hooks),
		logger: safeLogger(cfg.Logger),
		guard:  safeScopeGuard(cfg.ScopeGuard),
	}
}

var _ gocommand.Commander[UserUpdateInput] = (*UserUpdateCommand)(nil)

// Execute updates the user record and logs audit metadata.
func (c *UserUpdateCommand) Execute(ctx context.Context, input UserUpdateInput) error {
	if c == nil || c.repo == nil {
		return types.ErrMissingAuthRepository
	}
	if err := input.Validate(); err != nil {
		return err
	}

	scopeFilter, err := c.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionUsersWrite, input.User.ID)
	if err != nil {
		return err
	}

	user := normalizeAuthUser(input.User)
	if user != nil && user.Status != "" {
		current, err := c.repo.GetByID(ctx, user.ID)
		if err != nil {
			return err
		}
		if current != nil && current.Status != user.Status && c.policy != nil {
			if err := c.policy.Validate(current.Status, user.Status); err != nil {
				return err
			}
		}
	}
	updated, err := c.repo.Update(ctx, user)
	if err != nil {
		return err
	}

	record := types.ActivityRecord{
		UserID:     updated.ID,
		ActorID:    input.Actor.ID,
		Verb:       "user.updated",
		ObjectType: "user",
		ObjectID:   updated.ID.String(),
		Channel:    "users",
		TenantID:   scopeFilter.TenantID,
		OrgID:      scopeFilter.OrgID,
		Data: map[string]any{
			"email":  updated.Email,
			"role":   updated.Role,
			"status": updated.Status,
		},
		OccurredAt: now(c.clock),
	}
	logActivity(ctx, c.sink, record)
	emitActivityHook(ctx, c.hooks, record)

	if input.Result != nil && updated != nil {
		*input.Result = *updated
	}

	return nil
}
