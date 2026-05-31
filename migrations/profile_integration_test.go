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
	sources, err := migrations.ProfileSources(
		migrations.ProfileCombinedWithAuth,
		migrations.WithProfileCoreDependencies("go-auth"),
	)
	if err != nil {
		t.Fatalf("profile sources: %v", err)
	}
	registerOrderedSources(t, client, append([]persistence.OrderedMigrationSource{
		persistence.NewStableOrderedMigrationSource(
			"go-auth",
			authRoot,
			"go-auth",
			10,
			persistence.WithOrderedMigrationDialectOptions(
				persistence.WithDialectSourceLabel("go-auth"),
				persistence.WithValidationTargets("postgres", "sqlite"),
			),
		),
	}, migrations.StableOrderedSources(sources)...))

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

func TestProfileStandaloneStablePlanUsesSourceStableNames(t *testing.T) {
	ctx := context.Background()
	client, db := newSQLiteClient(t, "standalone-stable-plan")
	defer func() { _ = db.Close() }()

	sources, err := migrations.StableOrderedProfileSources(migrations.ProfileStandalone)
	if err != nil {
		t.Fatalf("stable profile sources: %v", err)
	}
	registerOrderedSources(t, client, sources)

	plan, err := client.Plan(ctx)
	if err != nil {
		t.Fatalf("plan stable standalone profile: %v", err)
	}

	auth := planEntryBySourceAndVersion(t, plan, "auth-bootstrap", "00001")
	if auth.SyntheticName != "ordsrc_000010_go_users_auth_00001" {
		t.Fatalf("auth synthetic name = %q", auth.SyntheticName)
	}
	assertStablePlanEntry(t, auth, "go_users_auth", 10, nil)

	extras := planEntryBySourceAndVersion(t, plan, "auth-extras", "00010")
	if extras.SyntheticName != "ordsrc_000020_go_users_auth_extras_00010" {
		t.Fatalf("auth extras synthetic name = %q", extras.SyntheticName)
	}
	assertStablePlanEntry(t, extras, "go_users_auth_extras", 20, []string{"go_users_auth"})

	core := planEntryBySourceAndVersion(t, plan, "core", "00003")
	if core.SyntheticName != "ordsrc_000030_go_users_00003" {
		t.Fatalf("core synthetic name = %q", core.SyntheticName)
	}
	assertStablePlanEntry(t, core, "go_users", 30, []string{"go_users_auth_extras"})
}

func TestProfileCombinedStablePlanWithExternalDependency(t *testing.T) {
	ctx := context.Background()
	client, db := newSQLiteClient(t, "combined-stable-plan")
	defer func() { _ = db.Close() }()

	authRoot := canonicalGoAuthMigrationsRoot(t)
	usersSources, err := migrations.StableOrderedProfileSources(
		migrations.ProfileCombinedWithAuth,
		migrations.WithProfileCoreDependencies("go-auth"),
	)
	if err != nil {
		t.Fatalf("stable profile sources: %v", err)
	}
	sources := append([]persistence.OrderedMigrationSource{
		persistence.NewStableOrderedMigrationSource(
			"go-auth",
			authRoot,
			"go-auth",
			10,
			persistence.WithOrderedMigrationDialectOptions(
				persistence.WithDialectSourceLabel("go-auth"),
				persistence.WithValidationTargets("postgres", "sqlite"),
			),
		),
	}, usersSources...)
	registerOrderedSources(t, client, sources)

	plan, err := client.Plan(ctx)
	if err != nil {
		t.Fatalf("plan stable combined profile: %v", err)
	}

	core := planEntryBySourceAndVersion(t, plan, "core", "00003")
	if core.SyntheticName != "ordsrc_000030_go_users_00003" {
		t.Fatalf("core synthetic name = %q", core.SyntheticName)
	}
	assertStablePlanEntry(t, core, "go_users", 30, []string{"go_auth"})
}

func TestProfileStableNamesSurviveUnrelatedSourceInsertion(t *testing.T) {
	ctx := context.Background()

	baselineClient, baselineDB := newSQLiteClient(t, "stable-insertion-baseline")
	defer func() { _ = baselineDB.Close() }()
	baselineSources, err := migrations.StableOrderedProfileSources(migrations.ProfileStandalone)
	if err != nil {
		t.Fatalf("baseline stable profile sources: %v", err)
	}
	registerOrderedSources(t, baselineClient, baselineSources)
	baselinePlan, err := baselineClient.Plan(ctx)
	if err != nil {
		t.Fatalf("baseline plan: %v", err)
	}
	baselineCore := planEntryBySourceAndVersion(t, baselinePlan, "core", "00003")

	insertedClient, insertedDB := newSQLiteClient(t, "stable-insertion-added")
	defer func() { _ = insertedDB.Close() }()
	insertedSources, err := migrations.StableOrderedProfileSources(migrations.ProfileStandalone)
	if err != nil {
		t.Fatalf("inserted stable profile sources: %v", err)
	}
	unrelated := persistence.NewStableOrderedMigrationSource(
		"unrelated",
		fstest.MapFS{
			"00001_unrelated.up.sql":   {Data: []byte("SELECT 1;")},
			"00001_unrelated.down.sql": {Data: []byte("SELECT 1;")},
		},
		"app-unrelated",
		25,
		persistence.WithOrderedMigrationDependencies("go-users-auth-extras"),
		persistence.WithOrderedMigrationDialectOptions(
			persistence.WithDialectSourceLabel("app-unrelated"),
			persistence.WithValidationTargets("sqlite"),
		),
	)
	registerOrderedSources(t, insertedClient, append([]persistence.OrderedMigrationSource{unrelated}, insertedSources...))
	insertedPlan, err := insertedClient.Plan(ctx)
	if err != nil {
		t.Fatalf("inserted plan: %v", err)
	}
	insertedCore := planEntryBySourceAndVersion(t, insertedPlan, "core", "00003")

	if insertedCore.SyntheticName != baselineCore.SyntheticName {
		t.Fatalf("core synthetic name changed after insertion: got %q want %q", insertedCore.SyntheticName, baselineCore.SyntheticName)
	}
	if insertedCore.SourceOrder != baselineCore.SourceOrder || insertedCore.SourceKey != baselineCore.SourceKey {
		t.Fatalf("core stable metadata changed after insertion: got %+v want %+v", insertedCore, baselineCore)
	}
}

