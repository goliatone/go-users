package query

import (
	"context"
	"testing"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestUserInventoryQuery_NormalizesFilters(t *testing.T) {
	repo := &recordingInventoryRepo{
		page: types.UserInventoryPage{
			Users: []types.AuthUser{
				{Email: "one@example.com"},
			},
			Total: 1,
		},
	}
	query := NewUserInventoryQuery(repo, types.NopLogger{}, nil)

	scope := types.ScopeFilter{
		TenantID: uuid.New(),
	}
	filter := types.UserInventoryFilter{
		Actor: types.ActorRef{
			ID: uuid.New(),
		},
		Scope: scope,
		// Negative offset and zero limit should be corrected.
		Pagination: types.Pagination{
			Limit:  0,
			Offset: -10,
		},
	}

	page, err := query.Query(context.Background(), filter)

	require.NoError(t, err)
	require.Equal(t, defaultInventoryLimit, repo.lastFilter.Pagination.Limit)
	require.Equal(t, 0, repo.lastFilter.Pagination.Offset)
	require.Equal(t, scope, repo.lastFilter.Scope)
	require.Equal(t, repo.page, page)
}

type recordingInventoryRepo struct {
	page       types.UserInventoryPage
	lastFilter types.UserInventoryFilter
}

func (r *recordingInventoryRepo) ListUsers(_ context.Context, filter types.UserInventoryFilter) (types.UserInventoryPage, error) {
	r.lastFilter = filter
	return r.page, nil
}
