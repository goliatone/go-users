package preferences

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

func TestPreferenceRepository_UpsertListDelete(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	applyDDL(t, db)

	repo, err := NewRepository(RepositoryConfig{DB: db})
	require.NoError(t, err)

	userID := uuid.New()
	tenantID := uuid.New()
	actor := uuid.New()

	first, err := repo.UpsertPreference(ctx, types.PreferenceRecord{
		UserID: userID,
		Scope: types.ScopeFilter{
			TenantID: tenantID,
		},
		Level:     types.PreferenceLevelUser,
		Key:       "notifications.email",
		Value:     map[string]any{"enabled": true},
		CreatedBy: actor,
		UpdatedBy: actor,
	})
	require.NoError(t, err)
	require.Equal(t, 1, first.Version)

	second, err := repo.UpsertPreference(ctx, types.PreferenceRecord{
		UserID: userID,
		Scope: types.ScopeFilter{
			TenantID: tenantID,
		},
		Level:     types.PreferenceLevelUser,
		Key:       "notifications.email",
		Value:     map[string]any{"enabled": false},
		UpdatedBy: uuid.New(),
	})
	require.NoError(t, err)
	require.Equal(t, 2, second.Version)
	require.False(t, second.Value["enabled"].(bool))

	tenantPref, err := repo.UpsertPreference(ctx, types.PreferenceRecord{
		Scope: types.ScopeFilter{
			TenantID: tenantID,
		},
		Level:     types.PreferenceLevelTenant,
		Key:       "dashboard.layout",
		Value:     map[string]any{"widgets": 3},
		CreatedBy: actor,
		UpdatedBy: actor,
	})
	require.NoError(t, err)
	require.Equal(t, types.PreferenceLevelTenant, tenantPref.Level)

	userPrefs, err := repo.ListPreferences(ctx, types.PreferenceFilter{
		UserID: userID,
		Scope: types.ScopeFilter{
			TenantID: tenantID,
		},
		Level: types.PreferenceLevelUser,
	})
	require.NoError(t, err)
	require.Len(t, userPrefs, 1)
	require.Equal(t, "notifications.email", userPrefs[0].Key)
	require.Equal(t, 2, userPrefs[0].Version)

	tenantPrefs, err := repo.ListPreferences(ctx, types.PreferenceFilter{
		Scope: types.ScopeFilter{
			TenantID: tenantID,
		},
		Level: types.PreferenceLevelTenant,
	})
	require.NoError(t, err)
	require.Len(t, tenantPrefs, 1)
	require.Equal(t, "dashboard.layout", tenantPrefs[0].Key)

	require.NoError(t, repo.DeletePreference(ctx, userID, types.ScopeFilter{TenantID: tenantID}, types.PreferenceLevelUser, "notifications.email"))

	remaining, err := repo.ListPreferences(ctx, types.PreferenceFilter{
		UserID: userID,
		Scope: types.ScopeFilter{
			TenantID: tenantID,
		},
		Level: types.PreferenceLevelUser,
	})
	require.NoError(t, err)
	require.Len(t, remaining, 0)
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
	content, err := os.ReadFile("../data/sql/migrations/000003_profiles_preferences.sql")
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
