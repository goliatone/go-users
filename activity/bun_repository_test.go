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

	for i := 0; i < 3; i++ {
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
