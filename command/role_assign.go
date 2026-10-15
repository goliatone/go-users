package command

import (
	"context"
	"maps"
	"reflect"

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
	registry   types.RoleRegistry
	guard      scope.Guard
	validation roleGrantValidation
}

// RoleAssignmentCommandConfig adds optional candidate-grant validation while
// preserving the existing constructors' scope-policy-only contract.
type RoleAssignmentCommandConfig struct {
	Registry               types.RoleRegistry
	ScopeGuard             scope.Guard
	GrantValidator         types.RoleGrantValidator
	RequireGrantValidation bool
}

// Validate rejects required or explicitly supplied typed nil validators.
func (cfg RoleAssignmentCommandConfig) Validate() error {
	if cfg.GrantValidator == nil {
		if cfg.RequireGrantValidation {
			return types.ErrMissingRoleGrantValidator
		}
		return nil
	}
	v := reflect.ValueOf(cfg.GrantValidator)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if v.IsNil() {
			return types.ErrMissingRoleGrantValidator
		}
	}
	return nil
}

type roleGrantValidation struct {
	validator types.RoleGrantValidator
	err       error
}

func (v roleGrantValidation) enforce(ctx context.Context, registry types.RoleRegistry, operation types.RoleGrantOperation, actor types.ActorRef, userID, roleID uuid.UUID, assignmentScope types.ScopeFilter) error {
	if v.err != nil {
		return v.err
	}
	if v.validator == nil {
		return nil
	}
	catalog := types.ScopeFilter{TenantID: assignmentScope.TenantID, OrgID: assignmentScope.OrgID}
	role, err := registry.GetRole(ctx, roleID, catalog)
	if err != nil {
		return err
	}
	if role == nil || role.ID != roleID || role.Scope.TenantID != catalog.TenantID || role.Scope.OrgID != catalog.OrgID || len(role.Scope.Labels) > 0 {
		return types.ErrInvalidRoleGrantDefinition
	}
	candidate := *role
	candidate.Permissions = append([]string(nil), role.Permissions...)
	candidate.Metadata = maps.Clone(role.Metadata)
	candidate.Scope = role.Scope.Clone()
	return v.validator.ValidateRoleGrant(ctx, types.RoleGrantCheck{
		Operation: operation, Actor: actor, Role: candidate,
		Assignment: types.RoleAssignment{UserID: userID, RoleID: roleID, RoleName: role.Name, Scope: assignmentScope.Clone(), AssignedBy: actor.ID},
	})
}

// NewAssignRoleCommand constructs the handler.
func NewAssignRoleCommand(registry types.RoleRegistry, guard scope.Guard) *AssignRoleCommand {
	return NewAssignRoleCommandWithConfig(RoleAssignmentCommandConfig{Registry: registry, ScopeGuard: guard})
}

// NewAssignRoleCommandWithConfig constructs a handler with grant validation.
// Invalid validator wiring is retained as an execution error.
func NewAssignRoleCommandWithConfig(cfg RoleAssignmentCommandConfig) *AssignRoleCommand {
	return &AssignRoleCommand{
		registry:   cfg.Registry,
		guard:      safeScopeGuard(cfg.ScopeGuard),
		validation: roleGrantValidation{validator: cfg.GrantValidator, err: cfg.Validate()},
	}
}

var _ gocommand.Commander[AssignRoleInput] = (*AssignRoleCommand)(nil)

// Execute assigns the requested role.
func (c *AssignRoleCommand) Execute(ctx context.Context, input AssignRoleInput) error {
	if c == nil || c.registry == nil {
		return types.ErrMissingRoleRegistry
	}
	if c.validation.err != nil {
		return c.validation.err
	}
	if err := input.Validate(); err != nil {
		return err
	}
	scope, err := c.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionRolesWrite, input.RoleID)
	if err != nil {
		return err
	}
	if c.validation.validator != nil {
		scope, err = scope.NormalizeLabels()
		if err != nil {
			return err
		}
	}
	if err := c.validation.enforce(ctx, c.registry, types.RoleGrantAssign, input.Actor, input.UserID, input.RoleID, scope); err != nil {
		return err
	}
	return c.registry.AssignRole(ctx, input.UserID, input.RoleID, scope, input.Actor.ID)
}

// UnassignRoleCommand removes assignments.
type UnassignRoleCommand struct {
	registry   types.RoleRegistry
	guard      scope.Guard
	validation roleGrantValidation
}

// NewUnassignRoleCommand constructs the handler.
func NewUnassignRoleCommand(registry types.RoleRegistry, guard scope.Guard) *UnassignRoleCommand {
	return NewUnassignRoleCommandWithConfig(RoleAssignmentCommandConfig{Registry: registry, ScopeGuard: guard})
}

// NewUnassignRoleCommandWithConfig constructs a revocation handler with grant validation.
func NewUnassignRoleCommandWithConfig(cfg RoleAssignmentCommandConfig) *UnassignRoleCommand {
	return &UnassignRoleCommand{
		registry:   cfg.Registry,
		guard:      safeScopeGuard(cfg.ScopeGuard),
		validation: roleGrantValidation{validator: cfg.GrantValidator, err: cfg.Validate()},
	}
}

var _ gocommand.Commander[UnassignRoleInput] = (*UnassignRoleCommand)(nil)

// Execute removes the given assignment.
func (c *UnassignRoleCommand) Execute(ctx context.Context, input UnassignRoleInput) error {
	if c == nil || c.registry == nil {
		return types.ErrMissingRoleRegistry
	}
	if c.validation.err != nil {
		return c.validation.err
	}
	if err := input.Validate(); err != nil {
		return err
	}
	scope, err := c.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionRolesWrite, input.RoleID)
	if err != nil {
		return err
	}
	if c.validation.validator != nil {
		scope, err = scope.NormalizeLabels()
		if err != nil {
			return err
		}
	}
	if err := c.validation.enforce(ctx, c.registry, types.RoleGrantRevoke, input.Actor, input.UserID, input.RoleID, scope); err != nil {
		return err
	}
	return c.registry.UnassignRole(ctx, input.UserID, input.RoleID, scope, input.Actor.ID)
}
