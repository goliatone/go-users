package groups_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	auth "github.com/goliatone/go-auth"
	"github.com/goliatone/go-users/activity"
	goauth "github.com/goliatone/go-users/adapter/goauth"
	"github.com/goliatone/go-users/groups"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/registry"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
)

const resourceWrite types.PolicyAction = "resources:write"
const resourceRead types.PolicyAction = "resources:read"

func databases(t *testing.T, fn func(*testing.T, *bun.DB, string)) {
	t.Helper()
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var db *bun.DB
			if dialect == "sqlite" {
				raw, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "groups.sqlite")+"?_foreign_keys=on&_busy_timeout=5000")
				require.NoError(t, err)
				db = bun.NewDB(raw, sqlitedialect.New())
			} else {
				dsn := os.Getenv("GO_USERS_TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("PostgreSQL requires disposable GO_USERS_TEST_POSTGRES_DSN")
				}
				adminRaw, err := sql.Open("postgres", dsn)
				require.NoError(t, err)
				schema := "groups_" + uuid.New().String()[:8]
				_, err = adminRaw.Exec("CREATE SCHEMA " + schema)
				require.NoError(t, err)
				raw, err := sql.Open("postgres", dsn+" search_path="+schema)
				require.NoError(t, err)
				db = bun.NewDB(raw, pgdialect.New())
				t.Cleanup(func() {
					_, err := adminRaw.Exec("DROP SCHEMA " + schema + " CASCADE")
					require.NoError(t, err)
					require.NoError(t, adminRaw.Close())
				})
			}
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			fn(t, db, dialect)
		})
	}
}

func migration(t *testing.T, db *bun.DB, dialect, name string) error {
	t.Helper()
	root := "../data/sql/migrations"
	if dialect == "sqlite" {
		root += "/sqlite"
	}
	ddl, err := os.ReadFile(filepath.Join(root, name))
	require.NoError(t, err)
	return db.RunInTx(context.Background(), nil, func(ctx context.Context, tx bun.Tx) error { _, err := tx.ExecContext(ctx, string(ddl)); return err })
}
func schema(t *testing.T, db *bun.DB, dialect string) {
	t.Helper()
	for _, name := range []string{"00003_custom_roles.up.sql", "00004_user_activity.up.sql", "00006_custom_roles_metadata.up.sql", "00007_custom_roles_order.up.sql", "00010_role_assignment_scopes.tx.up.sql", "00011_activity_scopes.tx.up.sql", "00012_groups.tx.up.sql"} {
		require.NoError(t, migration(t, db, dialect, name))
	}
}

func config(db *bun.DB) groups.Config {
	return groups.Config{DB: db, Resolver: types.PassthroughScopeResolver{}, Policy: types.AuthorizationPolicyFunc(func(context.Context, types.PolicyCheck) error { return nil }), Actions: []types.PolicyAction{resourceWrite, resourceRead}}
}

type fixture struct {
	service         *groups.Service
	roles           *registry.RoleRegistry
	actor           types.ActorRef
	a, b            uuid.UUID
	reader, manager uuid.UUID
	cfg             groups.Config
}

func setup(t *testing.T, db *bun.DB, dialect string) fixture {
	t.Helper()
	schema(t, db, dialect)
	ctx := context.Background()
	cfg := config(db)
	// Deliberately separate tenant/org catalog from Group-only assignment scope.
	cfg.CatalogScope = types.ScopeFilter{TenantID: uuid.New(), OrgID: uuid.New()}
	s, err := groups.NewStrict(cfg)
	require.NoError(t, err)
	r, err := registry.NewRoleRegistry(registry.RoleRegistryConfig{DB: db})
	require.NoError(t, err)
	actor := types.ActorRef{ID: uuid.New(), Type: types.ActorRoleSystemAdmin}
	a, err := s.Create(ctx, actor, "Group A")
	require.NoError(t, err)
	b, err := s.Create(ctx, actor, "Group B")
	require.NoError(t, err)
	reader, err := r.CreateRole(ctx, types.RoleMutation{Name: "reader", Scope: cfg.CatalogScope, Permissions: []string{string(groups.ActionRead), string(resourceRead)}})
	require.NoError(t, err)
	manager, err := r.CreateRole(ctx, types.RoleMutation{Name: "manager", Scope: cfg.CatalogScope, Permissions: []string{string(groups.ActionRead), string(groups.ActionManage), string(groups.ActionMembers), string(groups.ActionRoles), string(resourceWrite), string(resourceRead)}})
	require.NoError(t, err)
	// Trusted bootstrap, explicit membership and grant: Create confers neither.
	for _, id := range []uuid.UUID{a.ID, b.ID} {
		now := time.Now().UTC()
		_, err = db.NewInsert().Model(&groups.Membership{GroupID: id, PrincipalID: actor.ID, Active: true, CreatedAt: now, UpdatedAt: now}).Exec(ctx)
		require.NoError(t, err)
		require.NoError(t, r.AssignRoleFromCatalog(ctx, types.ScopedRoleGrant{UserID: actor.ID, RoleID: manager.ID, CatalogScope: cfg.CatalogScope, Scope: groups.Scope(id), ActorID: actor.ID}))
	}
	_, err = db.Exec("CREATE TABLE resource_writes (id TEXT PRIMARY KEY, group_id TEXT NOT NULL, payload TEXT NOT NULL)")
	require.NoError(t, err)
	return fixture{s, r, actor, a.ID, b.ID, reader.ID, manager.ID, cfg}
}

