package activityenrichment

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goliatone/go-users/activity"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
)

func TestCommand_DefaultsMissingKeysAndCutoff(t *testing.T) {
	now := time.Date(2024, time.January, 2, 10, 0, 0, 0, time.UTC)
	query := &stubEnrichmentQuery{
		pages: []activity.ActivityEnrichmentPage{{}},
	}
	store := &recordingEnrichmentStore{}
	cmd := New(Config{
		EnrichmentQuery: query,
		EnrichmentStore: store,
		EnrichedAtCutoff: 24 * time.Hour,
		Clock:           fixedClock{now: now},
	})

	err := cmd.Execute(context.Background(), Input{})
	require.NoError(t, err)
	require.Len(t, query.filters, 1)

	filter := query.filters[0]
	require.ElementsMatch(t, []string{activity.DataKeyActorDisplay, activity.DataKeyObjectDisplay}, filter.MissingKeys)
	require.NotNil(t, filter.EnrichedBefore)
	require.True(t, filter.EnrichedBefore.Equal(now.Add(-24*time.Hour)))
}

func TestCommand_EnrichesAndStampsEnrichedAt(t *testing.T) {
	now := time.Date(2024, time.January, 3, 9, 0, 0, 0, time.UTC)
	recordID := uuid.New()
	actorID := uuid.New()

	record := types.ActivityRecord{
		ID:         recordID,
		ActorID:    actorID,
		Verb:       "activity.test",
		ObjectType: "user",
		ObjectID:   "123",
	}

	query := &stubEnrichmentQuery{
		pages: []activity.ActivityEnrichmentPage{{
			Records: []types.ActivityRecord{record},
		}},
	}
	store := &recordingEnrichmentStore{}
	actorResolver := mapActorResolver{
		data: map[uuid.UUID]activity.ActorInfo{
			actorID: {
				ID:      actorID,
				Display: "Alice Example",
			},
		},
	}
	objectResolver := mapObjectResolver{
		data: map[string]map[string]activity.ObjectInfo{
			"user": {
				"123": {
					ID:      "123",
					Type:    "user",
					Display: "User 123",
				},
			},
		},
	}

	cmd := New(Config{
		EnrichmentQuery: query,
		EnrichmentStore: store,
		ActorResolver:   actorResolver,
		ObjectResolver:  objectResolver,
		Clock:           fixedClock{now: now},
	})

	err := cmd.Execute(context.Background(), Input{})
	require.NoError(t, err)
	require.Len(t, store.updates, 1)

	update := store.updates[0]
	require.Equal(t, recordID, update.id)
	require.Equal(t, "Alice Example", update.data[activity.DataKeyActorDisplay])
	require.Equal(t, "User 123", update.data[activity.DataKeyObjectDisplay])
	require.Equal(t, activity.DefaultEnricherVersion, update.data[activity.DataKeyEnricherVersion])
	require.ElementsMatch(t, []string{activity.DataKeyEnrichedAt, activity.DataKeyEnricherVersion}, update.opts.ForceKeys)

	enrichedAt, ok := update.data[activity.DataKeyEnrichedAt].(string)
	require.True(t, ok)
	parsed, err := time.Parse(time.RFC3339Nano, enrichedAt)
	require.NoError(t, err)
	require.True(t, parsed.Equal(now))
}

