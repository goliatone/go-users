package query

import (
	"context"
	"errors"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

var errRoleIDRequired = errors.New("go-users: role id required")

// RoleListQuery lists custom roles for admin surfaces.
type RoleListQuery struct {
	registry types.RoleRegistry
	guard    scope.Guard
}

// NewRoleListQuery builds the list query.
func NewRoleListQuery(registry types.RoleRegistry, guard scope.Guard) *RoleListQuery {
	return &RoleListQuery{
		registry: registry,
		guard:    safeScopeGuard(guard),
	}
}

var _ gocommand.Querier[types.RoleFilter, types.RolePage] = (*RoleListQuery)(nil)

// Query forwards to the registry.
func (q *RoleListQuery) Query(ctx context.Context, filter types.RoleFilter) (types.RolePage, error) {
	if q.registry == nil {
		return types.RolePage{}, types.ErrMissingRoleRegistry
	}
	if err := filter.Validate(); err != nil {
		return types.RolePage{}, err
	}
	scope, err := q.guard.Enforce(ctx, filter.Actor, filter.Scope, types.PolicyActionRolesRead, uuid.Nil)
	if err != nil {
		return types.RolePage{}, err
	}
	filter.Scope = scope
	return q.registry.ListRoles(ctx, filter)
}

// RoleDetailInput fetches a single role by ID.
type RoleDetailInput struct {
	RoleID uuid.UUID
	Scope  types.ScopeFilter
	Actor  types.ActorRef
}

// Type implements gocommand.Message.
func (RoleDetailInput) Type() string {
	return "query.role.detail"
}

// Validate implements gocommand.Message.
func (input RoleDetailInput) Validate() error {
	switch {
	case input.RoleID == uuid.Nil:
		return errRoleIDRequired
	case input.Actor.ID == uuid.Nil:
		return types.ErrActorRequired
	default:
		return nil
	}
}

// RoleDetailQuery loads custom role metadata.
type RoleDetailQuery struct {
	registry types.RoleRegistry
	guard    scope.Guard
}

// NewRoleDetailQuery constructs the detail query.
func NewRoleDetailQuery(registry types.RoleRegistry, guard scope.Guard) *RoleDetailQuery {
	return &RoleDetailQuery{
		registry: registry,
		guard:    safeScopeGuard(guard),
	}
}

var _ gocommand.Querier[RoleDetailInput, *types.RoleDefinition] = (*RoleDetailQuery)(nil)

// Query fetches role detail.
func (q *RoleDetailQuery) Query(ctx context.Context, input RoleDetailInput) (*types.RoleDefinition, error) {
	if q.registry == nil {
		return nil, types.ErrMissingRoleRegistry
	}
	if err := input.Validate(); err != nil {
		return nil, err
	}
	scope, err := q.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionRolesRead, input.RoleID)
	if err != nil {
		return nil, err
	}
	return q.registry.GetRole(ctx, input.RoleID, scope)
}

// RoleAssignmentsQuery lists role assignments filtered by scope/user/role.
type RoleAssignmentsQuery struct {
	registry types.RoleRegistry
	guard    scope.Guard
}

// NewRoleAssignmentsQuery constructs the query helper.
func NewRoleAssignmentsQuery(registry types.RoleRegistry, guard scope.Guard) *RoleAssignmentsQuery {
	return &RoleAssignmentsQuery{
		registry: registry,
		guard:    safeScopeGuard(guard),
	}
}

var _ gocommand.Querier[types.RoleAssignmentFilter, []types.RoleAssignment] = (*RoleAssignmentsQuery)(nil)

// Query returns assignments from the registry.
func (q *RoleAssignmentsQuery) Query(ctx context.Context, filter types.RoleAssignmentFilter) ([]types.RoleAssignment, error) {
	if q.registry == nil {
		return nil, types.ErrMissingRoleRegistry
	}
	if err := filter.Validate(); err != nil {
		return nil, err
	}
	scope, err := q.guard.Enforce(ctx, filter.Actor, filter.Scope, types.PolicyActionRolesRead, uuid.Nil)
	if err != nil {
		return nil, err
	}
	filter.Scope = scope
	return q.registry.ListAssignments(ctx, filter)
}
