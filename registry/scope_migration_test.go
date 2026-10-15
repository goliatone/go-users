package registry

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/migrate"
)

// GO_USERS_TEST_POSTGRES_DSN enables the same regressions on PostgreSQL. Each
// test creates and removes an isolated schema, leaving existing tables alone.
func scopeDatabases(t *testing.T, run func(*testing.T, *bun.DB, string)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) {
		db := newTestDB(t)
		_, err := db.Exec("PRAGMA foreign_keys = ON")
		require.NoError(t, err)
		run(t, db, "sqlite")
	})
	t.Run("postgres", func(t *testing.T) {
		dsn := os.Getenv("GO_USERS_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("set GO_USERS_TEST_POSTGRES_DSN for PostgreSQL regressions")
		}
		raw, err := sql.Open("postgres", dsn)
		require.NoError(t, err)
		raw.SetMaxOpenConns(1)
		db := bun.NewDB(raw, pgdialect.New())
		schema := "scope_test_" + uuid.New().String()[:8]
		_, err = db.Exec("CREATE SCHEMA ?", bun.Ident(schema))
		require.NoError(t, err)
		t.Cleanup(func() {
			_, err := db.Exec("DROP SCHEMA ? CASCADE", bun.Ident(schema))
			require.NoError(t, err)
			require.NoError(t, db.Close())
		})
		_, err = db.Exec("SET search_path TO ?", bun.Ident(schema))
		require.NoError(t, err)
		run(t, db, "postgres")
	})
}

func roleMigration(t *testing.T, db *bun.DB, dialect, name string) error {
	t.Helper()
	root := "../data/sql/migrations"
	if dialect == "sqlite" {
		root = filepath.Join(root, "sqlite")
	}
	ddl, err := os.ReadFile(filepath.Join(root, name))
	require.NoError(t, err)
	return db.RunInTx(context.Background(), nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.ExecContext(ctx, string(ddl))
		return err
	})
}

func legacyRoleSchema(t *testing.T, db *bun.DB, dialect string) {
	t.Helper()
	for _, name := range []string{"00003_custom_roles.up.sql", "00006_custom_roles_metadata.up.sql", "00007_custom_roles_order.up.sql"} {
		require.NoError(t, roleMigration(t, db, dialect, name))
	}
}

func TestAssignmentCompleteScopePersistence(t *testing.T) {
	scopeDatabases(t, func(t *testing.T, db *bun.DB, dialect string) {
		ctx := context.Background()
		legacyRoleSchema(t, db, dialect)
		require.NoError(t, roleMigration(t, db, dialect, "00010_role_assignment_scopes.tx.up.sql"))
		r, err := NewRoleRegistry(RoleRegistryConfig{DB: db})
		require.NoError(t, err)
		catalog := types.ScopeFilter{TenantID: uuid.New(), OrgID: uuid.New()}
		groupA, groupB, team := uuid.New(), uuid.New(), uuid.New()
		scopes := []types.ScopeFilter{catalog, catalog.WithLabel("group", groupA), catalog.WithLabel("group", groupB), catalog.WithLabel("group", groupA).WithLabel("team", team)}
		users := []uuid.UUID{uuid.New(), uuid.New()}
		roles := make([]uuid.UUID, 2)
		actor := uuid.New()
		for i, name := range []string{"reader", "editor"} {
			role, err := r.CreateRole(ctx, types.RoleMutation{Name: name, Scope: catalog, ActorID: actor})
			require.NoError(t, err)
			roles[i] = role.ID
			for _, user := range users {
				for _, scope := range scopes {
					require.NoError(t, r.AssignRole(ctx, user, role.ID, scope, actor))
					require.NoError(t, r.AssignRole(ctx, user, role.ID, scope, actor), "duplicate grants are idempotent")
				}
			}
		}
		// Equivalent raw map order/casing must address the same persisted grant.
		equivalent := types.ScopeFilter{TenantID: catalog.TenantID, OrgID: catalog.OrgID, Labels: map[string]uuid.UUID{" TEAM ": team, " Group ": groupA}}
		require.NoError(t, r.AssignRole(ctx, users[0], roles[0], equivalent, actor))
		r, err = NewRoleRegistry(RoleRegistryConfig{DB: db})
		require.NoError(t, err)
		for _, scope := range scopes {
			grants, err := r.ListAssignments(ctx, types.RoleAssignmentFilter{Scope: scope, UserIDs: users, RoleIDs: roles})
			require.NoError(t, err)
			require.Len(t, grants, 4, "full scope is exact, including unlabeled scope")
			for _, grant := range grants {
				require.Equal(t, scope.TenantID, grant.Scope.TenantID)
				require.Equal(t, scope.OrgID, grant.Scope.OrgID)
				require.Equal(t, len(scope.Labels), len(grant.Scope.Labels))
				for key, id := range scope.Labels {
					require.Equal(t, id, grant.Scope.Label(key))
				}
				require.Equal(t, actor, grant.AssignedBy)
				require.NotEmpty(t, grant.RoleName)
			}
		}
		require.NoError(t, r.UnassignRole(ctx, users[0], roles[0], scopes[2], actor))
		for i, scope := range scopes {
			grants, err := r.ListAssignments(ctx, types.RoleAssignmentFilter{Scope: scope})
			require.NoError(t, err)
			expected := 4
			if i == 2 {
				expected = 3
			}
			require.Len(t, grants, expected, "revocation must affect only one principal, role and complete scope")
		}
		wrong := scopes[1].Clone()
		wrong.OrgID = uuid.New()
		grants, err := r.ListAssignments(ctx, types.RoleAssignmentFilter{Scope: wrong})
		require.NoError(t, err)
		require.Empty(t, grants)
		// Canonical aliases work on reads and deletes too, with all filters.
		grants, err = r.ListAssignments(ctx, types.RoleAssignmentFilter{Scope: equivalent, UserID: users[0], RoleID: roles[0]})
		require.NoError(t, err)
		require.Len(t, grants, 1)
		require.NoError(t, r.UnassignRole(ctx, users[0], roles[0], equivalent, actor))
		grants, err = r.ListAssignments(ctx, types.RoleAssignmentFilter{Scope: equivalent, UserID: users[0], RoleID: roles[0]})
		require.NoError(t, err)
		require.Empty(t, grants)
		// Scope labels do not introduce global-definition fallback.
		global, err := r.CreateRole(ctx, types.RoleMutation{Name: "global", ActorID: actor})
		require.NoError(t, err)
		require.Error(t, r.AssignRole(ctx, users[0], global.ID, scopes[1], actor))
		// Foreign key/cascade survives the SQLite rebuild and PostgreSQL ALTER.
		require.NoError(t, r.DeleteRole(ctx, roles[1], catalog, actor))
		grants, err = r.ListAssignments(ctx, types.RoleAssignmentFilter{Scope: scopes[1], RoleID: roles[1]})
		require.NoError(t, err)
		require.Empty(t, grants)
	})
}