func TestCommand_BackfillIntegrationUpdatesMixedRecords(t *testing.T) {
	ctx := context.Background()
	db := newActivityTestDB(t)
	applyActivityMigration(t, db)

	store, err := activity.NewRepository(activity.RepositoryConfig{DB: db})
	require.NoError(t, err)

	now := time.Date(2024, time.January, 4, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-24 * time.Hour)
	actorID := uuid.New()

	missingID := uuid.New()
	staleID := uuid.New()
	freshID := uuid.New()

	require.NoError(t, store.Log(ctx, types.ActivityRecord{
		ID:         missingID,
		ActorID:    actorID,
		Verb:       "activity.missing",
		ObjectType: "user",
		ObjectID:   "1",
		OccurredAt: now.Add(-3 * time.Hour),
		Data:       map[string]any{},
	}))
	require.NoError(t, store.Log(ctx, types.ActivityRecord{
		ID:         staleID,
		ActorID:    actorID,
		Verb:       "activity.stale",
		ObjectType: "user",
		ObjectID:   "2",
		OccurredAt: now.Add(-2 * time.Hour),
		Data: map[string]any{
			activity.DataKeyActorDisplay:  "Old Actor",
			activity.DataKeyObjectDisplay: "Old Object",
			activity.DataKeyEnrichedAt:    now.Add(-48 * time.Hour).Format(time.RFC3339Nano),
		},
	}))
	require.NoError(t, store.Log(ctx, types.ActivityRecord{
		ID:         freshID,
		ActorID:    actorID,
		Verb:       "activity.fresh",
		ObjectType: "user",
		ObjectID:   "3",
		OccurredAt: now.Add(-1 * time.Hour),
		Data: map[string]any{
			activity.DataKeyActorDisplay:  "Fresh Actor",
			activity.DataKeyObjectDisplay: "Fresh Object",
			activity.DataKeyEnrichedAt:    now.Add(-1 * time.Hour).Format(time.RFC3339Nano),
		},
	}))

	cmd := New(Config{
		EnrichmentQuery: store,
		EnrichmentStore: store,
		ActorResolver: mapActorResolver{
			data: map[uuid.UUID]activity.ActorInfo{
				actorID: {
					ID:      actorID,
					Display: "Resolved Actor",
				},
			},
		},
		ObjectResolver: mapObjectResolver{
			data: map[string]map[string]activity.ObjectInfo{
				"user": {
					"1": {ID: "1", Type: "user", Display: "User 1"},
					"2": {ID: "2", Type: "user", Display: "User 2"},
					"3": {ID: "3", Type: "user", Display: "User 3"},
				},
			},
		},
		Clock:            fixedClock{now: now},
		EnrichedAtCutoff: 24 * time.Hour,
	})

	err = cmd.Execute(ctx, Input{BatchSize: 10, EnrichedBefore: &cutoff})
	require.NoError(t, err)

	page, err := store.ListActivity(ctx, types.ActivityFilter{
		Pagination: types.Pagination{Limit: 10},
	})
	require.NoError(t, err)

	records := indexByID(page.Records)
	missing := records[missingID]
	require.Equal(t, "Resolved Actor", missing.Data[activity.DataKeyActorDisplay])
	require.Equal(t, "User 1", missing.Data[activity.DataKeyObjectDisplay])
	require.Equal(t, now.Format(time.RFC3339Nano), missing.Data[activity.DataKeyEnrichedAt])

	stale := records[staleID]
	require.Equal(t, "Old Actor", stale.Data[activity.DataKeyActorDisplay])
	require.Equal(t, "Old Object", stale.Data[activity.DataKeyObjectDisplay])
	require.Equal(t, now.Format(time.RFC3339Nano), stale.Data[activity.DataKeyEnrichedAt])

	fresh := records[freshID]
	require.Equal(t, "Fresh Actor", fresh.Data[activity.DataKeyActorDisplay])
	require.Equal(t, "Fresh Object", fresh.Data[activity.DataKeyObjectDisplay])
	require.Equal(t, now.Add(-1*time.Hour).Format(time.RFC3339Nano), fresh.Data[activity.DataKeyEnrichedAt])
}

