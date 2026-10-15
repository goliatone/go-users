package registry

import (
	"context"
	"testing"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRoleCatalogRejectsLabelsBeforeRepositoryAccess(t *testing.T) {
	// No repositories: an unsupported scope must fail before any read or write.
	r := &RoleRegistry{}
	ctx := context.Background()
	scope := types.ScopeFilter{}.WithLabel("group", uuid.New())
	mutation := types.RoleMutation{Name: "reader", Scope: scope}
	_, err := r.CreateRole(ctx, mutation)
	require.ErrorIs(t, err, ErrUnsupportedRoleScope)
	_, err = r.GetRole(ctx, uuid.New(), scope)
	require.ErrorIs(t, err, ErrUnsupportedRoleScope)
	_, err = r.ListRoles(ctx, types.RoleFilter{Scope: scope})
	require.ErrorIs(t, err, ErrUnsupportedRoleScope)
	_, err = r.UpdateRole(ctx, uuid.New(), mutation)
	require.ErrorIs(t, err, ErrUnsupportedRoleScope)
	require.ErrorIs(t, r.DeleteRole(ctx, uuid.New(), scope, uuid.New()), ErrUnsupportedRoleScope)
}

func TestRoleCatalogTenantOrgCompatibility(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	applyTestMigration(t, db, roleSchemaDDL)
	r, err := NewRoleRegistry(RoleRegistryConfig{DB: db})
	require.NoError(t, err)
	scopes := []types.ScopeFilter{{}, {TenantID: uuid.New()}, {TenantID: uuid.New(), OrgID: uuid.New()}}
	for _, scope := range scopes {
		role, err := r.CreateRole(ctx, types.RoleMutation{Name: "reader", Scope: scope})
		require.NoError(t, err)
		require.Equal(t, scope, role.Scope)
		detail, err := r.GetRole(ctx, role.ID, scope)
		require.NoError(t, err)
		require.Equal(t, role.ID, detail.ID)
		page, err := r.ListRoles(ctx, types.RoleFilter{Scope: scope})
		require.NoError(t, err)
		require.Len(t, page.Roles, 1)
		_, err = r.UpdateRole(ctx, role.ID, types.RoleMutation{Name: "editor", Scope: scope})
		require.NoError(t, err)
		require.NoError(t, r.DeleteRole(ctx, role.ID, scope, uuid.New()))
		page, err = r.ListRoles(ctx, types.RoleFilter{Scope: scope})
		require.NoError(t, err)
		require.Empty(t, page.Roles)
	}
}
