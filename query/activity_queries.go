package query

import (
	"context"

	"github.com/goliatone/go-auth"
	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/activity"
	"github.com/goliatone/go-users/pkg/authctx"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

type activityQueryConfig struct {
	policy activity.ActivityAccessPolicy
}

// ActivityQueryOption customizes activity query behavior.
type ActivityQueryOption func(*activityQueryConfig)

// WithActivityAccessPolicy attaches a policy to activity queries.
func WithActivityAccessPolicy(policy activity.ActivityAccessPolicy) ActivityQueryOption {
	return func(cfg *activityQueryConfig) {
		if cfg == nil {
			return
		}
		cfg.policy = policy
	}
}

func applyActivityQueryOptions(opts []ActivityQueryOption) activityQueryConfig {
	cfg := activityQueryConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}

// ActivityFeedQuery renders paginated activity feeds for dashboards.
type ActivityFeedQuery struct {
	repo   types.ActivityRepository
	guard  scope.Guard
	policy activity.ActivityAccessPolicy
}

// NewActivityFeedQuery constructs the feed query helper.
func NewActivityFeedQuery(repo types.ActivityRepository, guard scope.Guard, opts ...ActivityQueryOption) *ActivityFeedQuery {
	cfg := applyActivityQueryOptions(opts)
	return &ActivityFeedQuery{
		repo:   repo,
		guard:  safeScopeGuard(guard),
		policy: cfg.policy,
	}
}

var _ gocommand.Querier[types.ActivityFilter, types.ActivityPage] = (*ActivityFeedQuery)(nil)

// Query fetches a page of activity logs via the injected repository.
func (q *ActivityFeedQuery) Query(ctx context.Context, filter types.ActivityFilter) (types.ActivityPage, error) {
	if q.repo == nil {
		return types.ActivityPage{}, types.ErrMissingActivityRepository
	}
	var actorCtx *auth.ActorContext
	if q.policy != nil {
		var err error
		actorCtx, err = authctx.ResolveActorContext(ctx)
		if err != nil {
			return types.ActivityPage{}, err
		}
		filter, err = q.policy.Apply(actorCtx, "", filter)
		if err != nil {
			return types.ActivityPage{}, err
		}
	}
	if err := filter.Validate(); err != nil {
		return types.ActivityPage{}, err
	}
	scope, err := q.guard.Enforce(ctx, filter.Actor, filter.Scope, types.PolicyActionActivityRead, uuid.Nil)
	if err != nil {
		return types.ActivityPage{}, err
	}
	filter.Scope = scope
	page, err := q.repo.ListActivity(ctx, filter)
	if err != nil {
		return types.ActivityPage{}, err
	}
	if q.policy != nil {
		page.Records = q.policy.Sanitize(actorCtx, "", page.Records)
	}
	return page, nil
}

// ActivityStatsQuery aggregates activity counts per verb.
type ActivityStatsQuery struct {
	repo   types.ActivityRepository
	guard  scope.Guard
	policy activity.ActivityAccessPolicy
}

// NewActivityStatsQuery constructs the stats helper.
func NewActivityStatsQuery(repo types.ActivityRepository, guard scope.Guard, opts ...ActivityQueryOption) *ActivityStatsQuery {
	cfg := applyActivityQueryOptions(opts)
	return &ActivityStatsQuery{
		repo:   repo,
		guard:  safeScopeGuard(guard),
		policy: cfg.policy,
	}
}

var _ gocommand.Querier[types.ActivityStatsFilter, types.ActivityStats] = (*ActivityStatsQuery)(nil)

// Query returns aggregate counts for UI widgets.
func (q *ActivityStatsQuery) Query(ctx context.Context, filter types.ActivityStatsFilter) (types.ActivityStats, error) {
	if q.repo == nil {
		return types.ActivityStats{}, types.ErrMissingActivityRepository
	}
	if q.policy != nil {
		actorCtx, err := authctx.ResolveActorContext(ctx)
		if err != nil {
			return types.ActivityStats{}, err
		}
		if statsPolicy, ok := q.policy.(activity.ActivityStatsPolicy); ok {
			filter, err = statsPolicy.ApplyStats(actorCtx, "", filter)
			if err != nil {
				return types.ActivityStats{}, err
			}
		} else {
			policyFilter, err := q.policy.Apply(actorCtx, "", types.ActivityFilter{
				Actor: filter.Actor,
				Scope: filter.Scope,
			})
			if err != nil {
				return types.ActivityStats{}, err
			}
			filter.Actor = policyFilter.Actor
			filter.Scope = policyFilter.Scope
			filter.MachineActivityEnabled = policyFilter.MachineActivityEnabled
			filter.MachineActorTypes = policyFilter.MachineActorTypes
			filter.MachineDataKeys = policyFilter.MachineDataKeys
		}
	}
	if err := filter.Validate(); err != nil {
		return types.ActivityStats{}, err
	}
	scope, err := q.guard.Enforce(ctx, filter.Actor, filter.Scope, types.PolicyActionActivityRead, uuid.Nil)
	if err != nil {
		return types.ActivityStats{}, err
	}
	filter.Scope = scope
	return q.repo.ActivityStats(ctx, filter)
}
