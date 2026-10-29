package service_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	auth "github.com/goliatone/go-auth"
	"github.com/goliatone/go-users/activity"
	"github.com/goliatone/go-users/command"
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

type authorizationTxKey struct{}

// This is a host fixture, not a go-users Group/version schema. Every writer
// first locks the same authoritative row before policy reads. Production hosts
// must lock all affected actor/target/Group rows in a consistent order.
type sqlAuthorizationHost struct {
	db          *bun.DB
	registry    *registry.RoleRegistry
	subject     uuid.UUID
	role        uuid.UUID
	scope       types.ScopeFilter
	beforeLock  func()
	afterLock   func()
	failVersion error
}

var _ auth.AuthorizationVersionStore = (*sqlAuthorizationHost)(nil)

func (h *sqlAuthorizationHost) CurrentAuthorizationVersion(ctx context.Context, subject, tenant string) (auth.PermissionVersionResult, error) {
	var version int
	err := h.db.NewRaw("SELECT version FROM host_authorization WHERE subject = ? AND tenant = ?", subject, tenant).Scan(ctx, &version)
	return auth.PermissionVersionResult{Version: strconv.Itoa(version)}, err
}
func (h *sqlAuthorizationHost) AdvanceAuthorizationVersion(ctx context.Context, subject, tenant, operation string, mutation auth.AuthorizationMutation) (auth.PermissionVersionResult, error) {
	var result auth.PermissionVersionResult
	err := h.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if h.beforeLock != nil {
			h.beforeLock()
		}
		// UPDATE obtains the SQLite writer lock / PostgreSQL row lock before reads.
		locked, err := tx.ExecContext(ctx, "UPDATE host_authorization SET version = version WHERE subject = ? AND tenant = ?", subject, tenant)
		if err != nil {
			return err
		}
		count, err := locked.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return types.ErrUnauthorizedScope
		}
		if h.afterLock != nil {
			h.afterLock()
		}
		var previous int
		err = tx.NewRaw("SELECT version FROM host_operations WHERE subject = ? AND tenant = ? AND operation = ?", subject, tenant, operation).Scan(ctx, &previous)
		if err == nil {
			result.Version = strconv.Itoa(previous)
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err = mutation(context.WithValue(ctx, authorizationTxKey{}, tx)); err != nil {
			return err
		}
		if h.failVersion != nil {
			return h.failVersion
		}
		var version int
		if err = tx.NewRaw("UPDATE host_authorization SET version = version + 1 WHERE subject = ? AND tenant = ? RETURNING version", subject, tenant).Scan(ctx, &version); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO host_operations (subject,tenant,operation,version) VALUES (?,?,?,?)", subject, tenant, operation, version); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO host_outbox (subject,tenant,operation,version,published) VALUES (?,?,?,?,FALSE)", subject, tenant, operation, version); err != nil {
			return err
		}
		result = auth.PermissionVersionResult{Version: strconv.Itoa(version), ObservedAt: time.Now().UTC()}
		return nil
	})
	return result, err
}

func (h *sqlAuthorizationHost) policy(tx bun.IDB, afterCheck func()) scope.Guard {
	guard, err := scope.NewStrictGuard(types.ScopeResolverFunc(func(_ context.Context, _ types.ActorRef, requested types.ScopeFilter) (types.ScopeFilter, error) {
		return requested.Clone(), nil
	}), types.AuthorizationPolicyFunc(func(ctx context.Context, check types.PolicyCheck) error {
		if check.Actor.ID != h.subject || check.Scope.Label("group_id") != h.scope.Label("group_id") {
			return types.ErrUnauthorizedScope
		}
		var enabled bool
		if err := tx.NewRaw("SELECT enabled FROM host_authorization WHERE subject = ? AND tenant = ?", h.subject.String(), h.scope.TenantID.String()).Scan(ctx, &enabled); err != nil {
			return err
		}
		if !enabled {
			return types.ErrUnauthorizedScope
		}
		if afterCheck != nil {
			afterCheck()
		}
		return nil
	}))
	if err != nil {
		panic(err)
	}
	return guard
}
func (h *sqlAuthorizationHost) assign(ctx context.Context, operation string, afterCheck func(), afterMutation func(context.Context, bun.Tx) error) (auth.AuthorizationVersionWriteResult, error) {
	return (auth.AuthorizationVersionWriter{Store: h}).Advance(ctx, operation, h.subject.String(), h.scope.TenantID.String(), func(ctx context.Context) error {
		tx := ctx.Value(authorizationTxKey{}).(bun.Tx)
		bound, err := h.registry.WithTx(tx)
		if err != nil {
			return err
		}
		cmd := command.NewAssignRoleCommand(bound, h.policy(tx, afterCheck))
		if err = cmd.Execute(ctx, command.AssignRoleInput{UserID: h.subject, RoleID: h.role, Scope: h.scope, Actor: types.ActorRef{ID: h.subject}}); err != nil {
			return err
		}
		if afterMutation != nil {
			return afterMutation(ctx, tx)
		}
		return nil
	})
}
func (h *sqlAuthorizationHost) revoke(ctx context.Context, operation string) (auth.AuthorizationVersionWriteResult, error) {
	return (auth.AuthorizationVersionWriter{Store: h}).Advance(ctx, operation, h.subject.String(), h.scope.TenantID.String(), func(ctx context.Context) error {
		tx := ctx.Value(authorizationTxKey{}).(bun.Tx)
		bound, err := h.registry.WithTx(tx)
		if err != nil {
			return err
		}
		if err = bound.UnassignRole(ctx, h.subject, h.role, h.scope, h.subject); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE host_authorization SET enabled = FALSE WHERE subject = ? AND tenant = ?", h.subject.String(), h.scope.TenantID.String())
		return err
	})
}

