package preferences

import (
	"context"
	"testing"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestResolver_ResolveMergesScopes(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()
	systemRecord := types.PreferenceRecord{
		ID:    uuid.New(),
		Key:   "notifications.email",
		Value: map[string]any{"enabled": false},
		Level: types.PreferenceLevelSystem,
	}
	tenantRecord := types.PreferenceRecord{
		ID: uuid.New(),
		Scope: types.ScopeFilter{
			TenantID: tenantID,
		},
		Key:   "notifications.email",
		Value: map[string]any{"frequency": "daily"},
		Level: types.PreferenceLevelTenant,
	}
	userRecord := types.PreferenceRecord{
		ID:     uuid.New(),
		UserID: userID,
		Scope: types.ScopeFilter{
			TenantID: tenantID,
		},
		Key:   "notifications.email",
		Value: map[string]any{"enabled": true},
		Level: types.PreferenceLevelUser,
	}

	repo := &fakePreferenceRepo{
		values: map[types.PreferenceLevel][]types.PreferenceRecord{
			types.PreferenceLevelSystem: {systemRecord},
			types.PreferenceLevelTenant: {tenantRecord},
			types.PreferenceLevelUser:   {userRecord},
		},
	}
	resolver, err := NewResolver(ResolverConfig{Repository: repo})
	require.NoError(t, err)

	snapshot, err := resolver.Resolve(context.Background(), ResolveInput{
		UserID: userID,
		Scope: types.ScopeFilter{
			TenantID: tenantID,
		},
		Keys: []string{"notifications.email"},
	})
	require.NoError(t, err)
	value := snapshot.Effective["notifications.email"].(map[string]any)
	require.True(t, value["enabled"].(bool))
	require.Equal(t, "daily", value["frequency"])

	require.Len(t, snapshot.Traces, 1)
	trace := snapshot.Traces[0]
	require.Equal(t, "notifications.email", trace.Key)
	require.Len(t, trace.Layers, 3)
	require.Equal(t, types.PreferenceLevelSystem, trace.Layers[0].Level)
	require.Equal(t, types.PreferenceLevelUser, trace.Layers[len(trace.Layers)-1].Level)
	require.Equal(t, userRecord.ID.String(), trace.Layers[len(trace.Layers)-1].SnapshotID)
}

type fakePreferenceRepo struct {
	values map[types.PreferenceLevel][]types.PreferenceRecord
}

func (f *fakePreferenceRepo) ListPreferences(_ context.Context, filter types.PreferenceFilter) ([]types.PreferenceRecord, error) {
	records := f.values[filter.Level]
	if len(filter.Keys) == 0 {
		return append([]types.PreferenceRecord(nil), records...), nil
	}
	result := make([]types.PreferenceRecord, 0, len(filter.Keys))
	for _, record := range records {
		for _, key := range filter.Keys {
			if record.Key == key {
				result = append(result, record)
			}
		}
	}
	return result, nil
}

func (f *fakePreferenceRepo) UpsertPreference(_ context.Context, record types.PreferenceRecord) (*types.PreferenceRecord, error) {
	return &record, nil
}

func (f *fakePreferenceRepo) DeletePreference(_ context.Context, _ uuid.UUID, _ types.ScopeFilter, _ types.PreferenceLevel, _ string) error {
	return nil
}
