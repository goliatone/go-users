package command

import (
	"context"
	"strings"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

// UpdateRoleInput captures mutable role fields.
type UpdateRoleInput struct {
	RoleID      uuid.UUID
	Name        string
	Description string
	Permissions []string
	IsSystem    bool
	Scope       types.ScopeFilter
	Actor       types.ActorRef
	Result      *types.RoleDefinition
}

// Type implements gocommand.Message.
func (UpdateRoleInput) Type() string {
	return "command.role.update"
}

// Validate implements gocommand.Message.
func (input UpdateRoleInput) Validate() error {
	if err := validateRoleTarget(input.RoleID, input.Actor); err != nil {
		return err
	}
	if strings.TrimSpace(input.Name) == "" {
		return ErrRoleNameRequired
	}
	return nil
}

// UpdateRoleCommand updates custom roles.
type UpdateRoleCommand struct {
	registry types.RoleRegistry
	guard    scope.Guard
}

// NewUpdateRoleCommand constructs the command handler.
func NewUpdateRoleCommand(registry types.RoleRegistry, guard scope.Guard) *UpdateRoleCommand {
	return &UpdateRoleCommand{
		registry: registry,
		guard:    safeScopeGuard(guard),
	}
}

var _ gocommand.Commander[UpdateRoleInput] = (*UpdateRoleCommand)(nil)

// Execute forwards the update payload to the registry.
func (c *UpdateRoleCommand) Execute(ctx context.Context, input UpdateRoleInput) error {
	if err := input.Validate(); err != nil {
		return err
	}
	scope, err := c.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionRolesWrite, input.RoleID)
	if err != nil {
		return err
	}
	role, err := c.registry.UpdateRole(ctx, input.RoleID, types.RoleMutation{
		Name:        strings.TrimSpace(input.Name),
		Description: strings.TrimSpace(input.Description),
		Permissions: input.Permissions,
		IsSystem:    input.IsSystem,
		Scope:       scope,
		ActorID:     input.Actor.ID,
	})
	if err != nil {
		return err
	}
	if input.Result != nil && role != nil {
		*input.Result = *role
	}
	return nil
}