func guard(t *testing.T, s *groups.Service) *scope.StrictGuard {
	t.Helper()
	g, err := s.Guard()
	require.NoError(t, err)
	return g
}

func TestNativeGroupsLifecycleAndReopen(t *testing.T) {
	databases(t, func(t *testing.T, db *bun.DB, dialect string) {
		f := setup(t, db, dialect)
		ctx := context.Background()
		member := types.ActorRef{ID: uuid.New(), Type: types.ActorRoleSystemAdmin}
		g := guard(t, f.service)
		_, err := g.Enforce(ctx, member, groups.Scope(f.a), resourceRead, uuid.Nil)
		require.Error(t, err, "global role does not bypass missing membership/grants")
		require.NoError(t, f.service.SetMember(ctx, f.actor, f.a, member.ID, true))
		_, err = g.Enforce(ctx, member, groups.Scope(f.a), resourceRead, uuid.Nil)
		require.Error(t, err, "membership is not permission")
		require.NoError(t, f.service.SetRole(ctx, f.actor, f.a, member.ID, f.reader, true))
		_, err = g.Enforce(ctx, member, groups.Scope(f.a), resourceRead, uuid.Nil)
		require.NoError(t, err)
		_, err = g.Enforce(ctx, member, groups.Scope(f.b), resourceRead, uuid.Nil)
		require.Error(t, err)
		require.Error(t, f.service.SetRole(ctx, member, f.a, member.ID, f.manager, true), "self escalation denied")
		require.Error(t, f.service.SetMember(ctx, member, f.a, uuid.New(), true))
		// Registry reopen and native service reconstruction retain actual SQL state.
		reopened, err := groups.NewStrict(f.cfg)
		require.NoError(t, err)
		visible, err := reopened.Discover(ctx, member)
		require.NoError(t, err)
		require.Len(t, visible, 1)
		require.Equal(t, f.a, visible[0].ID)
		require.NoError(t, f.service.SetRole(ctx, f.actor, f.a, member.ID, f.reader, false))
		_, err = guard(t, reopened).Enforce(ctx, member, groups.Scope(f.a), resourceRead, uuid.Nil)
		require.Error(t, err, "revocation is fresh without cached claims")
		require.NoError(t, f.service.SetMember(ctx, f.actor, f.a, member.ID, false))
		require.NoError(t, f.service.SetMember(ctx, f.actor, f.a, member.ID, true))
		_, err = g.Enforce(ctx, member, groups.Scope(f.a), resourceRead, uuid.Nil)
		require.Error(t, err, "rejoin never reactivates revoked grants")
		require.NoError(t, f.service.SetRole(ctx, f.actor, f.a, member.ID, f.reader, true))
		require.NoError(t, f.service.SetMember(ctx, f.actor, f.a, member.ID, false))
		require.NoError(t, f.service.SetMember(ctx, f.actor, f.a, member.ID, true))
		_, err = g.Enforce(ctx, member, groups.Scope(f.a), resourceRead, uuid.Nil)
		require.Error(t, err, "removal also destroys still-live grants")
		require.NoError(t, f.service.SetActive(ctx, f.actor, f.a, false))
		_, err = g.Enforce(ctx, f.actor, groups.Scope(f.a), resourceWrite, uuid.Nil)
		require.Error(t, err)
		require.NoError(t, f.service.SetActive(ctx, f.actor, f.a, true))
		_, err = g.Enforce(ctx, f.actor, groups.Scope(f.a), resourceWrite, uuid.Nil)
		require.NoError(t, err)
		// A grant in unlabeled/global scope cannot authorize a Group-only request.
		require.NoError(t, f.roles.AssignRoleFromCatalog(ctx, types.ScopedRoleGrant{UserID: member.ID, RoleID: f.manager, CatalogScope: f.cfg.CatalogScope, Scope: types.ScopeFilter{}, ActorID: f.actor.ID}))
		_, err = g.Enforce(ctx, member, groups.Scope(f.a), resourceWrite, uuid.Nil)
		require.Error(t, err)
	})
}

