package query

import (
	"context"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

const (
	defaultInventoryLimit = 50
	maxInventoryLimit     = 200
)

// UserInventoryQuery wraps ListUsers repositories and normalizes filters for
// admin dashboards.
type UserInventoryQuery struct {
	repo   types.UserInventoryRepository
	logger types.Logger
	guard  scope.Guard
}

// NewUserInventoryQuery constructs the query helper.
func NewUserInventoryQuery(repo types.UserInventoryRepository, logger types.Logger, guard scope.Guard) *UserInventoryQuery {
	return &UserInventoryQuery{
		repo:   repo,
		logger: logger,
		guard:  safeScopeGuard(guard),
	}
}

var _ gocommand.Querier[types.UserInventoryFilter, types.UserInventoryPage] = (*UserInventoryQuery)(nil)

// Query delegates to the configured repository after normalizing filters.
func (q *UserInventoryQuery) Query(ctx context.Context, filter types.UserInventoryFilter) (types.UserInventoryPage, error) {
	if q.repo == nil {
		return types.UserInventoryPage{}, types.ErrMissingInventoryRepository
	}
	if err := filter.Validate(); err != nil {
		return types.UserInventoryPage{}, err
	}
	scope, err := q.guard.Enforce(ctx, filter.Actor, filter.Scope, types.PolicyActionUsersRead, uuid.Nil)
	if err != nil {
		return types.UserInventoryPage{}, err
	}
	filter.Scope = scope
	normalized := normalizeInventoryFilter(filter)
	return q.repo.ListUsers(ctx, normalized)
}

func normalizeInventoryFilter(filter types.UserInventoryFilter) types.UserInventoryFilter {
	out := filter
	if out.Pagination.Limit <= 0 {
		out.Pagination.Limit = defaultInventoryLimit
	}
	if out.Pagination.Limit > maxInventoryLimit {
		out.Pagination.Limit = maxInventoryLimit
	}
	if out.Pagination.Offset < 0 {
		out.Pagination.Offset = 0
	}
	return out
}
