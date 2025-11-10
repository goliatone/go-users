package query

import (
	"context"
	"testing"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/preferences"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPreferenceQuery_DelegatesToResolver(t *testing.T) {
	resolver := &fakeResolver{}
	query := NewPreferenceQuery(resolver, nil)

	result, err := query.Query(context.Background(), PreferenceQueryInput{
		UserID: uuid.New(),
	})
	require.NoError(t, err)
	require.True(t, resolver.called)
	require.Equal(t, resolver.snapshot, result)
}

type fakeResolver struct {
	called   bool
	snapshot types.PreferenceSnapshot
}

func (f *fakeResolver) Resolve(context.Context, preferences.ResolveInput) (types.PreferenceSnapshot, error) {
	f.called = true
	f.snapshot = types.PreferenceSnapshot{
		Effective: map[string]any{"key": "value"},
	}
	return f.snapshot, nil
}
