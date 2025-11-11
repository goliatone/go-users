package command

import (
	"context"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

// UserLifecycleTransitionInput describes the lifecycle mutation request.
type UserLifecycleTransitionInput struct {
	UserID   uuid.UUID
	Target   types.LifecycleState
	Actor    types.ActorRef
	Reason   string
	Metadata map[string]any
	Scope    types.ScopeFilter
	Result   *UserLifecycleTransitionResult
}

// Type implements gocommand.Message.
func (UserLifecycleTransitionInput) Type() string {
	return "command.user.lifecycle.transition"
}

// Validate implements gocommand.Message.
func (input UserLifecycleTransitionInput) Validate() error {
	switch {
	case input.UserID == uuid.Nil:
		return ErrLifecycleUserIDRequired
	case input.Target == "":
		return ErrLifecycleTargetRequired
	case input.Actor.ID == uuid.Nil:
		return ErrActorRequired
	default:
		return nil
	}
}

// UserLifecycleTransitionResult carries the updated auth user.
type UserLifecycleTransitionResult struct {
	User *types.AuthUser
}

// UserLifecycleTransitionCommand implements go-command.Commander, enforcing the
// configured transition policy and logging hooks/audits.
type UserLifecycleTransitionCommand struct {
	repo     types.AuthRepository
	policy   types.TransitionPolicy
	clock    types.Clock
	logger   types.Logger
	hooks    types.Hooks
	activity types.ActivitySink
	guard    scope.Guard
}

// LifecycleCommandConfig configures the lifecycle command handler.
type LifecycleCommandConfig struct {
	Repository types.AuthRepository
	Policy     types.TransitionPolicy
	Clock      types.Clock
	Logger     types.Logger
	Hooks      types.Hooks
	Activity   types.ActivitySink
	ScopeGuard scope.Guard
}

// NewUserLifecycleTransitionCommand wires the lifecycle handler.
func NewUserLifecycleTransitionCommand(cfg LifecycleCommandConfig) *UserLifecycleTransitionCommand {
	policy := cfg.Policy
	if policy == nil {
		policy = types.DefaultTransitionPolicy()
	}
	return &UserLifecycleTransitionCommand{
		repo:     cfg.Repository,
		policy:   policy,
		clock:    safeClock(cfg.Clock),
		logger:   safeLogger(cfg.Logger),
		hooks:    safeHooks(cfg.Hooks),
		activity: safeActivitySink(cfg.Activity),
		guard:    safeScopeGuard(cfg.ScopeGuard),
	}
}

var _ gocommand.Commander[UserLifecycleTransitionInput] = (*UserLifecycleTransitionCommand)(nil)

// Execute performs the lifecycle transition against the upstream repository.
func (c *UserLifecycleTransitionCommand) Execute(ctx context.Context, input UserLifecycleTransitionInput) error {
	if err := input.Validate(); err != nil {
		return err
	}
	scope, err := c.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionUsersWrite, input.UserID)
	if err != nil {
		return err
	}
	current, err := c.repo.GetByID(ctx, input.UserID)
	if err != nil {
		return err
	}
	if err := c.enforcePolicy(current, input.Target); err != nil {
		return err
	}
	opts := make([]types.TransitionOption, 0, 2)
	if input.Reason != "" {
		opts = append(opts, types.WithTransitionReason(input.Reason))
	}
	if len(input.Metadata) > 0 {
		opts = append(opts, types.WithTransitionMetadata(input.Metadata))
	}
	updated, err := c.repo.UpdateStatus(ctx, input.Actor, input.UserID, input.Target, opts...)
	if err != nil {
		return err
	}

	eventTime := now(c.clock)
	record := types.ActivityRecord{
		UserID:     updated.ID,
		ActorID:    input.Actor.ID,
		Verb:       "user.lifecycle.transition",
		ObjectType: "user",
		ObjectID:   updated.ID.String(),
		Channel:    "lifecycle",
		TenantID:   scope.TenantID,
		OrgID:      scope.OrgID,
		Data: map[string]any{
			"from_state": current.Status,
			"to_state":   input.Target,
			"reason":     input.Reason,
			"metadata":   input.Metadata,
		},
		OccurredAt: eventTime,
	}
	logActivity(ctx, c.activity, record)
	emitActivityHook(ctx, c.hooks, record)

	emitLifecycleHook(ctx, c.hooks, types.LifecycleEvent{
		UserID:     updated.ID,
		ActorID:    input.Actor.ID,
		FromState:  current.Status,
		ToState:    input.Target,
		Reason:     input.Reason,
		OccurredAt: eventTime,
		Scope:      scope,
		Metadata:   input.Metadata,
	})

	if input.Result != nil {
		input.Result.User = updated
	}
	return nil
}

func (c *UserLifecycleTransitionCommand) enforcePolicy(current *types.AuthUser, target types.LifecycleState) error {
	if current == nil || c.policy == nil {
		return nil
	}
	if err := c.policy.Validate(current.Status, target); err != nil {
		c.logger.Debug("lifecycle policy rejected transition", "user_id", current.ID, "from", current.Status, "to", target)
		return err
	}
	return nil
}

// Describe returns a human readable description of the command for debugging.
func (UserLifecycleTransitionInput) Describe() string {
	return "user lifecycle transition"
}
