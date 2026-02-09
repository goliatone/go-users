package command

import (
	"context"
	"errors"
	"testing"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPreferenceUpsertManyCommand_BestEffort(t *testing.T) {
	repo := &bulkRepoStub{}
	cmd := NewPreferenceUpsertManyCommand(PreferenceCommandConfig{Repository: repo})
	actor := types.ActorRef{ID: uuid.New()}
	userID := uuid.New()

	var results []types.PreferenceBulkUpsertResult
	err := cmd.Execute(context.Background(), PreferenceUpsertManyInput{
		UserID: userID,
		Level:  types.PreferenceLevelUser,
		Actor:  actor,
		Values: map[string]any{
			"theme": "dark",
			"mail":  map[string]any{"enabled": true},
		},
		Results: &results,
	})
	require.NoError(t, err)
	require.Len(t, repo.upserted, 2)
	require.Len(t, results, 2)
	require.Equal(t, "dark", repo.byKey("theme").Value["value"])
	require.True(t, repo.byKey("mail").Value["enabled"].(bool))
}

func TestPreferenceDeleteManyCommand_BestEffort(t *testing.T) {
	repo := &bulkRepoStub{}
	cmd := NewPreferenceDeleteManyCommand(PreferenceCommandConfig{Repository: repo})

	var results []types.PreferenceBulkDeleteResult
	err := cmd.Execute(context.Background(), PreferenceDeleteManyInput{
		UserID:  uuid.New(),
		Level:   types.PreferenceLevelUser,
		Actor:   types.ActorRef{ID: uuid.New()},
		Keys:    []string{"theme", "locale"},
		Results: &results,
	})
	require.NoError(t, err)
	require.Len(t, repo.deletedKeys, 2)
	require.Len(t, results, 2)
}

func TestPreferenceBulkCommands_TransactionalRequiresBulkRepository(t *testing.T) {
	repo := &singlePreferenceRepoStub{}
	upsert := NewPreferenceUpsertManyCommand(PreferenceCommandConfig{Repository: repo})
	deleteCmd := NewPreferenceDeleteManyCommand(PreferenceCommandConfig{Repository: repo})
	actor := types.ActorRef{ID: uuid.New()}

	err := upsert.Execute(context.Background(), PreferenceUpsertManyInput{
		UserID: uuid.New(),
		Level:  types.PreferenceLevelUser,
		Actor:  actor,
		Mode:   types.PreferenceBulkModeTransactional,
		Values: map[string]any{"theme": "dark"},
	})
	require.ErrorIs(t, err, types.ErrPreferenceBulkTransactionalUnsupported)

	err = deleteCmd.Execute(context.Background(), PreferenceDeleteManyInput{
		UserID: uuid.New(),
		Level:  types.PreferenceLevelUser,
		Actor:  actor,
		Mode:   types.PreferenceBulkModeTransactional,
		Keys:   []string{"theme"},
	})
	require.ErrorIs(t, err, types.ErrPreferenceBulkTransactionalUnsupported)
}

func TestPreferenceUpsertManyCommand_TransactionalUsesBulkRepository(t *testing.T) {
	repo := &bulkRepoStub{}
	cmd := NewPreferenceUpsertManyCommand(PreferenceCommandConfig{Repository: repo})
	actor := types.ActorRef{ID: uuid.New()}

	err := cmd.Execute(context.Background(), PreferenceUpsertManyInput{
		UserID: uuid.New(),
		Level:  types.PreferenceLevelUser,
		Actor:  actor,
		Mode:   types.PreferenceBulkModeTransactional,
		Values: map[string]any{"theme": "dark"},
	})
	require.NoError(t, err)
	require.True(t, repo.transactionalUpsertCalled)
}

func TestPreferenceDeleteManyCommand_TransactionalUsesBulkRepository(t *testing.T) {
	repo := &bulkRepoStub{}
	cmd := NewPreferenceDeleteManyCommand(PreferenceCommandConfig{Repository: repo})
	actor := types.ActorRef{ID: uuid.New()}

	err := cmd.Execute(context.Background(), PreferenceDeleteManyInput{
		UserID: uuid.New(),
		Level:  types.PreferenceLevelUser,
		Actor:  actor,
		Mode:   types.PreferenceBulkModeTransactional,
		Keys:   []string{"theme"},
	})
	require.NoError(t, err)
	require.True(t, repo.transactionalDeleteCalled)
}

type singlePreferenceRepoStub struct{}

func (s *singlePreferenceRepoStub) ListPreferences(context.Context, types.PreferenceFilter) ([]types.PreferenceRecord, error) {
	return nil, nil
}

func (s *singlePreferenceRepoStub) UpsertPreference(_ context.Context, record types.PreferenceRecord) (*types.PreferenceRecord, error) {
	copy := record
	copy.ID = uuid.New()
	return &copy, nil
}

func (s *singlePreferenceRepoStub) DeletePreference(context.Context, uuid.UUID, types.ScopeFilter, types.PreferenceLevel, string) error {
	return nil
}

type bulkRepoStub struct {
	upserted                  []types.PreferenceRecord
	deletedKeys               []string
	transactionalUpsertCalled bool
	transactionalDeleteCalled bool
}

func (s *bulkRepoStub) ListPreferences(context.Context, types.PreferenceFilter) ([]types.PreferenceRecord, error) {
	return nil, nil
}

func (s *bulkRepoStub) UpsertPreference(_ context.Context, record types.PreferenceRecord) (*types.PreferenceRecord, error) {
	copy := record
	copy.ID = uuid.New()
	s.upserted = append(s.upserted, copy)
	return &copy, nil
}

func (s *bulkRepoStub) DeletePreference(_ context.Context, _ uuid.UUID, _ types.ScopeFilter, _ types.PreferenceLevel, key string) error {
	if key == "fail" {
		return errors.New("forced failure")
	}
	s.deletedKeys = append(s.deletedKeys, key)
	return nil
}

func (s *bulkRepoStub) UpsertManyPreferences(_ context.Context, records []types.PreferenceRecord, mode types.PreferenceBulkMode) ([]types.PreferenceRecord, error) {
	if mode == types.PreferenceBulkModeTransactional {
		s.transactionalUpsertCalled = true
	}
	result := make([]types.PreferenceRecord, 0, len(records))
	for _, record := range records {
		copy := record
		copy.ID = uuid.New()
		result = append(result, copy)
	}
	return result, nil
}

func (s *bulkRepoStub) DeleteManyPreferences(_ context.Context, _ uuid.UUID, _ types.ScopeFilter, _ types.PreferenceLevel, keys []string, mode types.PreferenceBulkMode) error {
	if mode == types.PreferenceBulkModeTransactional {
		s.transactionalDeleteCalled = true
	}
	s.deletedKeys = append(s.deletedKeys, keys...)
	return nil
}

func (s *bulkRepoStub) byKey(key string) types.PreferenceRecord {
	for _, record := range s.upserted {
		if record.Key == key {
			return record
		}
	}
	return types.PreferenceRecord{}
}
