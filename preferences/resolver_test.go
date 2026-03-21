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

func TestResolver_ResolveRawValueModeUnwrapsEnvelope(t *testing.T) {
	userID := uuid.New()
	systemRecord := types.PreferenceRecord{
		ID:      uuid.New(),
		Key:     "ui.theme",
		Value:   map[string]any{"value": "light"},
		Level:   types.PreferenceLevelSystem,
		Version: 1,
	}
	userRecord := types.PreferenceRecord{
		ID:      uuid.New(),
		UserID:  userID,
		Key:     "ui.theme",
		Value:   map[string]any{"value": "dark"},
		Level:   types.PreferenceLevelUser,
		Version: 2,
	}
	structured := types.PreferenceRecord{
		ID:      uuid.New(),
		UserID:  userID,
		Key:     "notifications",
		Value:   map[string]any{"enabled": true},
		Level:   types.PreferenceLevelUser,
		Version: 1,
	}
	repo := &fakePreferenceRepo{
		values: map[types.PreferenceLevel][]types.PreferenceRecord{
			types.PreferenceLevelSystem: {systemRecord},
			types.PreferenceLevelUser:   {userRecord, structured},
		},
	}
	resolver, err := NewResolver(ResolverConfig{Repository: repo})
	require.NoError(t, err)

	snapshot, err := resolver.Resolve(context.Background(), ResolveInput{
		UserID:     userID,
		OutputMode: types.PreferenceOutputRawValue,
	})
	require.NoError(t, err)
	require.Equal(t, "dark", snapshot.Effective["ui.theme"])
	notifications, ok := snapshot.Effective["notifications"].(map[string]any)
	require.True(t, ok)
	require.True(t, notifications["enabled"].(bool))
}

func TestResolver_ResolveIncludesEffectiveVersions(t *testing.T) {
	userID := uuid.New()
	systemRecord := types.PreferenceRecord{
		ID:      uuid.New(),
		Key:     "locale",
		Value:   map[string]any{"value": "en_us"},
		Level:   types.PreferenceLevelSystem,
		Version: 3,
	}
	userRecord := types.PreferenceRecord{
		ID:      uuid.New(),
		UserID:  userID,
		Key:     "locale",
		Value:   map[string]any{"value": "es-MX"},
		Level:   types.PreferenceLevelUser,
		Version: 8,
	}
	repo := &fakePreferenceRepo{
		values: map[types.PreferenceLevel][]types.PreferenceRecord{
			types.PreferenceLevelSystem: {systemRecord},
			types.PreferenceLevelUser:   {userRecord},
		},
	}
	resolver, err := NewResolver(ResolverConfig{Repository: repo})
	require.NoError(t, err)

	snapshot, err := resolver.Resolve(context.Background(), ResolveInput{
		UserID:          userID,
		OutputMode:      types.PreferenceOutputRawValue,
		IncludeVersions: true,
	})
	require.NoError(t, err)
	require.Equal(t, "es-MX", snapshot.Effective["locale"])
	require.Equal(t, 8, snapshot.EffectiveVersions["locale"])

	require.Len(t, snapshot.Traces, 1)
	layerVersions := make([]int, 0, len(snapshot.Traces[0].Layers))
	for _, layer := range snapshot.Traces[0].Layers {
		if layer.Found {
			layerVersions = append(layerVersions, layer.Version)
		}
	}
	require.Contains(t, layerVersions, 3)
	require.Contains(t, layerVersions, 8)
}

func TestResolver_ResolveNormalizesLocalePreferencePayloadFields(t *testing.T) {
	userID := uuid.New()
	userRecord := types.PreferenceRecord{
		ID:     uuid.New(),
		UserID: userID,
		Key:    "locale",
		Value: map[string]any{
			"language": "en_us",
			"timezone": "America/Los_Angeles",
		},
		Level: types.PreferenceLevelUser,
	}
	repo := &fakePreferenceRepo{
		values: map[types.PreferenceLevel][]types.PreferenceRecord{
			types.PreferenceLevelUser: {userRecord},
		},
	}
	resolver, err := NewResolver(ResolverConfig{Repository: repo})
	require.NoError(t, err)

	snapshot, err := resolver.Resolve(context.Background(), ResolveInput{
		UserID:     userID,
		OutputMode: types.PreferenceOutputEnvelope,
	})
	require.NoError(t, err)

	localeValue, ok := snapshot.Effective["locale"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "en-US", localeValue["language"])
	require.Equal(t, "America/Los_Angeles", localeValue["timezone"])
}

func TestResolver_ResolveRejectsUnknownOutputMode(t *testing.T) {
	repo := &fakePreferenceRepo{}
	resolver, err := NewResolver(ResolverConfig{Repository: repo})
	require.NoError(t, err)

	_, err = resolver.Resolve(context.Background(), ResolveInput{
		OutputMode: types.PreferenceOutputMode("unsupported"),
	})
	require.ErrorIs(t, err, types.ErrUnsupportedPreferenceOutputMode)
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