func authorizationDatabases(t *testing.T, run func(*testing.T, string)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) { run(t, "sqlite") })
	t.Run("postgres", func(t *testing.T) {
		if os.Getenv("GO_USERS_TEST_POSTGRES_DSN") == "" {
			t.Skip("set GO_USERS_TEST_POSTGRES_DSN for live PostgreSQL checks")
		}
		run(t, "postgres")
	})
}
func newSQLAuthorizationHost(t *testing.T, dialect string) *sqlAuthorizationHost {
	t.Helper()
	var db *bun.DB
	if dialect == "sqlite" {
		raw, err := sql.Open("sqlite3", "file:"+filepath.Join(t.TempDir(), "auth.db")+"?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=on")
		require.NoError(t, err)
		db = bun.NewDB(raw, sqlitedialect.New())
	} else {
		dsn := os.Getenv("GO_USERS_TEST_POSTGRES_DSN")
		admin, err := sql.Open("postgres", dsn)
		require.NoError(t, err)
		schema := "authorization_test_" + uuid.New().String()[:8]
		_, err = admin.Exec("CREATE SCHEMA " + schema)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, dropErr := admin.Exec("DROP SCHEMA " + schema + " CASCADE")
			require.NoError(t, dropErr)
			require.NoError(t, admin.Close())
		})
		raw, err := sql.Open("postgres", dsn+" search_path="+schema)
		require.NoError(t, err)
		db = bun.NewDB(raw, pgdialect.New())
	}
	db.SetMaxOpenConns(5)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	for _, name := range []string{"00004_user_activity.up.sql", "00003_custom_roles.up.sql", "00006_custom_roles_metadata.up.sql", "00007_custom_roles_order.up.sql", "00010_role_assignment_scopes.tx.up.sql", "00011_activity_scopes.tx.up.sql"} {
		root := "../data/sql/migrations"
		if dialect == "sqlite" {
			root = filepath.Join(root, "sqlite")
		}
		ddl, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err)
		require.NoError(t, db.RunInTx(context.Background(), nil, func(ctx context.Context, tx bun.Tx) error { _, err := tx.ExecContext(ctx, string(ddl)); return err }))
	}
	_, err := db.Exec("CREATE TABLE host_authorization (subject TEXT NOT NULL,tenant TEXT NOT NULL,version INTEGER NOT NULL,enabled BOOLEAN NOT NULL,PRIMARY KEY (subject,tenant))")
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE host_operations (subject TEXT NOT NULL,tenant TEXT NOT NULL,operation TEXT NOT NULL,version INTEGER NOT NULL,PRIMARY KEY(subject,tenant,operation))")
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE host_outbox (subject TEXT NOT NULL,tenant TEXT NOT NULL,operation TEXT NOT NULL,version INTEGER NOT NULL,published BOOLEAN NOT NULL,PRIMARY KEY(subject,tenant,operation))")
	require.NoError(t, err)
	r, err := registry.NewRoleRegistry(registry.RoleRegistryConfig{DB: db})
	require.NoError(t, err)
	scoped := types.ScopeFilter{TenantID: uuid.New()}.WithLabel("group_id", uuid.New())
	role, err := r.CreateRole(context.Background(), types.RoleMutation{Name: "editor", Scope: types.ScopeFilter{TenantID: scoped.TenantID}})
	require.NoError(t, err)
	h := &sqlAuthorizationHost{db: db, registry: r, subject: uuid.New(), role: role.ID, scope: scoped}
	_, err = db.Exec("INSERT INTO host_authorization (subject,tenant,version,enabled) VALUES (?,?,1,TRUE)", h.subject.String(), h.scope.TenantID.String())
	require.NoError(t, err)
	return h
}

