package command

import (
	"context"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

// DeleteRoleInput removes a custom role.
type DeleteRoleInput struct {
	RoleID uuid.UUID
	Scope  types.ScopeFilter
	Actor  types.ActorRef
}

// Type implements gocommand.Message.
func (DeleteRoleInput) Type() string {
	return "command.role.delete"
}

// Validate implements gocommand.Message.
func (input DeleteRoleInput) Validate() error {
	return validateRoleTarget(input.RoleID, input.Actor)
}

// DeleteRoleCommand deletes roles through the registry.
type DeleteRoleCommand struct {
	registry types.RoleRegistry
	guard    scope.Guard
}

// NewDeleteRoleCommand constructs the handler.
func NewDeleteRoleCommand(registry types.RoleRegistry, guard scope.Guard) *DeleteRoleCommand {
	return &DeleteRoleCommand{
		registry: registry,
		guard:    safeScopeGuard(guard),
	}
}

var _ gocommand.Commander[DeleteRoleInput] = (*DeleteRoleCommand)(nil)

// Execute deletes the requested role after validation.
func (c *DeleteRoleCommand) Execute(ctx context.Context, input DeleteRoleInput) error {
	if err := input.Validate(); err != nil {
		return err
	}
	scope, err := c.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionRolesWrite, input.RoleID)
	if err != nil {
		return err
	}
	return c.registry.DeleteRole(ctx, input.RoleID, scope, input.Actor.ID)
}