func TestNativeGroupStrictConstructionAndForgedScope(t *testing.T) {
	databases(t, func(t *testing.T, db *bun.DB, dialect string) {
		f := setup(t, db, dialect)
		cfg := f.cfg
		cfg.Resolver = nil
		_, err := groups.NewStrict(cfg)
		require.ErrorIs(t, err, scope.ErrScopeResolverRequired)
		cfg = f.cfg
		var nilPolicy types.AuthorizationPolicyFunc
		cfg.Policy = nilPolicy
		_, err = groups.NewStrict(cfg)
		require.ErrorIs(t, err, scope.ErrAuthorizationPolicyRequired)
		cfg = f.cfg
		cfg.Policy = types.AllowAllAuthorizationPolicy{}
		_, err = groups.NewStrict(cfg)
		require.Error(t, err)
		require.Error(t, scope.ValidateStrict(scope.NopGuard()))
		g := guard(t, f.service)
		ctx := context.Background()
		for _, input := range []struct {
			s types.ScopeFilter
			a types.PolicyAction
		}{
			{groups.Scope(uuid.New()), resourceRead}, {groups.Scope(f.a), ""}, {groups.Scope(f.a), "unknown"}, {types.ScopeFilter{}, resourceRead}, {groups.Scope(f.a).WithLabel("other", uuid.New()), resourceRead}, {types.ScopeFilter{TenantID: uuid.New(), Labels: groups.Scope(f.a).Labels}, resourceRead},
		} {
			_, err := g.Enforce(ctx, f.actor, input.s, input.a, uuid.Nil)
			require.Error(t, err)
		}
		cfg = f.cfg
		cfg.Resolver = types.ScopeResolverFunc(func(context.Context, types.ActorRef, types.ScopeFilter) (types.ScopeFilter, error) {
			return groups.Scope(f.b), nil
		})
		rewritten, err := groups.NewStrict(cfg)
		require.NoError(t, err)
		_, err = guard(t, rewritten).Enforce(ctx, f.actor, groups.Scope(f.a), resourceRead, uuid.Nil)
		require.Error(t, err, "resolver cannot substitute another Group")
		// Host policy remains an additional live constraint even with exact native grants.
		cfg = f.cfg
		cfg.Policy = types.AuthorizationPolicyFunc(func(context.Context, types.PolicyCheck) error { return types.ErrUnauthorizedScope })
		denied, err := groups.NewStrict(cfg)
		require.NoError(t, err)
		_, err = guard(t, denied).Enforce(ctx, f.actor, groups.Scope(f.a), resourceRead, uuid.Nil)
		require.ErrorIs(t, err, types.ErrUnauthorizedScope)
	})
}

