package activity

import (
	"context"
	"testing"
	"time"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRepository_LogAppliesEnricher(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	db := newTestActivityDB(t)
	applyActivityDDL(t, db)

	enricher := EnricherFunc(func(_ context.Context, rec types.ActivityRecord) (types.ActivityRecord, error) {
		rec.Data = cloneMetadata(rec.Data)
		rec.Data["enriched"] = true
		return rec, nil
	})

	store, err := NewRepository(RepositoryConfig{DB: db, Enricher: enricher})
	require.NoError(t, err)

	recordID := uuid.New()
	err = store.Log(ctx, types.ActivityRecord{
		ID:         recordID,
		Verb:       "activity.test",
		ObjectType: "test",
		ObjectID:   "1",
	})
	require.NoError(t, err)

	page, err := store.ListActivity(ctx, types.ActivityFilter{
		Pagination: types.Pagination{Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, page.Records, 1)
	require.Equal(t, recordID, page.Records[0].ID)
	require.Equal(t, true, page.Records[0].Data["enriched"])
}

func TestRepository_UpdateActivityDataMissingKeys(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	db := newTestActivityDB(t)
	applyActivityDDL(t, db)

	store, err := NewRepository(RepositoryConfig{DB: db})
	require.NoError(t, err)

	recordID := uuid.New()
	err = store.Log(ctx, types.ActivityRecord{
		ID:         recordID,
		Verb:       "activity.test",
		ObjectType: "test",
		ObjectID:   "1",
		Data: map[string]any{
			"actor_display": "Alice",
			"keep":          "yes",
		},
	})
	require.NoError(t, err)

	err = store.UpdateActivityData(ctx, recordID, map[string]any{
		"actor_display":  "Bob",
		"object_display": "Widget",
	})
	require.NoError(t, err)

	record := findActivityRecord(t, store, recordID)
	require.Equal(t, "Alice", record.Data["actor_display"])
	require.Equal(t, "Widget", record.Data["object_display"])
	require.Equal(t, "yes", record.Data["keep"])
}

func TestRepository_ListActivityForEnrichmentMissingOrStale(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	db := newTestActivityDB(t)
	applyActivityDDL(t, db)

	store, err := NewRepository(RepositoryConfig{DB: db})
	require.NoError(t, err)

	now := time.Now().UTC()
	cutoff := now.Add(-24 * time.Hour)
	old := now.Add(-48 * time.Hour)

	missingID := uuid.New()
	staleID := uuid.New()
	freshID := uuid.New()

	require.NoError(t, store.Log(ctx, types.ActivityRecord{
		ID:         missingID,
		Verb:       "activity.missing",
		ObjectType: "test",
		ObjectID:   "missing",
		OccurredAt: now.Add(-3 * time.Hour),
		Data:       map[string]any{},
	}))
	require.NoError(t, store.Log(ctx, types.ActivityRecord{
		ID:         staleID,
		Verb:       "activity.stale",
		ObjectType: "test",
		ObjectID:   "stale",
		OccurredAt: now.Add(-2 * time.Hour),
		Data: map[string]any{
			DataKeyActorDisplay: "Actor",
			DataKeyEnrichedAt:   old.Format(time.RFC3339Nano),
		},
	}))
	require.NoError(t, store.Log(ctx, types.ActivityRecord{
		ID:         freshID,
		Verb:       "activity.fresh",
		ObjectType: "test",
		ObjectID:   "fresh",
		OccurredAt: now.Add(-1 * time.Hour),
		Data: map[string]any{
			DataKeyActorDisplay: "Actor",
			DataKeyEnrichedAt:   now.Format(time.RFC3339Nano),
		},
	}))

	page, err := store.ListActivityForEnrichment(ctx, ActivityEnrichmentFilter{
		MissingKeys:    []string{DataKeyActorDisplay},
		EnrichedBefore: &cutoff,
		Limit:          10,
	})
	require.NoError(t, err)
	require.Len(t, page.Records, 2)

	ids := map[uuid.UUID]bool{}
	for _, rec := range page.Records {
		ids[rec.ID] = true
	}
	require.True(t, ids[missingID])
	require.True(t, ids[staleID])
	require.False(t, ids[freshID])
}

func findActivityRecord(t *testing.T, store *Repository, id uuid.UUID) types.ActivityRecord {
	t.Helper()
	page, err := store.ListActivity(context.Background(), types.ActivityFilter{
		Pagination: types.Pagination{Limit: 50},
	})
	require.NoError(t, err)
	for _, record := range page.Records {
		if record.ID == id {
			return record
		}
	}
	t.Fatalf("activity record %s not found", id.String())
	return types.ActivityRecord{}
}
