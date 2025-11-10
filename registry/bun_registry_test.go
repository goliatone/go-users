package registry

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
)

func TestBunRoleRegistry_CreateListAssign(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	applyTestMigration(t, db, roleSchemaDDL)

	var events []types.RoleEvent
	registry, err := NewRoleRegistry(RoleRegistryConfig{
		DB: db,
		Hooks: types.Hooks{
			AfterRoleChange: func(_ context.Context, evt types.RoleEvent) {
				events = append(events, evt)
			},
		},
		Clock: fixedClock{t: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
	})
	require.NoError(t, err)

	scope := types.ScopeFilter{TenantID: uuid.New()}
	actor := uuid.New()

	role, err := registry.CreateRole(ctx, types.RoleMutation{
		Name:        "Content Editor",
		Description: "can edit content",
		Permissions: []string{"content.read", "content.update"},
		Scope:       scope,
		ActorID:     actor,
	})
	require.NoError(t, err)
	require.Equal(t, "Content Editor", role.Name)

	page, err := registry.ListRoles(ctx, types.RoleFilter{
		Scope:      scope,
		Pagination: types.Pagination{Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, page.Roles, 1)
	require.Equal(t, role.ID, page.Roles[0].ID)

	userID := uuid.New()
	require.NoError(t, registry.AssignRole(ctx, userID, role.ID, scope, actor))

	assignments, err := registry.ListAssignments(ctx, types.RoleAssignmentFilter{
		Scope:  scope,
		UserID: userID,
	})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.Equal(t, role.Name, assignments[0].RoleName)

	require.NoError(t, registry.UnassignRole(ctx, userID, role.ID, scope, actor))
	require.Len(t, events, 3, "create + assign + unassign should emit events")
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

func applyTestMigration(t *testing.T, db *bun.DB, ddl string) {
	statements := splitSQLStatements(ddl)
	for _, stmt := range statements {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		_, err := db.Exec(stmt)
		require.NoError(t, err, "executing statement %q", stmt)
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
			stmt := strings.TrimSpace(builder.String())
			stmt = strings.TrimSuffix(stmt, ";")
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

type fixedClock struct {
	t time.Time
}

func (f fixedClock) Now() time.Time {
	return f.t
}

const roleSchemaDDL = `
CREATE TABLE custom_roles (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    permissions JSONB NOT NULL DEFAULT '[]',
    is_system BOOLEAN NOT NULL DEFAULT FALSE,
    tenant_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    org_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by UUID NOT NULL,
    updated_by UUID NOT NULL
);
CREATE UNIQUE INDEX custom_roles_scope_name_idx ON custom_roles (tenant_id, org_id, lower(name));
CREATE TABLE user_custom_roles (
    user_id UUID NOT NULL,
    role_id UUID NOT NULL,
    tenant_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    org_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    assigned_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    assigned_by UUID NOT NULL,
    PRIMARY KEY (user_id, role_id, tenant_id, org_id)
);
`
