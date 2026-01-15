package query

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/goliatone/go-auth"
	"github.com/goliatone/go-users/activity"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
)

func TestActivityFeedQueryPolicySelfOnlyForNonAdmin(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	db := newActivityQueryDB(t)
	applyActivityQueryDDL(t, db)
	store, err := activity.NewRepository(activity.RepositoryConfig{DB: db})
	require.NoError(t, err)

	tenantID := uuid.New()
	actorID := uuid.New()
	otherID := uuid.New()

	require.NoError(t, store.Log(ctx, types.ActivityRecord{
		UserID:   actorID,
		ActorID:  actorID,
		TenantID: tenantID,
		Verb:     "user.login",
	}))
	require.NoError(t, store.Log(ctx, types.ActivityRecord{
		UserID:   otherID,
		ActorID:  otherID,
		TenantID: tenantID,
		Verb:     "user.login",
	}))

	actorCtx := &auth.ActorContext{
		ActorID:  actorID.String(),
		Role:     types.ActorRoleSupport,
		TenantID: tenantID.String(),
	}
	policy := activity.NewDefaultAccessPolicy()
	feedQuery := NewActivityFeedQuery(store, nil, WithActivityAccessPolicy(policy))

	queryCtx := auth.WithActorContext(ctx, actorCtx)
	page, err := feedQuery.Query(queryCtx, types.ActivityFilter{
		Actor:      types.ActorRef{ID: actorID},
		Scope:      types.ScopeFilter{TenantID: tenantID},
		Pagination: types.Pagination{Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, page.Records, 1)
	require.Equal(t, actorID, page.Records[0].UserID)
}

func TestActivityFeedQueryPolicyAllowsSuperadminScopeWidening(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	db := newActivityQueryDB(t)
	applyActivityQueryDDL(t, db)
	store, err := activity.NewRepository(activity.RepositoryConfig{DB: db})
	require.NoError(t, err)

	tenantA := uuid.New()
	tenantB := uuid.New()
	actorID := uuid.New()

	require.NoError(t, store.Log(ctx, types.ActivityRecord{
		UserID:   actorID,
		ActorID:  actorID,
		TenantID: tenantA,
		Verb:     "user.login",
	}))
	recordID := uuid.New()
	require.NoError(t, store.Log(ctx, types.ActivityRecord{
		ID:       recordID,
		UserID:   uuid.New(),
		ActorID:  uuid.New(),
		TenantID: tenantB,
		Verb:     "user.login",
	}))

	actorCtx := &auth.ActorContext{
		ActorID:  actorID.String(),
		Role:     types.ActorRoleSystemAdmin,
		TenantID: tenantA.String(),
	}
	policy := activity.NewDefaultAccessPolicy(
		activity.WithPolicyFilterOptions(activity.WithSuperadminScope(true)),
	)
	feedQuery := NewActivityFeedQuery(store, nil, WithActivityAccessPolicy(policy))

	queryCtx := auth.WithActorContext(ctx, actorCtx)
	page, err := feedQuery.Query(queryCtx, types.ActivityFilter{
		Actor:      types.ActorRef{ID: actorID},
		Scope:      types.ScopeFilter{TenantID: tenantB},
		Pagination: types.Pagination{Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, page.Records, 1)
	require.Equal(t, tenantB, page.Records[0].TenantID)
}

func newActivityQueryDB(t *testing.T) *bun.DB {
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

func applyActivityQueryDDL(t *testing.T, db *bun.DB) {
	content, err := os.ReadFile("../data/sql/migrations/sqlite/00004_user_activity.up.sql")
	require.NoError(t, err)
	for _, stmt := range splitActivityStatements(string(content)) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
}

func splitActivityStatements(sql string) []string {
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
