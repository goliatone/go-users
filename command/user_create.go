package command

import (
	"context"
	"strings"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

// UserCreateInput captures the payload for direct user creation.
type UserCreateInput struct {
	User   *types.AuthUser
	Status types.LifecycleState
	Actor  types.ActorRef
	Scope  types.ScopeFilter
	Result *types.AuthUser
}

// Type implements gocommand.Message.
func (UserCreateInput) Type() string {
	return "command.user.create"
}

// Validate implements gocommand.Message.
func (input UserCreateInput) Validate() error {
	switch {
	case input.User == nil:
		return ErrUserRequired
	case strings.TrimSpace(input.User.Email) == "":
		return ErrUserEmailRequired
	case input.Actor.ID == uuid.Nil:
		return ErrActorRequired
	default:
		return nil
	}
}

// UserCreateCommand creates active users directly.
type UserCreateCommand struct {
	repo   types.AuthRepository
	clock  types.Clock
	sink   types.ActivitySink
	hooks  types.Hooks
	logger types.Logger
	guard  scope.Guard
}

// UserCreateCommandConfig wires dependencies for the create command.
type UserCreateCommandConfig struct {
	Repository types.AuthRepository
	Clock      types.Clock
	Activity   types.ActivitySink
	Hooks      types.Hooks
	Logger     types.Logger
	ScopeGuard scope.Guard
}

// NewUserCreateCommand constructs the create handler.
func NewUserCreateCommand(cfg UserCreateCommandConfig) *UserCreateCommand {
	return &UserCreateCommand{
		repo:   cfg.Repository,
		clock:  safeClock(cfg.Clock),
		sink:   safeActivitySink(cfg.Activity),
		hooks:  safeHooks(cfg.Hooks),
		logger: safeLogger(cfg.Logger),
		guard:  safeScopeGuard(cfg.ScopeGuard),
	}
}

var _ gocommand.Commander[UserCreateInput] = (*UserCreateCommand)(nil)

// Execute creates the user record and logs audit metadata.
func (c *UserCreateCommand) Execute(ctx context.Context, input UserCreateInput) error {
	if c.repo == nil {
		return types.ErrMissingAuthRepository
	}
	if err := input.Validate(); err != nil {
		return err
	}

	scopeFilter, err := c.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionUsersWrite, uuid.Nil)
	if err != nil {
		return err
	}

	user := normalizeAuthUser(input.User)
	status := input.Status
	if status == "" && user != nil {
		status = user.Status
	}
	if status == "" {
		status = types.LifecycleStateActive
	}
	if user != nil {
		user.Status = status
	}

	created, err := c.repo.Create(ctx, user)
	if err != nil {
		return err
	}

	record := types.ActivityRecord{
		UserID:     created.ID,
		ActorID:    input.Actor.ID,
		Verb:       "user.created",
		ObjectType: "user",
		ObjectID:   created.ID.String(),
		Channel:    "users",
		TenantID:   scopeFilter.TenantID,
		OrgID:      scopeFilter.OrgID,
		Data: map[string]any{
			"email":  created.Email,
			"role":   created.Role,
			"status": created.Status,
		},
		OccurredAt: now(c.clock),
	}
	logActivity(ctx, c.sink, record)
	emitActivityHook(ctx, c.hooks, record)

	if input.Result != nil && created != nil {
		*input.Result = *created
	}

	return nil
}
