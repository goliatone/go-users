package service_test

import (
	"context"
	"database/sql"
	"testing"

	users "github.com/goliatone/go-users"
	"github.com/goliatone/go-users/command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/registry"
	"github.com/goliatone/go-users/service"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
)

func grantRegistry(t *testing.T) *registry.RoleRegistry {
	t.Helper()
	raw, err := sql.Open("sqlite3", ":memory:?_foreign_keys=on")
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	db := bun.NewDB(raw, sqlitedialect.New())
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	for _, name := range []string{"00003_custom_roles.up.sql", "00006_custom_roles_metadata.up.sql", "00007_custom_roles_order.up.sql", "00010_role_assignment_scopes.tx.up.sql"} {
		ddl, err := users.CoreMigrationsFS.ReadFile("data/sql/migrations/sqlite/" + name)
		require.NoError(t, err)
		require.NoError(t, db.RunInTx(context.Background(), nil, func(ctx context.Context, tx bun.Tx) error {
			_, err := tx.ExecContext(ctx, string(ddl))
			return err
		}))
	}
	r, err := registry.NewRoleRegistry(registry.RoleRegistryConfig{DB: db})
	require.NoError(t, err)
	return r
}

func TestRoleGrantValidatorReceivesCandidateAndPreventsEscalation(t *testing.T) {
	ctx := context.Background()
	r := grantRegistry(t)
	catalog := types.ScopeFilter{TenantID: uuid.New(), OrgID: uuid.New()}
	a, b := catalog.WithLabel("group", uuid.New()), catalog.WithLabel("group", uuid.New())
	actor, target, blocked := types.ActorRef{ID: uuid.New()}, uuid.New(), uuid.New()
	reader, err := r.CreateRole(ctx, types.RoleMutation{Name: "reader", Scope: catalog, Permissions: []string{"content:read"}, Metadata: map[string]any{"tier": "read"}})
	require.NoError(t, err)
	admin, err := r.CreateRole(ctx, types.RoleMutation{Name: "admin", Scope: catalog, Permissions: []string{"content:read", "content:write", "grants:write"}})
	require.NoError(t, err)
	var checks []types.RoleGrantCheck
	cfg := strictTestConfig()
	cfg.RoleRegistry, cfg.RequireRoleGrantValidation = r, true
	cfg.RoleGrantValidator = types.RoleGrantValidatorFunc(func(_ context.Context, check types.RoleGrantCheck) error {
		checks = append(checks, check)
		require.Equal(t, actor, check.Actor)
		require.Equal(t, catalog, check.Role.Scope, "definitions load from the shared catalog without assignment labels")
		require.Equal(t, check.Role.ID, check.Assignment.RoleID)
		require.Equal(t, actor.ID, check.Assignment.AssignedBy)
		if check.Assignment.UserID == blocked {
			return types.ErrUnauthorizedScope
		}
		if check.Operation == types.RoleGrantAssign && check.Role.ID == admin.ID {
			return types.ErrUnauthorizedScope
		}
		return nil
	})
	svc, err := service.NewStrict(cfg)
	require.NoError(t, err)
	for _, input := range []command.AssignRoleInput{
		{UserID: actor.ID, RoleID: admin.ID, Scope: a, Actor: actor}, // self-escalation
		{UserID: target, RoleID: admin.ID, Scope: a, Actor: actor},   // non-grantable role
		{UserID: blocked, RoleID: reader.ID, Scope: a, Actor: actor}, // prohibited target
	} {
		require.ErrorIs(t, svc.Commands().AssignRole.Execute(ctx, input), types.ErrUnauthorizedScope)
	}
	require.Equal(t, admin.Permissions, checks[0].Role.Permissions)
	require.Equal(t, blocked, checks[2].Assignment.UserID)
	grants, err := r.ListAssignments(ctx, types.RoleAssignmentFilter{Scope: a})
	require.NoError(t, err)
	require.Empty(t, grants)
	for _, scope := range []types.ScopeFilter{a, b} {
		require.NoError(t, svc.Commands().AssignRole.Execute(ctx, command.AssignRoleInput{UserID: target, RoleID: reader.ID, Scope: scope, Actor: actor}))
	}
	require.Equal(t, a, checks[3].Assignment.Scope)
	require.Equal(t, reader.Metadata, checks[3].Role.Metadata)
	// Candidate permissions are live catalog data rather than an input/session snapshot.
	_, err = r.UpdateRole(ctx, reader.ID, types.RoleMutation{Name: "reader", Scope: catalog, Permissions: []string{"content:read", "content:export"}})
	require.NoError(t, err)
	require.NoError(t, svc.Commands().UnassignRole.Execute(ctx, command.UnassignRoleInput{UserID: target, RoleID: reader.ID, Scope: a, Actor: actor}))
	last := checks[len(checks)-1]
	require.Equal(t, types.RoleGrantRevoke, last.Operation)
	require.Equal(t, []string{"content:read", "content:export"}, last.Role.Permissions)
	grants, err = r.ListAssignments(ctx, types.RoleAssignmentFilter{Scope: a})
	require.NoError(t, err)
	require.Empty(t, grants)
	grants, err = r.ListAssignments(ctx, types.RoleAssignmentFilter{Scope: b})
	require.NoError(t, err)
	require.Len(t, grants, 1, "revoking A leaves B intact")
	require.Equal(t, target, grants[0].UserID)
}