func awaitAuthorizationBarrier(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("authorization barrier timed out")
	}
}
func assignmentCount(t *testing.T, h *sqlAuthorizationHost) int {
	t.Helper()
	rows, err := h.registry.ListAssignments(context.Background(), types.RoleAssignmentFilter{UserID: h.subject, Scope: h.scope})
	require.NoError(t, err)
	return len(rows)
}

func TestHostAuthorizationTransactionOrdering(t *testing.T) {
	authorizationDatabases(t, func(t *testing.T, dialect string) {
		t.Run("unsafe_check_then_write", func(t *testing.T) {
			h := newSQLAuthorizationHost(t, dialect)
			checked, resume := make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-resume:
				default:
					close(resume)
				}
			}()
			done := make(chan error, 1)
			go func() {
				done <- command.NewAssignRoleCommand(h.registry, h.policy(h.db, func() { close(checked); <-resume })).Execute(context.Background(), command.AssignRoleInput{UserID: h.subject, RoleID: h.role, Scope: h.scope, Actor: types.ActorRef{ID: h.subject}})
			}()
			awaitAuthorizationBarrier(t, checked)
			_, err := h.revoke(context.Background(), "unsafe-revoke")
			require.NoError(t, err)
			close(resume)
			require.NoError(t, <-done)
			require.Equal(t, 1, assignmentCount(t, h), "ordinary check/write can grant after revocation commits")
		})
		t.Run("write_locks_first", func(t *testing.T) {
			h := newSQLAuthorizationHost(t, dialect)
			checked, resume := make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-resume:
				default:
					close(resume)
				}
			}()
			done := make(chan error, 1)
			go func() {
				_, err := h.assign(context.Background(), "write", func() { close(checked); <-resume }, nil)
				done <- err
			}()
			awaitAuthorizationBarrier(t, checked)
			// A second connection must be unable to acquire the shared state lock.
			tx, err := h.db.BeginTx(context.Background(), nil)
			require.NoError(t, err)
			if dialect == "postgres" {
				_, err = tx.Exec("SET LOCAL lock_timeout = '100ms'")
			} else {
				_, err = tx.Exec("PRAGMA busy_timeout = 100")
			}
			require.NoError(t, err)
			_, err = tx.Exec("UPDATE host_authorization SET enabled = FALSE WHERE subject = ? AND tenant = ?", h.subject.String(), h.scope.TenantID.String())
			require.Error(t, err, "revocation cannot pass the in-flight protected write's lock")
			require.NoError(t, tx.Rollback())
			close(resume)
			require.NoError(t, <-done)
			require.Equal(t, 1, assignmentCount(t, h))
			_, err = h.revoke(context.Background(), "revoke")
			require.NoError(t, err)
			require.Zero(t, assignmentCount(t, h))
			result, err := h.assign(context.Background(), "later-write", nil, nil)
			require.ErrorIs(t, err, types.ErrUnauthorizedScope)
			require.False(t, result.Committed)
			current, err := h.CurrentAuthorizationVersion(context.Background(), h.subject.String(), h.scope.TenantID.String())
			require.NoError(t, err)
			require.Equal(t, "3", current.Version)
		})
		t.Run("revocation_locks_first", func(t *testing.T) {
			h := newSQLAuthorizationHost(t, dialect)
			locked, resume, attempted := make(chan struct{}), make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-resume:
				default:
					close(resume)
				}
			}()
			revoker := *h
			revoker.afterLock = func() { close(locked); <-resume }
			revoked := make(chan error, 1)
			go func() { _, err := revoker.revoke(context.Background(), "revoke-first"); revoked <- err }()
			awaitAuthorizationBarrier(t, locked)
			writer := *h
			writer.beforeLock = func() { close(attempted) }
			written := make(chan error, 1)
			go func() { _, err := writer.assign(context.Background(), "write-after-revoke", nil, nil); written <- err }()
			awaitAuthorizationBarrier(t, attempted)
			close(resume)
			require.NoError(t, <-revoked)
			require.ErrorIs(t, <-written, types.ErrUnauthorizedScope)
			require.Zero(t, assignmentCount(t, h))
		})
		t.Run("version_failure_rolls_back_grant", func(t *testing.T) {
			h := newSQLAuthorizationHost(t, dialect)
			h.failVersion = fmt.Errorf("version store unavailable")
			result, err := h.assign(context.Background(), "failed-version", nil, nil)
			require.ErrorIs(t, err, h.failVersion)
			require.False(t, result.Committed)
			require.Zero(t, assignmentCount(t, h))
			current, err := h.CurrentAuthorizationVersion(context.Background(), h.subject.String(), h.scope.TenantID.String())
			require.NoError(t, err)
			require.Equal(t, "1", current.Version)
		})
	})
}

