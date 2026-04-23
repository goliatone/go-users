package passwordreset

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
)

func TestRepositoryClaimReleaseAndFinalizeReset(t *testing.T) {
	ctx := context.Background()
	db := newPasswordResetTestDB(t)
	applyPasswordResetDDL(t, db)

	repo, err := NewRepository(RepositoryConfig{DB: db, Clock: fixedRepositoryClock{t: time.Date(2026, 4, 23, 9, 0, 0, 0, time.UTC)}})
	require.NoError(t, err)

	userID := uuid.New()
	insertPasswordResetUser(t, db, userID)

	record, err := repo.CreateReset(ctx, types.PasswordResetRecord{
		UserID:    userID,
		Email:     "user@example.com",
		Status:    types.PasswordResetStatusRequested,
		JTI:       "reset-jti",
		IssuedAt:  time.Date(2026, 4, 23, 9, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2026, 4, 23, 10, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	claimedAt := time.Date(2026, 4, 23, 9, 5, 0, 0, time.UTC)
	require.NoError(t, repo.ClaimReset(ctx, record.JTI, claimedAt, 2*time.Minute))

	stored, err := repo.GetResetByJTI(ctx, record.JTI)
	require.NoError(t, err)
	require.Equal(t, types.PasswordResetStatusProcessing, stored.Status)
	require.Equal(t, claimedAt, stored.UpdatedAt)
	require.True(t, stored.UsedAt.IsZero())

	require.NoError(t, repo.ReleaseResetClaim(ctx, record.JTI, claimedAt))

	stored, err = repo.GetResetByJTI(ctx, record.JTI)
	require.NoError(t, err)
	require.Equal(t, types.PasswordResetStatusRequested, stored.Status)
	require.True(t, stored.UsedAt.IsZero())

	claimedAgainAt := claimedAt.Add(30 * time.Second)
	require.NoError(t, repo.ClaimReset(ctx, record.JTI, claimedAgainAt, 2*time.Minute))

	resetAt := claimedAgainAt.Add(45 * time.Second)
	require.NoError(t, repo.FinalizeReset(ctx, record.JTI, claimedAgainAt, resetAt))

	stored, err = repo.GetResetByJTI(ctx, record.JTI)
	require.NoError(t, err)
	require.Equal(t, types.PasswordResetStatusChanged, stored.Status)
	require.Equal(t, resetAt, stored.UsedAt)
	require.Equal(t, resetAt, stored.ResetAt)
}

func TestRepositoryClaimResetSupportsStaleProcessingClaims(t *testing.T) {
	ctx := context.Background()
	db := newPasswordResetTestDB(t)
	applyPasswordResetDDL(t, db)

	repo, err := NewRepository(RepositoryConfig{DB: db})
	require.NoError(t, err)

	userID := uuid.New()
	insertPasswordResetUser(t, db, userID)

	record, err := repo.CreateReset(ctx, types.PasswordResetRecord{
		UserID:    userID,
		Email:     "user@example.com",
		Status:    types.PasswordResetStatusRequested,
		JTI:       "reset-jti",
		IssuedAt:  time.Date(2026, 4, 23, 9, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2026, 4, 23, 10, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	firstClaim := time.Date(2026, 4, 23, 9, 5, 0, 0, time.UTC)
	require.NoError(t, repo.ClaimReset(ctx, record.JTI, firstClaim, time.Minute))

	err = repo.ClaimReset(ctx, record.JTI, firstClaim.Add(30*time.Second), time.Minute)
	require.Error(t, err)
	require.True(t, repository.IsSQLExpectedCountViolation(err))

	reclaimedAt := firstClaim.Add(2 * time.Minute)
	require.NoError(t, repo.ClaimReset(ctx, record.JTI, reclaimedAt, time.Minute))

	stored, err := repo.GetResetByJTI(ctx, record.JTI)
	require.NoError(t, err)
	require.Equal(t, types.PasswordResetStatusProcessing, stored.Status)
	require.Equal(t, reclaimedAt, stored.UpdatedAt)
}

func newPasswordResetTestDB(t *testing.T) *bun.DB {
	sqldb, err := sql.Open("sqlite3", ":memory:?cache=shared")
	require.NoError(t, err)
	sqldb.SetMaxOpenConns(1)
	db := bun.NewDB(sqldb, sqlitedialect.New())
	t.Cleanup(func() {
		_ = db.Close()
		_ = sqldb.Close()
	})
	return db
}

func applyPasswordResetDDL(t *testing.T, db *bun.DB) {
	for _, path := range []string{
		"../data/sql/migrations/auth/sqlite/00001_users.up.sql",
		"../data/sql/migrations/sqlite/00008_user_tokens.up.sql",
		"../data/sql/migrations/sqlite/00009_password_reset_processing.up.sql",
	} {
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		for _, stmt := range splitPasswordResetStatements(string(content)) {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			_, err := db.Exec(stmt)
			require.NoError(t, err)
		}
	}
}

func insertPasswordResetUser(t *testing.T, db *bun.DB, userID uuid.UUID) {
	_, err := db.Exec(`
		INSERT INTO users (
			id, user_role, first_name, last_name, username, email, metadata
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, userID.String(), "member", "Test", "User", userID.String(), "user@example.com", "{}")
	require.NoError(t, err)
}

func splitPasswordResetStatements(sql string) []string {
	lines := strings.Split(sql, "\n")
	var builder strings.Builder
	var statements []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "--") || line == "---bun:split" {
			continue
		}
		builder.WriteString(line)
		if strings.HasSuffix(line, ";") {
			statements = append(statements, strings.TrimSuffix(builder.String(), ";"))
			builder.Reset()
		} else {
			builder.WriteString(" ")
		}
	}
	if builder.Len() > 0 {
		statements = append(statements, builder.String())
	}
	return statements
}

type fixedRepositoryClock struct {
	t time.Time
}

func (f fixedRepositoryClock) Now() time.Time {
	return f.t
}
