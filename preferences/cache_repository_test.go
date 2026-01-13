package preferences

import (
	"context"
	"testing"

	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-repository-cache/cache"
	"github.com/goliatone/go-repository-cache/repositorycache"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestPreferenceRepository_CacheWrapsRepository(t *testing.T) {
	db := newTestDB(t)
	applyDDL(t, db)

	base := newBaseRecordRepository(db)
	repo, err := NewRepository(RepositoryConfig{Repository: base}, WithCache(true))
	require.NoError(t, err)

	_, ok := repo.preferenceStore.(*repositorycache.CachedRepository[*Record])
	require.True(t, ok)
}

func TestPreferenceRepository_CacheDoesNotDoubleWrap(t *testing.T) {
	db := newTestDB(t)
	applyDDL(t, db)

	base := newBaseRecordRepository(db)
	cacheService, err := cache.NewCacheService(cache.DefaultConfig())
	require.NoError(t, err)
	keySerializer := cache.NewDefaultKeySerializer()
	cached := repositorycache.New(base, cacheService, keySerializer)

	repo, err := NewRepository(RepositoryConfig{Repository: cached}, WithCache(true))
	require.NoError(t, err)

	stored, ok := repo.preferenceStore.(*repositorycache.CachedRepository[*Record])
	require.True(t, ok)
	require.Same(t, cached, stored)
}

func TestPreferenceRepository_ListPreferencesUsesCache(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	applyDDL(t, db)

	base := newBaseRecordRepository(db)
	spy := &spyRecordRepository{Repository: base}
	repo, err := NewRepository(RepositoryConfig{Repository: spy}, WithCache(true))
	require.NoError(t, err)

	userID := uuid.New()
	tenantID := uuid.New()
	_, err = repo.UpsertPreference(ctx, types.PreferenceRecord{
		UserID: userID,
		Scope: types.ScopeFilter{
			TenantID: tenantID,
		},
		Level:     types.PreferenceLevelUser,
		Key:       "theme",
		Value:     map[string]any{"mode": "dark"},
		CreatedBy: uuid.New(),
		UpdatedBy: uuid.New(),
	})
	require.NoError(t, err)

	spy.listCalls = 0
	filter := types.PreferenceFilter{
		UserID: userID,
		Scope: types.ScopeFilter{
			TenantID: tenantID,
		},
		Level: types.PreferenceLevelUser,
	}

	_, err = repo.ListPreferences(ctx, filter)
	require.NoError(t, err)
	_, err = repo.ListPreferences(ctx, filter)
	require.NoError(t, err)
	require.Equal(t, 1, spy.listCalls)
}

func TestPreferenceRepository_UpsertInvalidatesCache(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	applyDDL(t, db)

	base := newBaseRecordRepository(db)
	spy := &spyRecordRepository{Repository: base}
	repo, err := NewRepository(RepositoryConfig{Repository: spy}, WithCache(true))
	require.NoError(t, err)

	userID := uuid.New()
	tenantID := uuid.New()
	actor := uuid.New()
	_, err = repo.UpsertPreference(ctx, types.PreferenceRecord{
		UserID: userID,
		Scope: types.ScopeFilter{
			TenantID: tenantID,
		},
		Level:     types.PreferenceLevelUser,
		Key:       "theme",
		Value:     map[string]any{"mode": "dark"},
		CreatedBy: actor,
		UpdatedBy: actor,
	})
	require.NoError(t, err)

	filter := types.PreferenceFilter{
		UserID: userID,
		Scope: types.ScopeFilter{
			TenantID: tenantID,
		},
		Level: types.PreferenceLevelUser,
	}

	_, err = repo.ListPreferences(ctx, filter)
	require.NoError(t, err)

	_, err = repo.UpsertPreference(ctx, types.PreferenceRecord{
		UserID: userID,
		Scope: types.ScopeFilter{
			TenantID: tenantID,
		},
		Level:     types.PreferenceLevelUser,
		Key:       "theme",
		Value:     map[string]any{"mode": "light"},
		UpdatedBy: actor,
	})
	require.NoError(t, err)

	spy.listCalls = 0
	_, err = repo.ListPreferences(ctx, filter)
	require.NoError(t, err)
	require.Equal(t, 1, spy.listCalls)
}

func TestPreferenceRepository_DeleteInvalidatesCache(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	applyDDL(t, db)

	base := newBaseRecordRepository(db)
	spy := &spyRecordRepository{Repository: base}
	repo, err := NewRepository(RepositoryConfig{Repository: spy}, WithCache(true))
	require.NoError(t, err)

	userID := uuid.New()
	tenantID := uuid.New()
	actor := uuid.New()
	_, err = repo.UpsertPreference(ctx, types.PreferenceRecord{
		UserID: userID,
		Scope: types.ScopeFilter{
			TenantID: tenantID,
		},
		Level:     types.PreferenceLevelUser,
		Key:       "theme",
		Value:     map[string]any{"mode": "dark"},
		CreatedBy: actor,
		UpdatedBy: actor,
	})
	require.NoError(t, err)

	filter := types.PreferenceFilter{
		UserID: userID,
		Scope: types.ScopeFilter{
			TenantID: tenantID,
		},
		Level: types.PreferenceLevelUser,
	}

	_, err = repo.ListPreferences(ctx, filter)
	require.NoError(t, err)

	require.NoError(t, repo.DeletePreference(ctx, userID, types.ScopeFilter{TenantID: tenantID}, types.PreferenceLevelUser, "theme"))

	spy.listCalls = 0
	_, err = repo.ListPreferences(ctx, filter)
	require.NoError(t, err)
	require.Equal(t, 1, spy.listCalls)
}

type spyRecordRepository struct {
	repository.Repository[*Record]
	listCalls int
}

func (s *spyRecordRepository) List(ctx context.Context, criteria ...repository.SelectCriteria) ([]*Record, int, error) {
	s.listCalls++
	return s.Repository.List(ctx, criteria...)
}

func newBaseRecordRepository(db *bun.DB) repository.Repository[*Record] {
	return repository.NewRepository(db, repository.ModelHandlers[*Record]{
		NewRecord: func() *Record { return &Record{} },
		GetID: func(rec *Record) uuid.UUID {
			if rec == nil {
				return uuid.Nil
			}
			return rec.ID
		},
		SetID: func(rec *Record, id uuid.UUID) {
			if rec != nil {
				rec.ID = id
			}
		},
	})
}