func TestNativeGroupCommandsAndAuditRollback(t *testing.T) {
	databases(t, func(t *testing.T, db *bun.DB, dialect string) {
		f := setup(t, db, dialect)
		ctx := context.Background()
		cmd := groups.NewManageCommand(f.service)
		principal := uuid.New()
		require.NoError(t, cmd.Execute(ctx, groups.ManageInput{Actor: f.actor, GroupID: f.a, PrincipalID: principal, Operation: groups.AddMember}))
		require.Error(t, cmd.Execute(ctx, groups.ManageInput{Actor: f.actor, GroupID: f.a, Operation: "invalid"}))
		// A failed protected callback rolls back database writes.
		expected := errors.New("callback failed")
		err := f.service.WithAuthorized(ctx, f.actor, groups.Scope(f.a), resourceWrite, uuid.Nil, func(ctx context.Context, tx bun.Tx) error {
			_, err := tx.ExecContext(ctx, "INSERT INTO resource_writes VALUES (?, ?, ?)", uuid.New(), f.a, "opaque")
			if err != nil {
				return err
			}
			return expected
		})
		require.ErrorIs(t, err, expected)
		var n int
		require.NoError(t, db.NewRaw("SELECT COUNT(*) FROM resource_writes").Scan(ctx, &n))
		require.Zero(t, n)
		// Native membership/lifecycle/role audit is persisted atomically, never swallowed.
		_, err = db.Exec("DROP TABLE user_activity")
		require.NoError(t, err)
		require.Error(t, f.service.SetMember(ctx, f.actor, f.a, uuid.New(), true))
		require.Error(t, f.service.SetActive(ctx, f.actor, f.a, false))
		var active bool
		require.NoError(t, db.NewRaw("SELECT active FROM user_groups WHERE id = ?", f.a).Scan(ctx, &active))
		require.True(t, active)
	})
}

func TestG5RoleAndMembershipActivity(t *testing.T) {
	databases(t, func(t *testing.T, db *bun.DB, dialect string) {
		f := setup(t, db, dialect)
		ctx := context.Background()
		principal := uuid.New()
		require.NoError(t, f.service.SetMember(ctx, f.actor, f.a, principal, true))
		require.NoError(t, f.service.SetRole(ctx, f.actor, f.a, principal, f.reader, true))
		require.NoError(t, f.service.SetRole(ctx, f.actor, f.a, principal, f.reader, false))
		repo, err := activity.NewRepository(activity.RepositoryConfig{DB: db})
		require.NoError(t, err)
		sink, err := goauth.NewActivitySink(goauth.ActivitySinkConfig{Sink: activity.SanitizingSink{Next: repo}, AnonymousActorID: uuid.New(), ScopeProjector: func(ctx context.Context, _ auth.ActivityEvent) (types.ScopeFilter, error) {
			return guard(t, f.service).Enforce(ctx, f.actor, groups.Scope(f.a), resourceRead, uuid.Nil)
		}})
		require.NoError(t, err)
		require.NoError(t, sink.Record(ctx, auth.ActivityEvent{EventType: auth.ActivityEventLoginSuccess, Metadata: map[string]any{"group": "forged", "password": "opaque"}}))
		page, err := repo.ListActivity(ctx, types.ActivityFilter{Scope: groups.Scope(f.a)})
		require.NoError(t, err)
		verbs := map[string]bool{}
		for _, record := range page.Records {
			require.Equal(t, f.a, record.Labels["group"])
			verbs[record.Verb] = true
			require.Empty(t, record.Data)
		}
		for _, verb := range []string{"group.created", "group.member.added", "role.assigned", "role.unassigned", string(auth.ActivityEventLoginSuccess)} {
			require.True(t, verbs[verb], verb)
		}
		pageB, err := repo.ListActivity(ctx, types.ActivityFilter{Scope: groups.Scope(f.b)})
		require.NoError(t, err)
		require.Len(t, pageB.Records, 1)
	})
}

