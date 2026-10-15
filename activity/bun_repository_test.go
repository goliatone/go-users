package activity

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
)

func TestRepository_LogAndList(t *testing.T) {
	ctx := context.Background()
	db := newTestActivityDB(t)
	applyActivityDDL(t, db)

	store, err := NewRepository(RepositoryConfig{DB: db})
	require.NoError(t, err)

	event := types.ActivityRecord{
		UserID:     uuid.New(),
		ActorID:    uuid.New(),
		Verb:       "user.lifecycle.transition",
		ObjectType: "user",
		ObjectID:   "abc",
		Channel:    "lifecycle",
		Data: map[string]any{
			"from": "pending",
			"to":   "active",
		},
	}
	require.NoError(t, store.Log(ctx, event))

	page, err := store.ListActivity(ctx, types.ActivityFilter{
		Verbs:      []string{"user.lifecycle.transition"},
		Pagination: types.Pagination{Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, page.Records, 1)
	require.Equal(t, "user.lifecycle.transition", page.Records[0].Verb)
	require.Equal(t, "active", page.Records[0].Data["to"])
}

func TestRepository_Stats(t *testing.T) {
	ctx := context.Background()
	db := newTestActivityDB(t)
	applyActivityDDL(t, db)
	store, err := NewRepository(RepositoryConfig{DB: db})
	require.NoError(t, err)

	for i := range 3 {
		require.NoError(t, store.Log(ctx, types.ActivityRecord{
			Verb:       "user.lifecycle.transition",
			ObjectType: "user",
			Data:       map[string]any{"index": i},
		}))
	}
	require.NoError(t, store.Log(ctx, types.ActivityRecord{
		Verb: "user.password.reset",
	}))

	stats, err := store.ActivityStats(ctx, types.ActivityStatsFilter{})
	require.NoError(t, err)
	require.Equal(t, 4, stats.Total)
	require.Equal(t, 3, stats.ByVerb["user.lifecycle.transition"])
	require.Equal(t, 1, stats.ByVerb["user.password.reset"])
}

func TestRepository_ChannelFilters(t *testing.T) {
	ctx := context.Background()
	db := newTestActivityDB(t)
	applyActivityDDL(t, db)
	store, err := NewRepository(RepositoryConfig{DB: db})
	require.NoError(t, err)

	channels := []string{"settings", "bulk", "export"}
	for _, channel := range channels {
		require.NoError(t, store.Log(ctx, types.ActivityRecord{
			Verb:    "activity." + channel,
			Channel: channel,
		}))
	}

	page, err := store.ListActivity(ctx, types.ActivityFilter{
		Channels:        []string{"settings", "bulk"},
		ChannelDenylist: []string{"bulk"},
		Pagination:      types.Pagination{Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, page.Records, 1)
	require.Equal(t, "settings", page.Records[0].Channel)

	page, err = store.ListActivity(ctx, types.ActivityFilter{
		Channel:         "export",
		ChannelDenylist: []string{"export"},
		Pagination:      types.Pagination{Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, page.Records, 0)

	page, err = store.ListActivity(ctx, types.ActivityFilter{
		Channel:    "export",
		Channels:   []string{"settings"},
		Pagination: types.Pagination{Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, page.Records, 1)
	require.Equal(t, "settings", page.Records[0].Channel)
}

func TestRepository_MachineActivityFilter(t *testing.T) {
	ctx := context.Background()
	db := newTestActivityDB(t)
	applyActivityDDL(t, db)
	store, err := NewRepository(RepositoryConfig{DB: db})
	require.NoError(t, err)

	require.NoError(t, store.Log(ctx, types.ActivityRecord{
		Verb: "activity.system",
		Data: map[string]any{"system": true},
	}))
	require.NoError(t, store.Log(ctx, types.ActivityRecord{
		Verb: "activity.job",
		Data: map[string]any{"actor_type": "job"},
	}))
	require.NoError(t, store.Log(ctx, types.ActivityRecord{
		Verb: "activity.user",
		Data: map[string]any{"event": "user.login"},
	}))

	disabled := false
	page, err := store.ListActivity(ctx, types.ActivityFilter{
		MachineActivityEnabled: &disabled,
		MachineActorTypes:      []string{"job"},
		MachineDataKeys:        []string{"system", "machine"},
		Pagination:             types.Pagination{Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, page.Records, 1)
	require.Equal(t, "activity.user", page.Records[0].Verb)
}

func newTestActivityDB(t *testing.T) *bun.DB {
	sqldb, err := sql.Open("sqlite3", ":memory:?cache=shared")
	require.NoError(t, err)
	sqldb.SetMaxOpenConns(1)
	db := bun.NewDB(sqldb, sqlitedialect.New())
	t.Cleanup(func() {
		_ = db.Close()
		_ = sqldb.Close()
	})
	return db
}

func applyActivityDDL(t *testing.T, db *bun.DB) {
	content, err := os.ReadFile("../data/sql/migrations/sqlite/00004_user_activity.up.sql")
	require.NoError(t, err)
	scopeDDL, err := os.ReadFile("../data/sql/migrations/sqlite/00011_activity_scopes.tx.up.sql")
	require.NoError(t, err)
	content = append(content, scopeDDL...)
	require.NoError(t, err)
	for _, stmt := range splitStatements(string(content)) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
}

func splitStatements(sql string) []string {
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
			statements = append(statements, strings.TrimSuffix(builder.String(), ";"))
			builder.Reset()
		} else {
			builder.WriteString(" ")
		}
	}
	if builder.Len() > 0 {
		statements = append(statements, builder.String())
	}
	return statements
}

// Metadata labels are not durable activity ownership. Never widen a labeled request to
// all activity in the tenant, even when metadata claims the requested Group.
func TestRepositoryIgnoresMetadataAsScopeOwnership(t *testing.T) {
	ctx := context.Background()
	db := newTestActivityDB(t)
	applyActivityDDL(t, db)
	store, err := NewRepository(RepositoryConfig{DB: db})
	require.NoError(t, err)
	tenant, a, b := uuid.New(), uuid.New(), uuid.New()
	for _, group := range []uuid.UUID{a, b} {
		require.NoError(t, store.Log(ctx, types.ActivityRecord{TenantID: tenant, Verb: "changed", ObjectType: "group", ObjectID: group.String(), Data: map[string]any{"group_id": a.String()}}))
	}
	for _, group := range []uuid.UUID{a, b} {
		scoped := types.ScopeFilter{TenantID: tenant}.WithLabel("group_id", group)
		page, err := store.ListActivity(ctx, types.ActivityFilter{Scope: scoped})
		require.NoError(t, err)
		require.Empty(t, page.Records)
		stats, err := store.ActivityStats(ctx, types.ActivityStatsFilter{Scope: scoped})
		require.NoError(t, err)
		require.Zero(t, stats.Total)
	}
	// A host may authorize a canonical Group object and select its own feed.
	page, err := store.ListActivity(ctx, types.ActivityFilter{Scope: types.ScopeFilter{TenantID: tenant}, ObjectType: "group", ObjectID: a.String(), Pagination: types.Pagination{Limit: 1}})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	require.Len(t, page.Records, 1)
	require.Equal(t, a.String(), page.Records[0].ObjectID)
	stats, err := store.ActivityStats(ctx, types.ActivityStatsFilter{Scope: types.ScopeFilter{TenantID: tenant}})
	require.NoError(t, err)
	require.Equal(t, 2, stats.Total)
	// Validation precedes SQL, including stores without a stats DB.
	empty := &Repository{}
	_, err = empty.ListActivity(ctx, types.ActivityFilter{Scope: types.ScopeFilter{Labels: map[string]uuid.UUID{"group_id": uuid.Nil}}})
	require.ErrorIs(t, err, types.ErrInvalidScopeLabels)
	_, err = empty.ActivityStats(ctx, types.ActivityStatsFilter{Scope: types.ScopeFilter{Labels: map[string]uuid.UUID{"group_id": uuid.Nil}}})
	require.ErrorIs(t, err, types.ErrInvalidScopeLabels)
}
