package command

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/goliatone/go-users/activity"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/query"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
)

func TestLifecycleCommandLogsActivity(t *testing.T) {
	ctx := context.Background()
	db := newActivityTestDB(t)
	applyActivityMigration(t, db)
	store, err := activity.NewRepository(activity.RepositoryConfig{
		DB: db,
	})
	require.NoError(t, err)

	repo := newFakeAuthRepo()
	userID := uuid.New()
	repo.users[userID] = &types.AuthUser{
		ID:     userID,
		Status: types.LifecycleStateActive,
	}
	cmd := NewUserLifecycleTransitionCommand(LifecycleCommandConfig{
		Repository: repo,
		Activity:   store,
	})

	err = cmd.Execute(ctx, UserLifecycleTransitionInput{
		UserID: userID,
		Target: types.LifecycleStateSuspended,
		Actor: types.ActorRef{
			ID: uuid.New(),
		},
		Scope: types.ScopeFilter{},
	})
	require.NoError(t, err)

	feedQuery := query.NewActivityFeedQuery(store, nil)
	page, err := feedQuery.Query(ctx, types.ActivityFilter{
		Actor: types.ActorRef{
			ID: uuid.New(),
		},
		Verbs:      []string{"user.lifecycle.transition"},
		Pagination: types.Pagination{Limit: 5},
	})
	require.NoError(t, err)
	require.Len(t, page.Records, 1)
	require.Equal(t, userID, page.Records[0].UserID)
	require.Equal(t, "user.lifecycle.transition", page.Records[0].Verb)
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
	content, err := os.ReadFile("../data/sql/migrations/sqlite/00004_user_activity.up.sql")
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
