package command

import (
	"context"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

// AssignRoleInput assigns a role to a user.
type AssignRoleInput struct {
	UserID uuid.UUID
	RoleID uuid.UUID
	Scope  types.ScopeFilter
	Actor  types.ActorRef
}

// Type implements gocommand.Message.
func (AssignRoleInput) Type() string {
	return "command.role.assign"
}

// Validate implements gocommand.Message.
func (input AssignRoleInput) Validate() error {
	if err := validateRoleTarget(input.RoleID, input.Actor); err != nil {
		return err
	}
	if input.UserID == uuid.Nil {
		return ErrUserIDRequired
	}
	return nil
}

// UnassignRoleInput removes a role assignment.
type UnassignRoleInput struct {
	UserID uuid.UUID
	RoleID uuid.UUID
	Scope  types.ScopeFilter
	Actor  types.ActorRef
}

// Type implements gocommand.Message.
func (UnassignRoleInput) Type() string {
	return "command.role.unassign"
}

// Validate implements gocommand.Message.
func (input UnassignRoleInput) Validate() error {
	if err := validateRoleTarget(input.RoleID, input.Actor); err != nil {
		return err
	}
	if input.UserID == uuid.Nil {
		return ErrUserIDRequired
	}
	return nil
}

// AssignRoleCommand wraps registry assignments.
type AssignRoleCommand struct {
	registry types.RoleRegistry
	guard    scope.Guard
}

// NewAssignRoleCommand constructs the handler.
func NewAssignRoleCommand(registry types.RoleRegistry, guard scope.Guard) *AssignRoleCommand {
	return &AssignRoleCommand{
		registry: registry,
		guard:    safeScopeGuard(guard),
	}
}

var _ gocommand.Commander[AssignRoleInput] = (*AssignRoleCommand)(nil)

// Execute assigns the requested role.
func (c *AssignRoleCommand) Execute(ctx context.Context, input AssignRoleInput) error {
	if err := input.Validate(); err != nil {
		return err
	}
	scope, err := c.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionRolesWrite, input.RoleID)
	if err != nil {
		return err
	}
	return c.registry.AssignRole(ctx, input.UserID, input.RoleID, scope, input.Actor.ID)
}

// UnassignRoleCommand removes assignments.
type UnassignRoleCommand struct {
	registry types.RoleRegistry
	guard    scope.Guard
}

// NewUnassignRoleCommand constructs the handler.
func NewUnassignRoleCommand(registry types.RoleRegistry, guard scope.Guard) *UnassignRoleCommand {
	return &UnassignRoleCommand{
		registry: registry,
		guard:    safeScopeGuard(guard),
	}
}

var _ gocommand.Commander[UnassignRoleInput] = (*UnassignRoleCommand)(nil)

// Execute removes the given assignment.
func (c *UnassignRoleCommand) Execute(ctx context.Context, input UnassignRoleInput) error {
	if err := input.Validate(); err != nil {
		return err
	}
	scope, err := c.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionRolesWrite, input.RoleID)
	if err != nil {
		return err
	}
	return c.registry.UnassignRole(ctx, input.UserID, input.RoleID, scope, input.Actor.ID)
}