func TestRoleGrantValidationRequiredWiringFailsClosed(t *testing.T) {
	cfg := strictTestConfig()
	cfg.RequireRoleGrantValidation = true
	for _, validator := range []types.RoleGrantValidator{nil, types.RoleGrantValidatorFunc(nil), (*nilGrantValidator)(nil)} {
		cfg.RoleGrantValidator = validator
		svc, err := users.NewStrict(cfg)
		require.Nil(t, svc)
		require.ErrorIs(t, err, types.ErrMissingRoleGrantValidator)
		svc = service.New(cfg)
		require.False(t, svc.Ready())
		require.ErrorIs(t, svc.HealthCheck(context.Background()), types.ErrMissingRoleGrantValidator)
		require.ErrorIs(t, svc.Commands().AssignRole.Execute(context.Background(), command.AssignRoleInput{UserID: uuid.New(), RoleID: uuid.New(), Actor: types.ActorRef{ID: uuid.New()}}), types.ErrMissingRoleGrantValidator)
		require.ErrorIs(t, svc.Commands().UnassignRole.Execute(context.Background(), command.UnassignRoleInput{UserID: uuid.New(), RoleID: uuid.New(), Actor: types.ActorRef{ID: uuid.New()}}), types.ErrMissingRoleGrantValidator)
	}
}

type nilGrantValidator struct{}

func (*nilGrantValidator) ValidateRoleGrant(context.Context, types.RoleGrantCheck) error {
	panic("typed nil validator was invoked")
}

func TestRoleGrantCandidateCannotRedirectThePersistedGrant(t *testing.T) {
	ctx := context.Background()
	r := grantRegistry(t)
	role, err := r.CreateRole(ctx, types.RoleMutation{Name: "reader", Permissions: []string{"content:read"}})
	require.NoError(t, err)
	a := types.ScopeFilter{}.WithLabel("group", uuid.New())
	other := uuid.New()
	cfg := command.RoleAssignmentCommandConfig{Registry: r, RequireGrantValidation: true, GrantValidator: types.RoleGrantValidatorFunc(func(_ context.Context, check types.RoleGrantCheck) error {
		check.Assignment.Scope.Labels["group"] = other
		check.Role.Permissions[0] = "admin:*"
		check.Assignment.UserID = other
		return nil
	})}
	input := command.AssignRoleInput{UserID: uuid.New(), RoleID: role.ID, Scope: a, Actor: types.ActorRef{ID: uuid.New()}}
	require.NoError(t, command.NewAssignRoleCommandWithConfig(cfg).Execute(ctx, input))
	grants, err := r.ListAssignments(ctx, types.RoleAssignmentFilter{Scope: a})
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.Equal(t, input.UserID, grants[0].UserID)
	require.Equal(t, a, grants[0].Scope)
	stored, err := r.GetRole(ctx, role.ID, types.ScopeFilter{})
	require.NoError(t, err)
	require.Equal(t, []string{"content:read"}, stored.Permissions)
}
