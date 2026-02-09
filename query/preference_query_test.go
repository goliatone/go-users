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
		UserID:          uuid.New(),
		Keys:            []string{"theme"},
		Levels:          []types.PreferenceLevel{types.PreferenceLevelUser},
		Base:            map[string]any{"theme": map[string]any{"value": "light"}},
		OutputMode:      types.PreferenceOutputRawValue,
		IncludeVersions: true,
		Actor: types.ActorRef{
			ID: uuid.New(),
		},
	})
	require.NoError(t, err)
	require.True(t, resolver.called)
	require.Equal(t, resolver.snapshot, result)
	require.Equal(t, types.PreferenceOutputRawValue, resolver.lastInput.OutputMode)
	require.True(t, resolver.lastInput.IncludeVersions)
	require.Equal(t, []string{"theme"}, resolver.lastInput.Keys)
}

type fakeResolver struct {
	called    bool
	lastInput preferences.ResolveInput
	snapshot  types.PreferenceSnapshot
}

func (f *fakeResolver) Resolve(_ context.Context, input preferences.ResolveInput) (types.PreferenceSnapshot, error) {
	f.called = true
	f.lastInput = input
	f.snapshot = types.PreferenceSnapshot{
		Effective: map[string]any{"key": "value"},
	}
	return f.snapshot, nil
}