func TestAssignmentMigrationPreservesLegacyAndGuardsDowngrade(t *testing.T) {
	scopeDatabases(t, func(t *testing.T, db *bun.DB, dialect string) {
		ctx := context.Background()
		legacyRoleSchema(t, db, dialect)
		r, err := NewRoleRegistry(RoleRegistryConfig{DB: db})
		require.NoError(t, err)
		actor, user := uuid.New(), uuid.New()
		scope := types.ScopeFilter{TenantID: uuid.New(), OrgID: uuid.New()}
		role, err := r.CreateRole(ctx, types.RoleMutation{Name: "reader", ActorID: actor, Scope: scope})
		require.NoError(t, err)
		assignedAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
		_, err = db.Exec("INSERT INTO user_custom_roles (user_id, role_id, tenant_id, org_id, assigned_at, assigned_by) VALUES (?, ?, ?, ?, ?, ?)", user, role.ID, scope.TenantID, scope.OrgID, assignedAt, actor)
		require.NoError(t, err)
		// Exercise Bun's SQL loader: .tx suffix supplies the transaction, and
		// success-only markers preserve retryability after a refused downgrade.
		root := "../data/sql/migrations"
		if dialect == "sqlite" {
			root = filepath.Join(root, "sqlite")
		}
		files := fstest.MapFS{}
		for _, direction := range []string{"up", "down"} {
			name := "00010_role_assignment_scopes.tx." + direction + ".sql"
			ddl, err := os.ReadFile(filepath.Join(root, name))
			require.NoError(t, err)
			files[name] = &fstest.MapFile{Data: ddl}
		}
		migrations := migrate.NewMigrations()
		require.NoError(t, migrations.Discover(files))
		migrator := migrate.NewMigrator(db, migrations, migrate.WithMarkAppliedOnSuccess(true))
		require.NoError(t, migrator.Init(ctx))
		for i := 0; i < 2; i++ {
			_, err := migrator.Migrate(ctx)
			require.NoError(t, err)
			grants, err := r.ListAssignments(ctx, types.RoleAssignmentFilter{UserID: user, Scope: scope})
			require.NoError(t, err)
			require.Len(t, grants, 1)
			require.Empty(t, grants[0].Scope.Labels)
			require.Equal(t, actor, grants[0].AssignedBy)
			require.True(t, assignedAt.Equal(grants[0].AssignedAt))
			labeled := scope.WithLabel("group", uuid.New())
			require.NoError(t, r.AssignRole(ctx, user, role.ID, labeled, actor))
			_, err = migrator.Rollback(ctx)
			require.Error(t, err, "downgrade cannot discard labeled grants")
			status, err := migrator.MigrationsWithStatus(ctx)
			require.NoError(t, err)
			require.Len(t, status.Applied(), 1, "failed rollback retains applied marker")
			grants, err = r.ListAssignments(ctx, types.RoleAssignmentFilter{UserID: user, Scope: labeled})
			require.NoError(t, err)
			require.Len(t, grants, 1, "failed downgrade leaves grants intact")
			require.NoError(t, r.UnassignRole(ctx, user, role.ID, labeled, actor))
			_, err = migrator.Rollback(ctx)
			require.NoError(t, err)
			var count int
			require.NoError(t, db.NewRaw("SELECT COUNT(*) FROM user_custom_roles").Scan(ctx, &count))
			require.Equal(t, 1, count)
		}
	})
}

