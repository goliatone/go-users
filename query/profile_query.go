package query

import (
	"context"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

// ProfileQueryInput scopes profile lookups.
type ProfileQueryInput struct {
	UserID uuid.UUID
	Scope  types.ScopeFilter
	Actor  types.ActorRef
}

// ProfileQuery fetches user profile records.
type ProfileQuery struct {
	repo  types.ProfileRepository
	guard scope.Guard
}

// NewProfileQuery constructs the profile query helper.
func NewProfileQuery(repo types.ProfileRepository, guard scope.Guard) *ProfileQuery {
	return &ProfileQuery{
		repo:  repo,
		guard: safeScopeGuard(guard),
	}
}

var _ gocommand.Querier[ProfileQueryInput, *types.UserProfile] = (*ProfileQuery)(nil)

// Query returns the profile for the supplied identifiers.
func (q *ProfileQuery) Query(ctx context.Context, input ProfileQueryInput) (*types.UserProfile, error) {
	if q.repo == nil {
		return nil, types.ErrMissingProfileRepository
	}
	if input.UserID == uuid.Nil {
		return nil, types.ErrUserIDRequired
	}
	scope, err := q.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionProfilesRead, input.UserID)
	if err != nil {
		return nil, err
	}
	return q.repo.GetProfile(ctx, input.UserID, scope)
}
