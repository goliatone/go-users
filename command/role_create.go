package command

import (
	"context"
	"strings"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

// CreateRoleInput carries data for creating custom roles.
type CreateRoleInput struct {
	Name        string
	Description string
	RoleKey     string
	Permissions []string
	Metadata    map[string]any
	IsSystem    bool
	Scope       types.ScopeFilter
	Actor       types.ActorRef
	Result      *types.RoleDefinition
}

// Type implements gocommand.Message.
func (CreateRoleInput) Type() string {
	return "command.role.create"
}

// Validate implements gocommand.Message.
func (input CreateRoleInput) Validate() error {
	return validateRoleMutation(input.Actor, input.Name)
}

// CreateRoleCommand invokes the injected role registry.
type CreateRoleCommand struct {
	registry types.RoleRegistry
	guard    scope.Guard
}

// NewCreateRoleCommand wires a role creation handler.
func NewCreateRoleCommand(registry types.RoleRegistry, guard scope.Guard) *CreateRoleCommand {
	return &CreateRoleCommand{
		registry: registry,
		guard:    safeScopeGuard(guard),
	}
}

var _ gocommand.Commander[CreateRoleInput] = (*CreateRoleCommand)(nil)

// Execute validates and forwards the creation payload to the registry.
func (c *CreateRoleCommand) Execute(ctx context.Context, input CreateRoleInput) error {
	if err := input.Validate(); err != nil {
		return err
	}
	scope, err := c.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionRolesWrite, uuid.Nil)
	if err != nil {
		return err
	}
	role, err := c.registry.CreateRole(ctx, types.RoleMutation{
		Name:        strings.TrimSpace(input.Name),
		Description: strings.TrimSpace(input.Description),
		RoleKey:     strings.TrimSpace(input.RoleKey),
		Permissions: input.Permissions,
		Metadata:    input.Metadata,
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