func TestAssignmentInvalidLabelsFailBeforeRepositoryAccess(t *testing.T) {
	r := &RoleRegistry{}
	ctx := context.Background()
	for _, labels := range []map[string]uuid.UUID{{" ": uuid.New()}, {"group": uuid.Nil}, {"Group": uuid.New(), " group ": uuid.New()}} {
		scope := types.ScopeFilter{Labels: labels}
		require.ErrorIs(t, r.AssignRole(ctx, uuid.New(), uuid.New(), scope, uuid.New()), types.ErrInvalidScopeLabels)
		require.ErrorIs(t, r.UnassignRole(ctx, uuid.New(), uuid.New(), scope, uuid.New()), types.ErrInvalidScopeLabels)
		_, err := r.ListAssignments(ctx, types.RoleAssignmentFilter{Scope: scope})
		require.ErrorIs(t, err, types.ErrInvalidScopeLabels)
	}
}

func TestTransactionRegistryRollbackAndCommit(t *testing.T) {
	scopeDatabases(t, func(t *testing.T, db *bun.DB, dialect string) {
		ctx := context.Background()
		legacyRoleSchema(t, db, dialect)
		require.NoError(t, roleMigration(t, db, dialect, "00010_role_assignment_scopes.tx.up.sql"))
		notifications := 0
		r, err := NewRoleRegistry(RoleRegistryConfig{DB: db, Hooks: types.Hooks{AfterRoleChange: func(context.Context, types.RoleEvent) { notifications++ }}})
		require.NoError(t, err)
		_, err = r.WithTx(bun.Tx{})
		require.Error(t, err)
		catalog := types.ScopeFilter{TenantID: uuid.New()}
		assigned := catalog.WithLabel("group_id", uuid.New())
		role, err := r.CreateRole(ctx, types.RoleMutation{Name: "editor", Scope: catalog})
		require.NoError(t, err)
		require.Equal(t, 1, notifications)
		user := uuid.New()
		rollback := errors.New("rollback")
		for _, commit := range []bool{false, true} {
			err = db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
				bound, err := r.WithTx(tx)
				if err != nil {
					return err
				}
				def, err := bound.GetRole(ctx, role.ID, catalog)
				if err != nil {
					return err
				}
				def.Name = "updated"
				if _, err = bound.UpdateRole(ctx, role.ID, types.RoleMutation{Name: def.Name, Scope: catalog}); err != nil {
					return err
				}
				if err = bound.AssignRole(ctx, user, role.ID, assigned, uuid.Nil); err != nil {
					return err
				}
				// Duplicate assignment must not poison a PostgreSQL transaction.
				if err = bound.AssignRole(ctx, user, role.ID, assigned, uuid.Nil); err != nil {
					return err
				}
				rows, err := bound.ListAssignments(ctx, types.RoleAssignmentFilter{Scope: assigned, UserID: user})
				if err != nil {
					return err
				}
				require.Len(t, rows, 1)
				roles, err := bound.ListRoles(ctx, types.RoleFilter{Scope: catalog})
				if err != nil {
					return err
				}
				require.Len(t, roles.Roles, 1)
				require.Equal(t, 1, notifications)
				if !commit {
					return rollback
				}
				return nil
			})
			if commit {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, rollback)
			}
			rows, err := r.ListAssignments(ctx, types.RoleAssignmentFilter{Scope: assigned, UserID: user})
			require.NoError(t, err)
			if commit {
				require.Len(t, rows, 1)
			} else {
				require.Empty(t, rows)
			}
			def, err := r.GetRole(ctx, role.ID, catalog)
			require.NoError(t, err)
			if commit {
				require.Equal(t, "updated", def.Name)
			} else {
				require.Equal(t, "editor", def.Name)
			}
		}
		err = db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			bound, err := r.WithTx(tx)
			if err != nil {
				return err
			}
			if err = bound.UnassignRole(ctx, user, role.ID, assigned, uuid.Nil); err != nil {
				return err
			}
			return bound.DeleteRole(ctx, role.ID, catalog, uuid.Nil)
		})
		require.NoError(t, err)
		require.Equal(t, 1, notifications)
	})
}
