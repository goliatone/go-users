package activity

import (
	"context"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

// Local G5 reproduction: an unlabeled legacy row must not enter a Group feed.
func TestG5NativeActivityGroupIsolation(t *testing.T) {
	db := newTestActivityDB(t)
	applyActivityDDL(t, db)
	r, err := NewRepository(RepositoryConfig{DB: db})
	require.NoError(t, err)
	require.NoError(t, r.Log(context.Background(), types.ActivityRecord{Verb: "legacy.event"}))
	page, err := r.ListActivity(context.Background(), types.ActivityFilter{Scope: types.ScopeFilter{}.WithLabel("group", uuid.New())})
	require.NoError(t, err)
	require.Zero(t, page.Total, "Group SQL filtering must exclude unlabeled legacy rows")
}
