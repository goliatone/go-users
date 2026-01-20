package migrations_test

import (
	"context"
	"database/sql"
	"io/fs"
	"sort"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	users "github.com/goliatone/go-users"
	"github.com/goliatone/go-users/migrations"
)

func TestMigrationsApplyToSQLite(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	ctx := context.Background()
	if len(migrations.Filesystems()) == 0 {
		t.Fatalf("no migration filesystems registered")
	}

	authFS, err := fs.Sub(users.GetAuthBootstrapMigrationsFS(), "data/sql/migrations/auth/sqlite")
	if err != nil {
		t.Fatalf("failed to load auth bootstrap migrations: %v", err)
	}
	if err := applyFilesystem(ctx, db, authFS); err != nil {
		t.Fatalf("failed to apply auth bootstrap migrations: %v", err)
	}

	coreFS, err := fs.Sub(users.GetCoreMigrationsFS(), "data/sql/migrations/sqlite")
	if err != nil {
		t.Fatalf("failed to load core migrations: %v", err)
	}
	if err := applyFilesystem(ctx, db, coreFS); err != nil {
		t.Fatalf("failed to apply core migrations: %v", err)
	}

	var tableName string
	if err := db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name='custom_roles'").Scan(&tableName); err != nil {
		t.Fatalf("failed to verify custom_roles table: %v", err)
	}
	if tableName != "custom_roles" {
		t.Fatalf("expected custom_roles table, got %q", tableName)
	}
}

func applyFilesystem(ctx context.Context, db *sql.DB, filesystem fs.FS) error {
	entries, err := fs.Glob(filesystem, "*.up.sql")
	if err != nil {
		return err
	}
	sort.Strings(entries)
	for _, entry := range entries {
		sqlBytes, err := fs.ReadFile(filesystem, entry)
		if err != nil {
			return err
		}
		statements := splitStatements(string(sqlBytes))
		for _, stmt := range statements {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
	}
	return nil
}

func splitStatements(sql string) []string {
	parts := strings.Split(sql, ";")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