func TestNativeGroupRevocationOrdersProtectedWrites(t *testing.T) {
	databases(t, func(t *testing.T, db *bun.DB, dialect string) {
		for _, revokeFirst := range []bool{false, true} {
			t.Run(map[bool]string{false: "write-first", true: "revoke-first"}[revokeFirst], func(t *testing.T) {
				// Reuse schema across orderings, but provision distinct Groups and principals.
				if !revokeFirst {
					schema(t, db, dialect)
					_, err := db.Exec("CREATE TABLE resource_writes (id TEXT PRIMARY KEY, group_id TEXT NOT NULL, payload TEXT NOT NULL)")
					require.NoError(t, err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				cfg := config(db)
				reached, release := make(chan struct{}), make(chan struct{})
				blockingActor := uuid.New()
				blockedAction := resourceWrite
				if revokeFirst {
					blockedAction = groups.ActionRoles
				}
				cfg.Policy = types.AuthorizationPolicyFunc(func(ctx context.Context, c types.PolicyCheck) error {
					if c.Actor.ID == blockingActor && c.Action == blockedAction {
						select {
						case <-reached:
						default:
							close(reached)
						}
						select {
						case <-release:
						case <-ctx.Done():
							return ctx.Err()
						}
					}
					return nil
				})
				s, err := groups.NewStrict(cfg)
				require.NoError(t, err)
				manager := types.ActorRef{ID: uuid.New()}
				writer := types.ActorRef{ID: uuid.New()}
				if revokeFirst {
					manager.ID = blockingActor
				} else {
					writer.ID = blockingActor
				}
				group, err := s.Create(ctx, manager, "Group A")
				require.NoError(t, err)
				roles, err := registry.NewRoleRegistry(registry.RoleRegistryConfig{DB: db})
				require.NoError(t, err)
				manage, err := roles.CreateRole(ctx, types.RoleMutation{Name: "manage-" + uuid.New().String(), Permissions: []string{string(groups.ActionRoles), string(resourceWrite)}})
				require.NoError(t, err)
				write, err := roles.CreateRole(ctx, types.RoleMutation{Name: "write-" + uuid.New().String(), Permissions: []string{string(resourceWrite)}})
				require.NoError(t, err)
				for _, p := range []struct {
					actor types.ActorRef
					role  uuid.UUID
				}{{manager, manage.ID}, {writer, write.ID}} {
					now := time.Now().UTC()
					_, err := db.NewInsert().Model(&groups.Membership{GroupID: group.ID, PrincipalID: p.actor.ID, Active: true, CreatedAt: now, UpdatedAt: now}).Exec(ctx)
					require.NoError(t, err)
					require.NoError(t, roles.AssignRoleFromCatalog(ctx, types.ScopedRoleGrant{UserID: p.actor.ID, RoleID: p.role, Scope: groups.Scope(group.ID), ActorID: manager.ID}))
				}
				callbackEntered := make(chan struct{}, 1)
				doWrite := func() error {
					return s.WithAuthorized(ctx, writer, groups.Scope(group.ID), resourceWrite, uuid.Nil, func(ctx context.Context, tx bun.Tx) error {
						callbackEntered <- struct{}{}
						_, err := tx.ExecContext(ctx, "INSERT INTO resource_writes VALUES (?, ?, ?)", uuid.New(), group.ID, "opaque")
						return err
					})
				}
				doRevoke := func() error { return s.SetRole(ctx, manager, group.ID, writer.ID, write.ID, false) }
				first, second := make(chan error, 1), make(chan error, 1)
				if revokeFirst {
					go func() { first <- doRevoke() }()
				} else {
					go func() { first <- doWrite() }()
				}
				select {
				case <-reached:
				case <-ctx.Done():
					t.Fatal("first operation did not reach policy barrier")
				}
				started := make(chan struct{})
				go func() {
					close(started)
					if revokeFirst {
						second <- doWrite()
					} else {
						second <- doRevoke()
					}
				}()
				<-started
				select {
				case err := <-second:
					t.Fatalf("second operation escaped transaction lock: %v", err)
				case <-time.After(50 * time.Millisecond):
				}
				close(release)
				require.NoError(t, <-first)
				secondErr := <-second
				if revokeFirst {
					require.ErrorIs(t, secondErr, groups.ErrDenied)
					require.Empty(t, callbackEntered)
				} else {
					require.NoError(t, secondErr)
					require.Len(t, callbackEntered, 1)
				}
				// Once revocation commits, no subsequent protected callback can run.
				err = doWrite()
				require.ErrorIs(t, err, groups.ErrDenied)
				var n int
				require.NoError(t, db.NewRaw("SELECT COUNT(*) FROM resource_writes WHERE group_id = ?", group.ID).Scan(ctx, &n))
				if revokeFirst {
					require.Zero(t, n)
				} else {
					require.Equal(t, 1, n)
				}
			})
		}
	})
}

func TestNativeActivitySQLScopePaginationStatsAndCursor(t *testing.T) {
	databases(t, func(t *testing.T, db *bun.DB, dialect string) {
		schema(t, db, dialect)
		ctx := context.Background()
		repo, err := activity.NewRepository(activity.RepositoryConfig{DB: db})
		require.NoError(t, err)
		a, b := groups.Scope(uuid.New()), groups.Scope(uuid.New())
		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		// Interleave A/B and legacy rows so filtering after pagination would fail.
		for i := 0; i < 9; i++ {
			scope := a
			if i%3 == 1 {
				scope = b
			}
			if i%3 == 2 {
				scope = types.ScopeFilter{}
			}
			require.NoError(t, repo.Log(ctx, types.ActivityRecord{Labels: scope.Labels, Verb: "event", OccurredAt: base.Add(time.Duration(i) * time.Second), Data: map[string]any{"opaque": true}}))
		}
		for _, scope := range []types.ScopeFilter{a, b, {}} {
			page, err := repo.ListActivity(ctx, types.ActivityFilter{Scope: scope, Pagination: types.Pagination{Limit: 1, Offset: 1}})
			require.NoError(t, err)
			require.Equal(t, 3, page.Total)
			require.Len(t, page.Records, 1)
			require.True(t, page.HasMore)
			stats, err := repo.ActivityStats(ctx, types.ActivityStatsFilter{Scope: scope})
			require.NoError(t, err)
			require.Equal(t, 3, stats.Total)
			require.Equal(t, 3, stats.ByVerb["event"])
			filter := types.ActivityFilter{Scope: scope, Pagination: types.Pagination{Limit: 2}}
			first, err := repo.ListActivityCursor(ctx, filter, nil)
			require.NoError(t, err)
			require.Len(t, first, 2)
			last := first[len(first)-1]
			next, err := repo.ListActivityCursor(ctx, filter, &activity.ActivityCursor{OccurredAt: last.OccurredAt, ID: last.ID})
			require.NoError(t, err)
			require.Len(t, next, 1)
			for _, record := range append(first, next...) {
				require.Equal(t, scope.Label("group"), record.Labels["group"])
			}
		}
		aliased := types.ScopeFilter{Labels: map[string]uuid.UUID{" Group ": a.Label("group")}}
		page, err := repo.ListActivity(ctx, types.ActivityFilter{Scope: aliased})
		require.NoError(t, err)
		require.Equal(t, 3, page.Total)
		bad := types.ScopeFilter{Labels: map[string]uuid.UUID{"group": uuid.Nil}}
		_, err = repo.ListActivity(ctx, types.ActivityFilter{Scope: bad})
		require.ErrorIs(t, err, types.ErrInvalidScopeLabels)
		_, err = repo.ActivityStats(ctx, types.ActivityStatsFilter{Scope: bad})
		require.ErrorIs(t, err, types.ErrInvalidScopeLabels)
		require.ErrorIs(t, repo.Log(ctx, types.ActivityRecord{Labels: bad.Labels, Verb: "invalid"}), types.ErrInvalidScopeLabels)
		policy := activity.NewRecordPolicy(activity.RecordPolicyConfig{DataSanitizer: func(record types.ActivityRecord) map[string]any { record.Labels["group"] = uuid.New(); return nil }})
		record := types.ActivityRecord{Labels: a.Labels, Data: map[string]any{"opaque": true}}
		sanitized := policy.Sanitize(record)
		require.Equal(t, a.Labels, sanitized.Labels)
		require.Empty(t, sanitized.Data)
	})
}

func TestNativeGroupAndActivityMigrationUpgradeAndGuardedDowngrade(t *testing.T) {
	databases(t, func(t *testing.T, db *bun.DB, dialect string) {
		ctx := context.Background()
		require.NoError(t, migration(t, db, dialect, "00004_user_activity.up.sql"))
		id := uuid.New()
		_, err := db.Exec("INSERT INTO user_activity (id,verb,data) VALUES (?, ?, ?)", id, "legacy.event", "{}")
		require.NoError(t, err)
		require.NoError(t, migration(t, db, dialect, "00011_activity_scopes.tx.up.sql"))
		repo, err := activity.NewRepository(activity.RepositoryConfig{DB: db})
		require.NoError(t, err)
		page, err := repo.ListActivity(ctx, types.ActivityFilter{})
		require.NoError(t, err)
		require.Len(t, page.Records, 1)
		require.Equal(t, id, page.Records[0].ID)
		require.Empty(t, page.Records[0].Labels)
		require.NoError(t, migration(t, db, dialect, "00011_activity_scopes.tx.down.sql"))
		require.NoError(t, migration(t, db, dialect, "00011_activity_scopes.tx.up.sql"))
		a := groups.Scope(uuid.New())
		require.NoError(t, repo.Log(ctx, types.ActivityRecord{Labels: a.Labels, Verb: "scoped.event"}))
		require.Error(t, migration(t, db, dialect, "00011_activity_scopes.tx.down.sql"), "downgrade cannot discard scope")
		page, err = repo.ListActivity(ctx, types.ActivityFilter{Scope: a})
		require.NoError(t, err)
		require.Len(t, page.Records, 1)
		require.NoError(t, migration(t, db, dialect, "00012_groups.tx.up.sql"))
		now := time.Now().UTC()
		group := &groups.Group{ID: uuid.New(), Name: "Group A", Active: true, CreatedAt: now, UpdatedAt: now}
		_, err = db.NewInsert().Model(group).Exec(ctx)
		require.NoError(t, err)
		member := &groups.Membership{GroupID: group.ID, PrincipalID: uuid.New(), Active: true, CreatedAt: now, UpdatedAt: now}
		_, err = db.NewInsert().Model(member).Exec(ctx)
		require.NoError(t, err)
		_, err = db.NewInsert().Model(member).Exec(ctx)
		require.Error(t, err, "membership uniqueness")
		require.Error(t, migration(t, db, dialect, "00012_groups.tx.down.sql"))
		n, err := db.NewSelect().Model((*groups.Membership)(nil)).Count(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, n)
	})
}

func TestNativeGroupDuplicateGrantIsTransactionSafe(t *testing.T) {
	databases(t, func(t *testing.T, db *bun.DB, dialect string) {
		f := setup(t, db, dialect)
		ctx := context.Background()
		principal := uuid.New()
		require.NoError(t, f.service.SetMember(ctx, f.actor, f.a, principal, true))
		require.NoError(t, f.service.SetRole(ctx, f.actor, f.a, principal, f.reader, true))
		require.NoError(t, f.service.SetRole(ctx, f.actor, f.a, principal, f.reader, true))
		grants, err := f.roles.ListAssignments(ctx, types.RoleAssignmentFilter{UserID: principal, Scope: groups.Scope(f.a)})
		require.NoError(t, err)
		require.Len(t, grants, 1)
	})
}

func TestNativeGroupZeroServiceDenies(t *testing.T) {
	var s groups.Service
	_, err := s.Guard()
	require.Error(t, err)
	_, err = s.Create(context.Background(), types.ActorRef{ID: uuid.New()}, "Group A")
	require.Error(t, err)
	require.Error(t, s.SetMember(context.Background(), types.ActorRef{ID: uuid.New()}, uuid.New(), uuid.New(), true))
	_, err = s.Discover(context.Background(), types.ActorRef{ID: uuid.New()})
	require.Error(t, err)
}

func TestNativeGroupsSurviveDatabaseRestart(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "restart.sqlite") + "?_foreign_keys=on&_busy_timeout=5000"
	raw, err := sql.Open("sqlite3", dsn)
	require.NoError(t, err)
	db := bun.NewDB(raw, sqlitedialect.New())
	f := setup(t, db, "sqlite")
	principal := types.ActorRef{ID: uuid.New()}
	ctx := context.Background()
	require.NoError(t, f.service.SetMember(ctx, f.actor, f.a, principal.ID, true))
	require.NoError(t, f.service.SetRole(ctx, f.actor, f.a, principal.ID, f.reader, true))
	require.NoError(t, db.Close())
	raw, err = sql.Open("sqlite3", dsn)
	require.NoError(t, err)
	restarted := bun.NewDB(raw, sqlitedialect.New())
	defer restarted.Close()
	cfg := f.cfg
	cfg.DB = restarted
	service, err := groups.NewStrict(cfg)
	require.NoError(t, err)
	_, err = guard(t, service).Enforce(ctx, principal, groups.Scope(f.a), resourceRead, uuid.Nil)
	require.NoError(t, err)
	require.NoError(t, service.SetRole(ctx, f.actor, f.a, principal.ID, f.reader, false))
	require.NoError(t, service.SetMember(ctx, f.actor, f.a, principal.ID, false))
	require.NoError(t, service.SetMember(ctx, f.actor, f.a, principal.ID, true))
	_, err = guard(t, service).Enforce(ctx, principal, groups.Scope(f.a), resourceRead, uuid.Nil)
	require.Error(t, err)
}

func TestRegistryRoleEventsPersistExactActivityScope(t *testing.T) {
	databases(t, func(t *testing.T, db *bun.DB, dialect string) {
		f := setup(t, db, dialect)
		ctx := context.Background()
		repo, err := activity.NewRepository(activity.RepositoryConfig{DB: db})
		require.NoError(t, err)
		hook := &activity.UsersActivityHook{Mapper: activity.RoleEventMapper{}, Sink: activity.SanitizingSink{Next: repo}}
		var hookErrors []error
		roles, err := registry.NewRoleRegistry(registry.RoleRegistryConfig{DB: db, Hooks: types.Hooks{AfterRoleChange: func(ctx context.Context, e types.RoleEvent) { hookErrors = append(hookErrors, hook.Notify(ctx, e)) }}})
		require.NoError(t, err)
		role, err := roles.CreateRole(ctx, types.RoleMutation{Name: "event-reader", Scope: f.cfg.CatalogScope, ActorID: f.actor.ID})
		require.NoError(t, err)
		grant := types.ScopedRoleGrant{UserID: uuid.New(), RoleID: role.ID, CatalogScope: f.cfg.CatalogScope, Scope: groups.Scope(f.a), ActorID: f.actor.ID}
		require.NoError(t, roles.AssignRoleFromCatalog(ctx, grant))
		require.NoError(t, roles.UnassignRole(ctx, grant.UserID, grant.RoleID, grant.Scope, grant.ActorID))
		require.Len(t, hookErrors, 3)
		for _, err := range hookErrors {
			require.NoError(t, err)
		}
		catalog, err := repo.ListActivity(ctx, types.ActivityFilter{Scope: f.cfg.CatalogScope})
		require.NoError(t, err)
		require.Len(t, catalog.Records, 1)
		require.Equal(t, "role.created", catalog.Records[0].Verb)
		events, err := repo.ListActivity(ctx, types.ActivityFilter{Scope: groups.Scope(f.a), Verbs: []string{"role.assigned", "role.unassigned"}})
		require.NoError(t, err)
		require.Len(t, events.Records, 2)
		for _, event := range events.Records {
			require.Equal(t, f.a, event.Labels["group"])
			require.Equal(t, uuid.Nil, event.TenantID)
			require.Empty(t, event.Data)
		}
	})
}

// The merged activity implementation must retain the caller-owned audit API
// while preserving full canonical arbitrary scope through disclosure policy.
func TestTransactionActivityPreservesCompleteScope(t *testing.T) {
	databases(t, func(t *testing.T, db *bun.DB, dialect string) {
		schema(t, db, dialect)
		ctx := context.Background()
		repo, err := activity.NewRepository(activity.RepositoryConfig{DB: db})
		require.NoError(t, err)
		group, team := uuid.New(), uuid.New()
		requested := types.ScopeFilter{Labels: map[string]uuid.UUID{" Group ": group, "TEAM": team}}
		record := types.ActivityRecord{ID: uuid.New(), Verb: "resource.changed", Labels: requested.Labels, Data: map[string]any{"opaque": "private"}, IP: "127.0.0.1"}
		require.NoError(t, db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error { return repo.LogTx(ctx, tx, record, nil) }))
		page, err := repo.ListActivity(ctx, types.ActivityFilter{Scope: requested})
		require.NoError(t, err)
		require.Len(t, page.Records, 1)
		require.Equal(t, map[string]uuid.UUID{"group": group, "team": team}, page.Records[0].Labels)
		require.Empty(t, page.Records[0].Data)
		require.Empty(t, page.Records[0].IP)
		partial, err := repo.ListActivity(ctx, types.ActivityFilter{Scope: groups.Scope(group)})
		require.NoError(t, err)
		require.Empty(t, partial.Records)
		err = db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			good := record
			good.ID = uuid.New()
			if err := repo.LogTx(ctx, tx, good, nil); err != nil {
				return err
			}
			bad := record
			bad.ID = uuid.New()
			bad.Labels = map[string]uuid.UUID{"group": uuid.Nil}
			return repo.LogTx(ctx, tx, bad, nil)
		})
		require.ErrorIs(t, err, types.ErrInvalidScopeLabels)
		page, err = repo.ListActivity(ctx, types.ActivityFilter{Scope: requested})
		require.NoError(t, err)
		require.Equal(t, 1, page.Total, "failed required audit rolls back prior writes")
	})
}
