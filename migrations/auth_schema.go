package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// AuthSchemaCheck describes a table/column requirement for auth-backed schemas.
type AuthSchemaCheck struct {
	Table   string
	Columns []string
}

// DefaultAuthSchemaChecks captures the minimal columns go-users expects on auth tables.
var DefaultAuthSchemaChecks = []AuthSchemaCheck{
	{
		Table: "users",
		Columns: []string{
			"id",
			"email",
			"username",
			"status",
		},
	},
	{
		Table: "password_reset",
		Columns: []string{
			"id",
			"user_id",
			"email",
			"status",
			"jti",
			"issued_at",
			"expires_at",
			"used_at",
			"scope_tenant_id",
			"scope_org_id",
		},
	},
}

// AuthSchemaOption customizes auth schema validation.
type AuthSchemaOption func(*authSchemaConfig)

type authSchemaConfig struct {
	checks []AuthSchemaCheck
}

// WithAuthSchemaChecks replaces the default checks with a custom list.
func WithAuthSchemaChecks(checks []AuthSchemaCheck) AuthSchemaOption {
	return func(cfg *authSchemaConfig) {
		cfg.checks = checks
	}
}

// AuthSchemaValidationError summarizes missing auth tables/columns.
type AuthSchemaValidationError struct {
	MissingTables  []string
	MissingColumns map[string][]string
}

func (e *AuthSchemaValidationError) Error() string {
	if e == nil {
		return ""
	}
	parts := make([]string, 0, 2)
	if len(e.MissingTables) > 0 {
		parts = append(parts, fmt.Sprintf("missing tables: %s", strings.Join(e.MissingTables, ", ")))
	}
	if len(e.MissingColumns) > 0 {
		tableKeys := make([]string, 0, len(e.MissingColumns))
		for table := range e.MissingColumns {
			tableKeys = append(tableKeys, table)
		}
		sort.Strings(tableKeys)
		cols := make([]string, 0, len(tableKeys))
		for _, table := range tableKeys {
			missing := e.MissingColumns[table]
			sort.Strings(missing)
			cols = append(cols, fmt.Sprintf("%s(%s)", table, strings.Join(missing, ", ")))
		}
		parts = append(parts, fmt.Sprintf("missing columns: %s", strings.Join(cols, "; ")))
	}
	if len(parts) == 0 {
		return "auth schema validation failed"
	}
	return "auth schema validation failed: " + strings.Join(parts, "; ")
}

// ValidateAuthSchema ensures auth-owned tables expose columns go-users relies on.
func ValidateAuthSchema(ctx context.Context, db *sql.DB, dialect string, opts ...AuthSchemaOption) error {
	if db == nil {
		return errors.New("migrations: db required")
	}
	normalized := strings.ToLower(strings.TrimSpace(dialect))
	switch normalized {
	case "postgres", "postgresql":
		normalized = "postgres"
	case "sqlite", "sqlite3":
		normalized = "sqlite"
	default:
		return fmt.Errorf("migrations: unsupported dialect %q", dialect)
	}

	cfg := authSchemaConfig{
		checks: DefaultAuthSchemaChecks,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if len(cfg.checks) == 0 {
		return nil
	}

	missingTables := make([]string, 0)
	missingColumns := make(map[string][]string)
	for _, check := range cfg.checks {
		if strings.TrimSpace(check.Table) == "" {
			continue
		}
		cols, err := fetchColumns(ctx, db, normalized, check.Table)
		if err != nil {
			return err
		}
		if len(cols) == 0 {
			missingTables = append(missingTables, check.Table)
			continue
		}
		for _, col := range check.Columns {
			normalizedCol := strings.ToLower(strings.TrimSpace(col))
			if normalizedCol == "" {
				continue
			}
			if !cols[normalizedCol] {
				missingColumns[check.Table] = append(missingColumns[check.Table], normalizedCol)
			}
		}
	}

	if len(missingTables) == 0 && len(missingColumns) == 0 {
		return nil
	}
	sort.Strings(missingTables)
	return &AuthSchemaValidationError{
		MissingTables:  missingTables,
		MissingColumns: missingColumns,
	}
}

func fetchColumns(ctx context.Context, db *sql.DB, dialect, table string) (map[string]bool, error) {
	switch dialect {
	case "postgres":
		return fetchColumnsPostgres(ctx, db, table)
	case "sqlite":
		return fetchColumnsSQLite(ctx, db, table)
	default:
		return nil, fmt.Errorf("migrations: unsupported dialect %q", dialect)
	}
}

func fetchColumnsPostgres(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1
	`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		cols[strings.ToLower(name)] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return cols, nil
}

func fetchColumnsSQLite(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	query := fmt.Sprintf("PRAGMA table_info(%s)", table)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var (
			cid        int
			name       string
			colType    string
			notNull    int
			defaultV   sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultV, &primaryKey); err != nil {
			return nil, err
		}
		cols[strings.ToLower(name)] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return cols, nil
}