func TestProfileStandaloneBackfillsLegacyPositionalMarkers(t *testing.T) {
	ctx := context.Background()
	client, db := newSQLiteClient(t, "standalone-backfill")
	defer func() { _ = db.Close() }()

	profileSources, err := migrations.ProfileSources(migrations.ProfileStandalone)
	if err != nil {
		t.Fatalf("profile sources: %v", err)
	}
	legacySources := orderedProfileSources(profileSources)
	registerOrderedSources(t, client, legacySources)
	if err = client.Migrate(ctx); err != nil {
		t.Fatalf("legacy positional migrate: %v", err)
	}

	stable := persistence.NewMigrations()
	if err = stable.RegisterOrderedMigrationSources(migrations.StableOrderedSources(profileSources)...); err != nil {
		t.Fatalf("register stable sources: %v", err)
	}
	if err = stable.BackfillStableOrderedMigrationMarkers(ctx, client.DB(), legacyOrderedProfileSourcesForBackfill(profileSources)); err != nil {
		t.Fatalf("backfill stable markers: %v", err)
	}

	plan, err := stable.Plan(ctx, client.DB())
	if err != nil {
		t.Fatalf("stable plan after backfill: %v", err)
	}
	for _, entry := range plan.Entries {
		if strings.HasPrefix(entry.SyntheticName, "ordsrc_") && !entry.Applied {
			t.Fatalf("stable entry %q was not marked applied after backfill", entry.SyntheticName)
		}
	}

	core := planEntryBySourceAndVersion(t, plan, "core", "00003")
	if core.SyntheticName != "ordsrc_000030_go_users_00003" {
		t.Fatalf("core synthetic name = %q", core.SyntheticName)
	}
	if !core.Applied {
		t.Fatalf("expected core migration to be applied after backfill")
	}

	var aliasCount int
	if err := client.DB().NewSelect().
		TableExpr("bun_ordered_migration_aliases").
		ColumnExpr("COUNT(*)").
		Where("stable_name LIKE ?", "ordsrc_%").
		Scan(ctx, &aliasCount); err != nil {
		t.Fatalf("count ordered migration aliases: %v", err)
	}
	if aliasCount == 0 {
		t.Fatalf("expected stable ordered migration aliases to be created")
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
	registerOrderedSources(t, client, migrations.StableOrderedSources(sources))
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

func legacyOrderedProfileSourcesForBackfill(sources []migrations.ProfileSource) []persistence.OrderedMigrationSource {
	ordered := orderedProfileSources(sources)
	for idx := range ordered {
		ordered[idx].SourceKey = sources[idx].SourceKey
	}
	return ordered
}

func registerOrderedSources(t *testing.T, client *persistence.Client, sources []persistence.OrderedMigrationSource) {
	t.Helper()
	if err := client.RegisterOrderedMigrationSources(sources...); err != nil {
		t.Fatalf("register ordered migrations: %v", err)
	}
}

func planEntryBySourceAndVersion(t *testing.T, plan *persistence.MigrationPlan, sourceName, version string) persistence.MigrationPlanEntry {
	t.Helper()

	for _, entry := range plan.Entries {
		if entry.SourceName == sourceName && entry.OriginalVersion == version {
			return entry
		}
	}
	t.Fatalf("missing plan entry for source %q version %q", sourceName, version)
	return persistence.MigrationPlanEntry{}
}

func assertStablePlanEntry(t *testing.T, entry persistence.MigrationPlanEntry, sourceKey string, order int, dependsOn []string) {
	t.Helper()

	if entry.IdentityMode != persistence.OrderedMigrationIdentitySourceStable {
		t.Fatalf("entry %q identity mode = %s, want source-stable", entry.SyntheticName, entry.IdentityMode.String())
	}
	if entry.SourceKey != sourceKey {
		t.Fatalf("entry %q source key = %q, want %q", entry.SyntheticName, entry.SourceKey, sourceKey)
	}
	if entry.SourceOrder != order {
		t.Fatalf("entry %q source order = %d, want %d", entry.SyntheticName, entry.SourceOrder, order)
	}
	if !equalSlices(entry.SourceDependsOn, dependsOn) {
		t.Fatalf("entry %q dependencies = %v, want %v", entry.SyntheticName, entry.SourceDependsOn, dependsOn)
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
		data, readErr := fs.ReadFile(root, path)
		if readErr != nil {
			return readErr
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
	defer func() { _ = rows.Close() }()

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
	defer func() { _ = rows.Close() }()

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