func requiredGrantAudit(t *testing.T, h *sqlAuthorizationHost, operation string) func(context.Context, bun.Tx) error {
	t.Helper()
	repo, err := activity.NewRepository(activity.RepositoryConfig{DB: h.db})
	require.NoError(t, err)
	policy := activity.NewRecordPolicy(activity.RecordPolicyConfig{DataSanitizer: func(record types.ActivityRecord) map[string]any {
		return map[string]any{"group_id": record.Data["group_id"], "operation_id": record.Data["operation_id"]}
	}})
	return func(ctx context.Context, tx bun.Tx) error {
		return repo.LogTx(ctx, tx, types.ActivityRecord{
			ID: uuid.NewSHA1(uuid.NameSpaceURL, []byte(operation)), UserID: h.subject, ActorID: h.subject, TenantID: h.scope.TenantID,
			Verb: "role.assigned", ObjectType: "role", ObjectID: h.role.String(), Channel: "authorization", IP: "private", Labels: maps.Clone(h.scope.Labels),
			Data: map[string]any{"group_id": h.scope.Label("group_id").String(), "operation_id": operation, "password": "secret", "token": "secret", "submitted_group_id": "untrusted"},
		}, policy)
	}
}

func assertHostAtomicState(t *testing.T, h *sqlAuthorizationHost, grants, audits, operations int, version string) {
	t.Helper()
	require.Equal(t, grants, assignmentCount(t, h))
	var count int
	require.NoError(t, h.db.NewRaw("SELECT COUNT(*) FROM user_activity").Scan(context.Background(), &count))
	require.Equal(t, audits, count)
	require.NoError(t, h.db.NewRaw("SELECT COUNT(*) FROM host_operations").Scan(context.Background(), &count))
	require.Equal(t, operations, count)
	require.NoError(t, h.db.NewRaw("SELECT COUNT(*) FROM host_outbox").Scan(context.Background(), &count))
	require.Equal(t, operations, count)
	current, err := h.CurrentAuthorizationVersion(context.Background(), h.subject.String(), h.scope.TenantID.String())
	require.NoError(t, err)
	require.Equal(t, version, current.Version)
}

