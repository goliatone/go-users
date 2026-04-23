package migrations_test

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	auth "github.com/goliatone/go-auth"
	persistence "github.com/goliatone/go-persistence-bun"
	users "github.com/goliatone/go-users"
	"github.com/goliatone/go-users/migrations"
	_ "github.com/mattn/go-sqlite3"
	"github.com/uptrace/bun/dialect/sqlitedialect"
)

type profileTestPersistenceConfig struct {
	driver string
	server string
}

func (c profileTestPersistenceConfig) GetDebug() bool {
	return false
}

func (c profileTestPersistenceConfig) GetDriver() string {
	return c.driver
}

func (c profileTestPersistenceConfig) GetServer() string {
	return c.server
}

func (c profileTestPersistenceConfig) GetPingTimeout() time.Duration {
	return time.Second
}

func (c profileTestPersistenceConfig) GetOtelIdentifier() string {
	return ""
}

func TestAuthCompatibilityTracksMatchGoAuthSQLite(t *testing.T) {
	ctx := context.Background()

	authClient, authDB := newSQLiteClient(t, "goauth-parity")
	defer func() { _ = authDB.Close() }()

	authRoot := canonicalGoAuthMigrationsRoot(t)
	authClient.RegisterDialectMigrations(
		authRoot,
		persistence.WithDialectSourceLabel("go-auth"),
		persistence.WithValidationTargets("postgres", "sqlite"),
	)
	if err := authClient.ValidateDialects(ctx); err != nil {
		t.Fatalf("validate go-auth dialects: %v", err)
	}
	if err := authClient.Migrate(ctx); err != nil {
		t.Fatalf("migrate go-auth schema: %v", err)
	}

	compatClient, compatDB := newSQLiteClient(t, "go-users-compat")
	defer func() { _ = compatDB.Close() }()

	authBootstrapFS, err := fs.Sub(users.GetAuthBootstrapMigrationsFS(), "data/sql/migrations/auth")
	if err != nil {
		t.Fatalf("auth bootstrap fs: %v", err)
	}
	authExtrasFS, err := fs.Sub(users.GetAuthExtrasMigrationsFS(), "data/sql/migrations/auth_extras")
	if err != nil {
		t.Fatalf("auth extras fs: %v", err)
	}
	compatClient.RegisterDialectMigrations(
		authBootstrapFS,
		persistence.WithDialectSourceLabel("go-users-auth"),
		persistence.WithValidationTargets("postgres", "sqlite"),
	)
	compatClient.RegisterDialectMigrations(
		authExtrasFS,
		persistence.WithDialectSourceLabel("go-users-auth-extras"),
		persistence.WithValidationTargets("postgres", "sqlite"),
	)
	if err := compatClient.ValidateDialects(ctx); err != nil {
		t.Fatalf("validate go-users compatibility dialects: %v", err)
	}
	if err := compatClient.Migrate(ctx); err != nil {
		t.Fatalf("migrate go-users compatibility schema: %v", err)
	}

	tables := []string{"users", "password_reset", "social_accounts", "user_identifiers"}
	for _, table := range tables {
		wantCols := mustTableColumns(t, authDB, table)
		gotCols := mustTableColumns(t, compatDB, table)
		if !equalSlices(wantCols, gotCols) {
			t.Fatalf("columns mismatch for table %s: got=%v want=%v", table, gotCols, wantCols)
		}

		wantIndexes := mustNamedIndexes(t, authDB, table)
		gotIndexes := mustNamedIndexes(t, compatDB, table)
		if !equalSlices(wantIndexes, gotIndexes) {
			t.Fatalf("indexes mismatch for table %s: got=%v want=%v", table, gotIndexes, wantIndexes)
		}
	}
}

func TestProfileStandaloneSQLiteMigrateRollback(t *testing.T) {
	ctx := context.Background()
	client, db := newSQLiteClient(t, "standalone")
	defer func() { _ = db.Close() }()

	sources, err := migrations.ProfileSources(migrations.ProfileStandalone)
	if err != nil {
		t.Fatalf("profile sources: %v", err)
	}
	registerProfileSources(t, client, sources)

	if err := client.ValidateDialects(ctx); err != nil {
		t.Fatalf("validate dialects: %v", err)
	}
	if err := client.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !tableExists(ctx, db, "users") || !tableExists(ctx, db, "social_accounts") || !tableExists(ctx, db, "custom_roles") {
		t.Fatalf("expected users, social_accounts, and custom_roles tables after migrate")
	}

	if err := client.RollbackAll(ctx); err != nil {
		t.Fatalf("rollback all: %v", err)
	}
	if tableExists(ctx, db, "custom_roles") {
		t.Fatalf("expected custom_roles table removed after rollback")
	}
}

