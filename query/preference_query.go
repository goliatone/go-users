package query

import (
	"context"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/preferences"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

// PreferenceQueryInput scopes preference resolution.
type PreferenceQueryInput struct {
	UserID uuid.UUID
	Scope  types.ScopeFilter
	Levels []types.PreferenceLevel
	Keys   []string
	Base   map[string]any
	Actor  types.ActorRef
}

// PreferenceQuery resolves effective preferences via the injected resolver.
type PreferenceQuery struct {
	resolver preferenceResolver
	guard    scope.Guard
}

type preferenceResolver interface {
	Resolve(ctx context.Context, input preferences.ResolveInput) (types.PreferenceSnapshot, error)
}

// NewPreferenceQuery constructs the query helper.
func NewPreferenceQuery(resolver preferenceResolver, guard scope.Guard) *PreferenceQuery {
	return &PreferenceQuery{
		resolver: resolver,
		guard:    safeScopeGuard(guard),
	}
}

var _ gocommand.Querier[PreferenceQueryInput, types.PreferenceSnapshot] = (*PreferenceQuery)(nil)

// Query resolves preferences for the provided scope chain.
func (q *PreferenceQuery) Query(ctx context.Context, input PreferenceQueryInput) (types.PreferenceSnapshot, error) {
	if q.resolver == nil {
		return types.PreferenceSnapshot{}, types.ErrMissingPreferenceResolver
	}
	scope, err := q.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionPreferencesRead, input.UserID)
	if err != nil {
		return types.PreferenceSnapshot{}, err
	}
	return q.resolver.Resolve(ctx, preferences.ResolveInput{
		UserID: input.UserID,
		Scope:  scope,
		Levels: input.Levels,
		Keys:   input.Keys,
		Base:   input.Base,
	})
}