func TestCommand_ConcurrentRunsDoNotOverwriteExistingKeys(t *testing.T) {
	now := time.Date(2024, time.January, 5, 8, 0, 0, 0, time.UTC)
	recordID := uuid.New()
	actorID := uuid.New()
	record := types.ActivityRecord{
		ID:         recordID,
		ActorID:    actorID,
		Verb:       "activity.concurrent",
		ObjectType: "user",
		ObjectID:   "1",
	}

	store := newMemoryEnrichmentStore()
	store.seed(recordID, map[string]any{})

	queryA := &stubEnrichmentQuery{
		pages: []activity.ActivityEnrichmentPage{{
			Records: []types.ActivityRecord{record},
		}},
	}
	queryB := &stubEnrichmentQuery{
		pages: []activity.ActivityEnrichmentPage{{
			Records: []types.ActivityRecord{record},
		}},
	}

	cmdA := New(Config{
		EnrichmentQuery: queryA,
		EnrichmentStore: store,
		ActorResolver: mapActorResolver{
			data: map[uuid.UUID]activity.ActorInfo{
				actorID: {
					ID:      actorID,
					Display: "Alice",
				},
			},
		},
		Clock: fixedClock{now: now},
	})
	cmdB := New(Config{
		EnrichmentQuery: queryB,
		EnrichmentStore: store,
		ActorResolver: mapActorResolver{
			data: map[uuid.UUID]activity.ActorInfo{
				actorID: {
					ID:      actorID,
					Display: "Bob",
				},
			},
		},
		Clock: fixedClock{now: now},
	})

	errCh := make(chan error, 2)
	go func() {
		errCh <- cmdA.Execute(context.Background(), Input{})
	}()
	go func() {
		errCh <- cmdB.Execute(context.Background(), Input{})
	}()

	for i := 0; i < 2; i++ {
		require.NoError(t, <-errCh)
	}

	data := store.get(recordID)
	display, ok := data[activity.DataKeyActorDisplay].(string)
	require.True(t, ok)
	require.Contains(t, []string{"Alice", "Bob"}, display)
	require.Equal(t, 1, store.changedUpdates)
	require.Len(t, store.actorDisplayHistory, 1)
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

type stubEnrichmentQuery struct {
	mu      sync.Mutex
	updates int
	filters []activity.ActivityEnrichmentFilter
	pages   []activity.ActivityEnrichmentPage
}

func (q *stubEnrichmentQuery) ListActivityForEnrichment(ctx context.Context, filter activity.ActivityEnrichmentFilter) (activity.ActivityEnrichmentPage, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.filters = append(q.filters, filter)
	if q.updates < len(q.pages) {
		page := q.pages[q.updates]
		q.updates++
		return page, nil
	}
	q.updates++
	return activity.ActivityEnrichmentPage{}, nil
}

type mapActorResolver struct {
	data map[uuid.UUID]activity.ActorInfo
	err  error
}

func (r mapActorResolver) ResolveActors(ctx context.Context, ids []uuid.UUID, meta activity.ResolveContext) (map[uuid.UUID]activity.ActorInfo, error) {
	if r.err != nil {
		return nil, r.err
	}
	resolved := make(map[uuid.UUID]activity.ActorInfo, len(ids))
	for _, id := range ids {
		if info, ok := r.data[id]; ok {
			resolved[id] = info
		}
	}
	return resolved, nil
}

type mapObjectResolver struct {
	data map[string]map[string]activity.ObjectInfo
	err  error
}

func (r mapObjectResolver) ResolveObjects(ctx context.Context, objectType string, ids []string, meta activity.ResolveContext) (map[string]activity.ObjectInfo, error) {
	if r.err != nil {
		return nil, r.err
	}
	source := r.data[objectType]
	if len(source) == 0 {
		return nil, nil
	}
	resolved := make(map[string]activity.ObjectInfo, len(ids))
	for _, id := range ids {
		if info, ok := source[id]; ok {
			resolved[id] = info
		}
	}
	return resolved, nil
}

type storeUpdate struct {
	id   uuid.UUID
	data map[string]any
	opts activity.ActivityEnrichmentUpdateOptions
}

type recordingEnrichmentStore struct {
	mu      sync.Mutex
	updates []storeUpdate
}

func (s *recordingEnrichmentStore) UpdateActivityData(ctx context.Context, id uuid.UUID, data map[string]any) error {
	return s.UpdateActivityDataWithOptions(ctx, id, data, activity.ActivityEnrichmentUpdateOptions{})
}

func (s *recordingEnrichmentStore) UpdateActivityDataWithOptions(ctx context.Context, id uuid.UUID, data map[string]any, opts activity.ActivityEnrichmentUpdateOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updates = append(s.updates, storeUpdate{
		id:   id,
		data: cloneAnyMap(data),
		opts: opts,
	})
	return nil
}

type memoryEnrichmentStore struct {
	mu                 sync.Mutex
	data               map[uuid.UUID]map[string]any
	changedUpdates     int
	actorDisplayHistory []string
}

func newMemoryEnrichmentStore() *memoryEnrichmentStore {
	return &memoryEnrichmentStore{
		data: make(map[uuid.UUID]map[string]any),
	}
}

func (s *memoryEnrichmentStore) seed(id uuid.UUID, data map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[id] = cloneAnyMap(data)
}

func (s *memoryEnrichmentStore) get(id uuid.UUID) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneAnyMap(s.data[id])
}

