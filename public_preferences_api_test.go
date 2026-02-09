package users_test

import (
	"context"
	"testing"

	"github.com/goliatone/go-users/command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/preferences"
	"github.com/goliatone/go-users/query"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPublicPreferenceAPIsCompile(t *testing.T) {
	repo := &publicPreferenceRepo{}
	resolver, err := preferences.NewResolver(preferences.ResolverConfig{Repository: repo})
	require.NoError(t, err)

	_, err = resolver.Resolve(context.Background(), preferences.ResolveInput{
		UserID:          uuid.New(),
		Scope:           types.ScopeFilter{TenantID: uuid.New()},
		OutputMode:      types.PreferenceOutputRawValue,
		IncludeVersions: true,
	})
	require.NoError(t, err)

	_ = command.PreferenceUpsertManyInput{
		UserID: uuid.New(),
		Scope:  types.ScopeFilter{TenantID: uuid.New()},
		Level:  types.PreferenceLevelUser,
		Values: map[string]any{"theme": "dark"},
		Actor:  types.ActorRef{ID: uuid.New()},
		Mode:   types.PreferenceBulkModeBestEffort,
	}
	_ = command.PreferenceDeleteManyInput{
		UserID: uuid.New(),
		Scope:  types.ScopeFilter{TenantID: uuid.New()},
		Level:  types.PreferenceLevelUser,
		Keys:   []string{"theme"},
		Actor:  types.ActorRef{ID: uuid.New()},
		Mode:   types.PreferenceBulkModeBestEffort,
	}
	_ = query.PreferenceQueryInput{
		UserID:          uuid.New(),
		Scope:           types.ScopeFilter{TenantID: uuid.New()},
		OutputMode:      types.PreferenceOutputRawValue,
		IncludeVersions: true,
		Actor:           types.ActorRef{ID: uuid.New()},
	}
	_ = types.PreferenceSnapshot{EffectiveVersions: map[string]int{"theme": 1}}
	_ = types.PreferenceTraceLayer{Version: 1}
}

type publicPreferenceRepo struct{}

func (p *publicPreferenceRepo) ListPreferences(context.Context, types.PreferenceFilter) ([]types.PreferenceRecord, error) {
	return nil, nil
}

func (p *publicPreferenceRepo) UpsertPreference(_ context.Context, record types.PreferenceRecord) (*types.PreferenceRecord, error) {
	copy := record
	if copy.ID == uuid.Nil {
		copy.ID = uuid.New()
	}
	return &copy, nil
}

func (p *publicPreferenceRepo) DeletePreference(context.Context, uuid.UUID, types.ScopeFilter, types.PreferenceLevel, string) error {
	return nil
}

func (p *publicPreferenceRepo) UpsertManyPreferences(_ context.Context, records []types.PreferenceRecord, _ types.PreferenceBulkMode) ([]types.PreferenceRecord, error) {
	out := make([]types.PreferenceRecord, 0, len(records))
	for _, record := range records {
		copy := record
		if copy.ID == uuid.Nil {
			copy.ID = uuid.New()
		}
		out = append(out, copy)
	}
	return out, nil
}

func (p *publicPreferenceRepo) DeleteManyPreferences(context.Context, uuid.UUID, types.ScopeFilter, types.PreferenceLevel, []string, types.PreferenceBulkMode) error {
	return nil
}
