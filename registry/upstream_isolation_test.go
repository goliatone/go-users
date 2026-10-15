package registry

import (
	"context"
	"errors"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"testing"
)

// Local reproduction of G2; the historical integration harness was unavailable.
func TestG2PersistedRoleGroupIsolation(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	applyTestMigration(t, db, roleSchemaDDL)
	r, err := NewRoleRegistry(RoleRegistryConfig{DB: db})
	require.NoError(t, err)
	role, err := r.CreateRole(ctx, types.RoleMutation{Name: "reader"})
	require.NoError(t, err)
	user, actor := uuid.New(), uuid.New()
	a, b := types.ScopeFilter{}.WithLabel("group", uuid.New()), types.ScopeFilter{}.WithLabel("group", uuid.New())
	require.NoError(t, r.AssignRole(ctx, user, role.ID, a, actor))
	require.NoError(t, r.AssignRole(ctx, user, role.ID, b, actor))
	require.NoError(t, r.UnassignRole(ctx, user, role.ID, b, actor))
	r, err = NewRoleRegistry(RoleRegistryConfig{DB: db})
	require.NoError(t, err)
	grants, err := r.ListAssignments(ctx, types.RoleAssignmentFilter{UserID: user, Scope: a})
	require.NoError(t, err)
	require.Len(t, grants, 1, "revoking B must preserve the independent persisted A grant")
}

func TestExplicitCatalogAndTransactionRollback(t *testing.T) {
	scopeDatabases(t, func(t *testing.T, db *bun.DB, dialect string) {
		legacyRoleSchema(t, db, dialect)
		require.NoError(t, roleMigration(t, db, dialect, "00010_role_assignment_scopes.tx.up.sql"))
		ctx := context.Background()
		r, err := NewRoleRegistry(RoleRegistryConfig{DB: db})
		require.NoError(t, err)
		catalog := types.ScopeFilter{TenantID: uuid.New(), OrgID: uuid.New()}
		role, err := r.CreateRole(ctx, types.RoleMutation{Name: "shared", Scope: catalog})
		require.NoError(t, err)
		actor, user := uuid.New(), uuid.New()
		a := types.ScopeFilter{}.WithLabel("group", uuid.New())
		grant := types.ScopedRoleGrant{UserID: user, RoleID: role.ID, CatalogScope: catalog, Scope: a, ActorID: actor}
		require.NoError(t, r.AssignRoleFromCatalog(ctx, grant))
		grants, err := r.ListAssignments(ctx, types.RoleAssignmentFilter{Scope: a, UserID: user})
		require.NoError(t, err)
		require.Len(t, grants, 1)
		expected := errors.New("rollback")
		err = db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			bound := r.BindTx(tx)
			if err := bound.UnassignRole(ctx, user, role.ID, a, actor); err != nil {
				return err
			}
			return expected
		})
		require.ErrorIs(t, err, expected)
		grants, err = r.ListAssignments(ctx, types.RoleAssignmentFilter{Scope: a, UserID: user})
		require.NoError(t, err)
		require.Len(t, grants, 1)
	})
}