func TestProfileCombinedWithGoAuthSQLiteMigrateRollback(t *testing.T) {
	ctx := context.Background()
	client, db := newSQLiteClient(t, "combined")
	defer func() { _ = db.Close() }()

	authRoot := canonicalGoAuthMigrationsRoot(t)
	sources, err := migrations.ProfileSources(migrations.ProfileCombinedWithAuth)
	if err != nil {
		t.Fatalf("profile sources: %v", err)
	}
	registerOrderedSources(t, client, append([]persistence.OrderedMigrationSource{
		{
			Name: "go-auth",
			Root: authRoot,
			Options: []persistence.DialectMigrationOption{
				persistence.WithDialectSourceLabel("go-auth"),
				persistence.WithValidationTargets("postgres", "sqlite"),
			},
		},
	}, orderedProfileSources(sources)...))

	if err := client.ValidateDialects(ctx); err != nil {
		t.Fatalf("validate dialects: %v", err)
	}
	if err := client.Migrate(ctx); err != nil {
		t.Fatalf("migrate combined profile: %v", err)
	}
	if err := client.Migrate(ctx); err != nil {
		t.Fatalf("second migrate should be idempotent: %v", err)
	}

	if !tableExists(ctx, db, "users") || !tableExists(ctx, db, "custom_roles") {
		t.Fatalf("expected users and custom_roles tables after migrate")
	}

	if err := client.RollbackAll(ctx); err != nil {
		t.Fatalf("rollback all: %v", err)
	}
	if tableExists(ctx, db, "users") || tableExists(ctx, db, "custom_roles") {
		t.Fatalf("expected users and custom_roles tables removed after rollback")
	}
}

func newSQLiteClient(t *testing.T, name string) (*persistence.Client, *sql.DB) {
	t.Helper()

	dsn := "file:" + filepath.Join(t.TempDir(), fmt.Sprintf("%s.db", name)) + "?cache=shared&_fk=1"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	cfg := profileTestPersistenceConfig{
		driver: "sqlite3",
		server: dsn,
	}
	client, err := persistence.New(cfg, db, sqlitedialect.New())
	if err != nil {
		_ = db.Close()
		t.Fatalf("persistence.New: %v", err)
	}
	return client, db
}

func registerProfileSources(t *testing.T, client *persistence.Client, sources []migrations.ProfileSource) {
	t.Helper()
	registerOrderedSources(t, client, orderedProfileSources(sources))
}

func orderedProfileSources(sources []migrations.ProfileSource) []persistence.OrderedMigrationSource {
	ordered := make([]persistence.OrderedMigrationSource, 0, len(sources))
	for _, source := range sources {
		opts := []persistence.DialectMigrationOption{
			persistence.WithDialectSourceLabel(source.SourceLabel),
		}
		if len(source.ValidationTargets) > 0 {
			opts = append(opts, persistence.WithValidationTargets(source.ValidationTargets...))
		}
		ordered = append(ordered, persistence.OrderedMigrationSource{
			Name:    source.Name,
			Root:    source.Filesystem,
			Options: opts,
		})
	}
	return ordered
}

func registerOrderedSources(t *testing.T, client *persistence.Client, sources []persistence.OrderedMigrationSource) {
	t.Helper()
	if err := client.RegisterOrderedMigrationSources(sources...); err != nil {
		t.Fatalf("register ordered migrations: %v", err)
	}
}

func canonicalGoAuthMigrationsRoot(t *testing.T) fs.FS {
	t.Helper()

	root, err := fs.Sub(auth.GetMigrationsFS(), "data/sql/migrations")
	if err != nil {
		t.Fatalf("go-auth migrations root: %v", err)
	}

	hasTimestampIdentifiers := fileExists(root, "20240701090000_auth0_identifiers.up.sql")
	if !hasTimestampIdentifiers {
		return root
	}

	filtered := fstest.MapFS{}
	err = fs.WalkDir(root, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if path == "0001_auth0_identifiers.up.sql" ||
			path == "0001_auth0_identifiers.down.sql" ||
			path == "sqlite/0001_auth0_identifiers.up.sql" ||
			path == "sqlite/0001_auth0_identifiers.down.sql" {
			return nil
		}
		data, err := fs.ReadFile(root, path)
		if err != nil {
			return err
		}
		filtered[path] = &fstest.MapFile{Data: data}
		return nil
	})
	if err != nil {
		t.Fatalf("filter go-auth migrations: %v", err)
	}
	return filtered
}

func fileExists(filesystem fs.FS, path string) bool {
	_, err := fs.Stat(filesystem, path)
	return err == nil
}

func tableExists(ctx context.Context, db *sql.DB, table string) bool {
	var name string
	err := db.QueryRowContext(
		ctx,
		"SELECT name FROM sqlite_master WHERE type='table' AND name = ?",
		table,
	).Scan(&name)
	return err == nil && name == table
}

func mustTableColumns(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()

	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		t.Fatalf("table info %s: %v", table, err)
	}
	defer rows.Close()

	cols := make([]string, 0, 16)
	for rows.Next() {
		var (
			cid        int
			name       string
			colType    string
			notNull    int
			defaultVal sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultVal, &primaryKey); err != nil {
			t.Fatalf("scan table info %s: %v", table, err)
		}
		cols = append(cols, strings.ToLower(name))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table info %s: %v", table, err)
	}
	sort.Strings(cols)
	return cols
}

func mustNamedIndexes(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()

	rows, err := db.Query(fmt.Sprintf("PRAGMA index_list(%s)", table))
	if err != nil {
		t.Fatalf("index list %s: %v", table, err)
	}
	defer rows.Close()

	indexes := make([]string, 0, 8)
	for rows.Next() {
		var (
			seq     int
			name    string
			unique  int
			origin  string
			partial int
		)
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			t.Fatalf("scan index list %s: %v", table, err)
		}
		if strings.HasPrefix(name, "sqlite_autoindex_") {
			continue
		}
		indexes = append(indexes, strings.ToLower(name))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate index list %s: %v", table, err)
	}
	sort.Strings(indexes)
	return indexes
}

func equalSlices[T comparable](left, right []T) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx] != right[idx] {
			return false
		}
	}
	return true
}
