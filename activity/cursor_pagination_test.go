package activity

import (
	"context"
	"testing"
	"time"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestApplyCursorPaginationFiltersByCreatedAt(t *testing.T) {
	ctx := context.Background()
	db := newTestActivityDB(t)
	applyActivityDDL(t, db)
	store, err := NewRepository(RepositoryConfig{DB: db})
	require.NoError(t, err)

	base := time.Date(2024, 2, 10, 12, 0, 0, 0, time.UTC)
	records := []types.ActivityRecord{
		{ID: uuid.New(), Verb: "activity.old", OccurredAt: base.Add(-2 * time.Hour)},
		{ID: uuid.New(), Verb: "activity.mid", OccurredAt: base.Add(-1 * time.Hour)},
		{ID: uuid.New(), Verb: "activity.new", OccurredAt: base},
	}
	for _, record := range records {
		require.NoError(t, store.Log(ctx, record))
	}

	cursor := &ActivityCursor{
		OccurredAt: records[1].OccurredAt,
		ID:         records[1].ID,
	}

	var rows []LogEntry
	err = ApplyCursorPagination(db.NewSelect().Model(&rows), cursor, 10).Scan(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, records[0].ID, rows[0].ID)
}

func TestApplyCursorPaginationBreaksTiesWithID(t *testing.T) {
	ctx := context.Background()
	db := newTestActivityDB(t)
	applyActivityDDL(t, db)
	store, err := NewRepository(RepositoryConfig{DB: db})
	require.NoError(t, err)

	occurredAt := time.Date(2024, 2, 10, 9, 30, 0, 0, time.UTC)
	idLow := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	idHigh := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	require.NoError(t, store.Log(ctx, types.ActivityRecord{
		ID:         idLow,
		Verb:       "activity.tie",
		OccurredAt: occurredAt,
	}))
	require.NoError(t, store.Log(ctx, types.ActivityRecord{
		ID:         idHigh,
		Verb:       "activity.tie",
		OccurredAt: occurredAt,
	}))

	cursor := &ActivityCursor{
		OccurredAt: occurredAt,
		ID:         idHigh,
	}

	var rows []LogEntry
	err = ApplyCursorPagination(db.NewSelect().Model(&rows), cursor, 10).Scan(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, idLow, rows[0].ID)
}
