package profile

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

func TestRepository_UpsertAndGet(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	applyDDL(t, db)

	repo, err := NewRepository(RepositoryConfig{DB: db})
	require.NoError(t, err)

	userID := uuid.New()
	tenantID := uuid.New()
	actor := uuid.New()
	profile := types.UserProfile{
		UserID:      userID,
		DisplayName: "Initial Name",
		Locale:      "en",
		Scope: types.ScopeFilter{
			TenantID: tenantID,
		},
		Contact: map[string]any{
			"email": "user@example.com",
		},
		Metadata: map[string]any{
			"source": "import",
		},
		CreatedBy: actor,
		UpdatedBy: actor,
	}

	created, err := repo.UpsertProfile(ctx, profile)
	require.NoError(t, err)
	require.Equal(t, "Initial Name", created.DisplayName)
	require.Equal(t, tenantID, created.Scope.TenantID)
	require.NotZero(t, created.CreatedAt)
	require.NotZero(t, created.UpdatedAt)

	updatedProfile := *created
	updatedProfile.DisplayName = "Updated Name"
	updatedProfile.Bio = "Bio"
	updatedProfile.UpdatedBy = uuid.New()

	updated, err := repo.UpsertProfile(ctx, updatedProfile)
	require.NoError(t, err)
	require.Equal(t, "Updated Name", updated.DisplayName)
	require.Equal(t, created.CreatedAt, updated.CreatedAt)
	require.Equal(t, updatedProfile.UpdatedBy, updated.UpdatedBy)
	require.Equal(t, "Bio", updated.Bio)

	fetched, err := repo.GetProfile(ctx, userID, types.ScopeFilter{TenantID: tenantID})
	require.NoError(t, err)
	require.Equal(t, "Updated Name", fetched.DisplayName)
	require.Equal(t, "user@example.com", fetched.Contact["email"])
	require.Equal(t, "import", fetched.Metadata["source"])
}

func newTestDB(t *testing.T) *bun.DB {
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

func applyDDL(t *testing.T, db *bun.DB) {
	content, err := os.ReadFile("../data/sql/migrations/sqlite/00005_profiles_preferences.up.sql")
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