func TestHostRequiredAuditAndPublication(t *testing.T) {
	authorizationDatabases(t, func(t *testing.T, dialect string) {
		t.Run("required_audit_failure_rolls_back", func(t *testing.T) {
			h := newSQLAuthorizationHost(t, dialect)
			// A real DB constraint makes audit persistence fail after the grant INSERT.
			repo, err := activity.NewRepository(activity.RepositoryConfig{DB: h.db})
			require.NoError(t, err)
			id := uuid.New()
			require.NoError(t, repo.Log(context.Background(), types.ActivityRecord{ID: id, Verb: "existing"}))
			result, err := h.assign(context.Background(), "audit-fails", nil, func(ctx context.Context, tx bun.Tx) error {
				return repo.LogTx(ctx, tx, types.ActivityRecord{ID: id, Verb: "duplicate"}, nil)
			})
			require.Error(t, err)
			require.False(t, result.Committed)
			assertHostAtomicState(t, h, 0, 1, 0, "1")
			// Failed operation IDs are not consumed; the repaired request can commit.
			result, err = h.assign(context.Background(), "audit-fails", nil, requiredGrantAudit(t, h, "audit-fails"))
			require.NoError(t, err)
			require.True(t, result.Committed)
			assertHostAtomicState(t, h, 1, 2, 1, "2")
		})
		t.Run("version_failure_rolls_back_required_audit", func(t *testing.T) {
			h := newSQLAuthorizationHost(t, dialect)
			h.failVersion = errors.New("failed version advancement")
			result, err := h.assign(context.Background(), "version-fails", nil, requiredGrantAudit(t, h, "version-fails"))
			require.ErrorIs(t, err, h.failVersion)
			require.False(t, result.Committed)
			assertHostAtomicState(t, h, 0, 0, 0, "1")
		})
		t.Run("durable_publication_retry_does_not_repeat_mutation", func(t *testing.T) {
			h := newSQLAuthorizationHost(t, dialect)
			ctx := context.Background()
			deliveryErr := errors.New("publisher unavailable")
			publisher := auth.AuthorizationVersionPublisherFunc(func(ctx context.Context, event auth.AuthorizationVersionEvent) error {
				// Publication sees only committed grant, required audit and version.
				assertHostAtomicState(t, h, 1, 1, 1, "2")
				return deliveryErr
			})
			calls := 0
			mutate := func(ctx context.Context) error {
				calls++
				tx := ctx.Value(authorizationTxKey{}).(bun.Tx)
				bound, err := h.registry.WithTx(tx)
				if err != nil {
					return err
				}
				if err = command.NewAssignRoleCommand(bound, h.policy(tx, nil)).Execute(ctx, command.AssignRoleInput{UserID: h.subject, RoleID: h.role, Scope: h.scope, Actor: types.ActorRef{ID: h.subject}}); err != nil {
					return err
				}
				return requiredGrantAudit(t, h, "publish")(ctx, tx)
			}
			writer := auth.AuthorizationVersionWriter{Store: h, Publisher: publisher}
			result, err := writer.Advance(ctx, "publish", h.subject.String(), h.scope.TenantID.String(), mutate)
			require.ErrorIs(t, err, deliveryErr)
			require.True(t, result.Committed)
			require.False(t, result.Published)
			assertHostAtomicState(t, h, 1, 1, 1, "2")
			// Retrying the same immutable operation publishes again, skips mutation.
			result, err = writer.Advance(ctx, "publish", h.subject.String(), h.scope.TenantID.String(), mutate)
			require.ErrorIs(t, err, deliveryErr)
			require.True(t, result.Committed)
			require.Equal(t, 1, calls)
			// A host outbox worker can recover after process failure without invoking
			// the command. Consumers must deduplicate at-least-once deliveries by ID.
			var pending []struct {
				Subject   string
				Tenant    string
				Operation string
				Version   int
			}
			require.NoError(t, h.db.NewRaw("SELECT subject,tenant,operation,version FROM host_outbox WHERE published = FALSE").Scan(ctx, &pending))
			require.Len(t, pending, 1)
			delivered := map[string]bool{}
			recoverPublisher := auth.AuthorizationVersionPublisherFunc(func(ctx context.Context, event auth.AuthorizationVersionEvent) error {
				delivered[event.OperationID] = true
				_, publishErr := h.db.ExecContext(ctx, "UPDATE host_outbox SET published = TRUE WHERE subject = ? AND tenant = ? AND operation = ?", event.ApplicationSubject, event.TenantID, event.OperationID)
				return publishErr
			})
			for _, row := range pending {
				event := auth.AuthorizationVersionEvent{OperationID: row.Operation, ApplicationSubject: row.Subject, TenantID: row.Tenant, Version: strconv.Itoa(row.Version)}
				require.NoError(t, recoverPublisher.PublishAuthorizationVersion(ctx, event))
				require.NoError(t, recoverPublisher.PublishAuthorizationVersion(ctx, event))
			}
			require.Len(t, delivered, 1)
			require.Equal(t, 1, calls)
			repo, err := activity.NewRepository(activity.RepositoryConfig{DB: h.db})
			require.NoError(t, err)
			page, err := repo.ListActivity(ctx, types.ActivityFilter{Scope: h.scope})
			require.NoError(t, err)
			require.Len(t, page.Records, 1)
			record := page.Records[0]
			require.Equal(t, h.subject, record.ActorID)
			require.Equal(t, h.scope.TenantID, record.TenantID)
			require.Equal(t, h.scope.Labels, record.Labels)
			require.Empty(t, record.IP)
			require.Equal(t, map[string]any{"group_id": h.scope.Label("group_id").String(), "operation_id": "publish"}, record.Data)
			assertHostAtomicState(t, h, 1, 1, 1, "2")
		})
	})
}