func (s *memoryEnrichmentStore) UpdateActivityData(ctx context.Context, id uuid.UUID, data map[string]any) error {
	return s.UpdateActivityDataWithOptions(ctx, id, data, activity.ActivityEnrichmentUpdateOptions{})
}

func (s *memoryEnrichmentStore) UpdateActivityDataWithOptions(ctx context.Context, id uuid.UUID, data map[string]any, opts activity.ActivityEnrichmentUpdateOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing := s.data[id]
	if existing == nil {
		existing = map[string]any{}
	}

	missing := missingData(existing, data)
	missing = forceKeys(missing, data, opts.ForceKeys)
	if len(missing) == 0 {
		s.data[id] = existing
		return nil
	}

	changed := false
	for key, value := range missing {
		if !reflect.DeepEqual(existing[key], value) {
			changed = true
		}
		existing[key] = value
	}
	if changed {
		s.changedUpdates++
		if display, ok := missing[activity.DataKeyActorDisplay].(string); ok {
			s.actorDisplayHistory = append(s.actorDisplayHistory, display)
		}
	}

	s.data[id] = existing
	return nil
}

func forceKeys(missing map[string]any, updates map[string]any, forceKeys []string) map[string]any {
	if len(forceKeys) == 0 || len(updates) == 0 {
		return missing
	}
	if missing == nil {
		missing = make(map[string]any, len(forceKeys))
	}
	for _, key := range forceKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		value, ok := updates[key]
		if !ok || value == nil {
			continue
		}
		missing[key] = value
	}
	return missing
}

func cloneAnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func indexByID(records []types.ActivityRecord) map[uuid.UUID]types.ActivityRecord {
	index := make(map[uuid.UUID]types.ActivityRecord, len(records))
	for _, record := range records {
		index[record.ID] = record
	}
	return index
}

func newActivityTestDB(t *testing.T) *bun.DB {
	sqlDB, err := sql.Open("sqlite3", ":memory:?cache=shared")
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	db := bun.NewDB(sqlDB, sqlitedialect.New())
	t.Cleanup(func() {
		_ = db.Close()
		_ = sqlDB.Close()
	})
	return db
}

func applyActivityMigration(t *testing.T, db *bun.DB) {
	content, err := os.ReadFile("../../data/sql/migrations/sqlite/00004_user_activity.up.sql")
	require.NoError(t, err)
	for _, stmt := range splitSQLStatements(string(content)) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
}

func splitSQLStatements(sql string) []string {
	lines := strings.Split(sql, "\n")
	var builder strings.Builder
	var statements []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		builder.WriteString(line)
		if strings.HasSuffix(line, ";") {
			stmt := strings.TrimSpace(strings.TrimSuffix(builder.String(), ";"))
			statements = append(statements, stmt)
			builder.Reset()
		} else {
			builder.WriteString(" ")
		}
	}
	if builder.Len() > 0 {
		statements = append(statements, strings.TrimSpace(builder.String()))
	}
	return statements
}
