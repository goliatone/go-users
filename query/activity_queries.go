package query

import (
	"context"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

// ActivityFeedQuery renders paginated activity feeds for dashboards.
type ActivityFeedQuery struct {
	repo  types.ActivityRepository
	guard scope.Guard
}

// NewActivityFeedQuery constructs the feed query helper.
func NewActivityFeedQuery(repo types.ActivityRepository, guard scope.Guard) *ActivityFeedQuery {
	return &ActivityFeedQuery{
		repo:  repo,
		guard: safeScopeGuard(guard),
	}
}

var _ gocommand.Querier[types.ActivityFilter, types.ActivityPage] = (*ActivityFeedQuery)(nil)

// Query fetches a page of activity logs via the injected repository.
func (q *ActivityFeedQuery) Query(ctx context.Context, filter types.ActivityFilter) (types.ActivityPage, error) {
	if q.repo == nil {
		return types.ActivityPage{}, types.ErrMissingActivityRepository
	}
	scope, err := q.guard.Enforce(ctx, filter.Actor, filter.Scope, types.PolicyActionActivityRead, uuid.Nil)
	if err != nil {
		return types.ActivityPage{}, err
	}
	filter.Scope = scope
	return q.repo.ListActivity(ctx, filter)
}

// ActivityStatsQuery aggregates activity counts per verb.
type ActivityStatsQuery struct {
	repo  types.ActivityRepository
	guard scope.Guard
}

// NewActivityStatsQuery constructs the stats helper.
func NewActivityStatsQuery(repo types.ActivityRepository, guard scope.Guard) *ActivityStatsQuery {
	return &ActivityStatsQuery{
		repo:  repo,
		guard: safeScopeGuard(guard),
	}
}

var _ gocommand.Querier[types.ActivityStatsFilter, types.ActivityStats] = (*ActivityStatsQuery)(nil)

// Query returns aggregate counts for UI widgets.
func (q *ActivityStatsQuery) Query(ctx context.Context, filter types.ActivityStatsFilter) (types.ActivityStats, error) {
	if q.repo == nil {
		return types.ActivityStats{}, types.ErrMissingActivityRepository
	}
	scope, err := q.guard.Enforce(ctx, filter.Actor, filter.Scope, types.PolicyActionActivityRead, uuid.Nil)
	if err != nil {
		return types.ActivityStats{}, err
	}
	filter.Scope = scope
	return q.repo.ActivityStats(ctx, filter)
}
