package activity

import (
	"context"
	"database/sql"
	"encoding/json"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
)

func TestActivityIngestionPrerequisites(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		name := "sqlite"
		if postgres {
			name = "postgres"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			var db *bun.DB
			prefix := "../data/sql/migrations/"
			if postgres {
				dsn := os.Getenv("GO_USERS_TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("set GO_USERS_TEST_POSTGRES_DSN to a disposable PostgreSQL database")
				}
				base, err := sql.Open("postgres", dsn)
				require.NoError(t, err)
				schema := "ingestion_" + strings.ReplaceAll(uuid.NewString(), "-", "")
				_, err = base.Exec(`CREATE SCHEMA "` + schema + `"`)
				require.NoError(t, err)
				t.Cleanup(func() { _, _ = base.Exec(`DROP SCHEMA "` + schema + `" CASCADE`); _ = base.Close() })
				u, err := url.Parse(dsn)
				require.NoError(t, err)
				q := u.Query()
				q.Set("search_path", schema)
				u.RawQuery = q.Encode()
				raw, err := sql.Open("postgres", u.String())
				require.NoError(t, err)
				db = bun.NewDB(raw, pgdialect.New())
			} else {
				raw, err := sql.Open("sqlite3", "file:"+filepath.Join(t.TempDir(), "activity.db"))
				require.NoError(t, err)
				db = bun.NewDB(raw, sqlitedialect.New())
				prefix += "sqlite/"
			}
			t.Cleanup(func() { _ = db.Close() })
			for _, migration := range []string{"00004_user_activity.up.sql", "00011_activity_scopes.tx.up.sql"} {
				raw, err := os.ReadFile(prefix + migration)
				require.NoError(t, err)
				_, err = db.Exec(string(raw))
				require.NoError(t, err)
			}
			repo, err := NewRepository(RepositoryConfig{DB: db})
			require.NoError(t, err)
			tenant, org := uuid.New(), uuid.New()
			scope := types.ScopeFilter{TenantID: tenant, OrgID: org}
			expected := map[string]string{"huge": "1e309", "large": "9007199254740993", "amount": "0.01234567890123456789"}
			data := map[string]any{}
			for k, v := range expected {
				data[k] = json.Number(v)
			}
			record := types.ActivityRecord{ID: uuid.New(), ActorID: uuid.New(), TenantID: tenant, OrgID: org, Verb: "invoice.approved", ObjectID: "human", OccurredAt: time.Now().UTC().Truncate(time.Microsecond), Data: data}
			policy := NewRecordPolicy(RecordPolicyConfig{DataSanitizer: func(r types.ActivityRecord) map[string]any { return r.Data }})
			require.NoError(t, db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error { return repo.LogTx(ctx, tx, record, policy) }))
			page, err := repo.ListActivity(ctx, types.ActivityFilter{Scope: scope})
			require.NoError(t, err)
			require.Len(t, page.Records, 1)
			for k, v := range expected {
				n, ok := page.Records[0].Data[k].(json.Number)
				require.True(t, ok, "%s lost json.Number", k)
				got, ok := new(big.Rat).SetString(string(n))
				require.True(t, ok)
				want, ok := new(big.Rat).SetString(v)
				require.True(t, ok)
				require.Zero(t, got.Cmp(want))
			}
			for i, metadata := range []map[string]any{{}, {"system": true}, {"actor_type": "job"}, {"actor": map[string]any{"type": "system"}}} {
				copy := record
				copy.ID = uuid.New()
				copy.ObjectID = "legacy"
				copy.Data = metadata
				if i == 0 {
					copy.ObjectID = "human"
				}
				require.NoError(t, repo.Log(ctx, copy))
			}
			disabled := false
			filter := types.ActivityFilter{Scope: scope, MachineActivityEnabled: &disabled, MachineActorTypes: DefaultMachineActorTypes(), MachineDataKeys: DefaultMachineDataKeys(), Pagination: types.Pagination{Limit: 1}}
			page, err = repo.ListActivity(ctx, filter)
			require.NoError(t, err)
			require.Equal(t, 2, page.Total)
			require.Len(t, page.Records, 1)
			stats, err := repo.ActivityStats(ctx, types.ActivityStatsFilter{Scope: scope, MachineActivityEnabled: &disabled, MachineActorTypes: DefaultMachineActorTypes(), MachineDataKeys: DefaultMachineDataKeys()})
			require.NoError(t, err)
			require.Equal(t, 2, stats.Total)
			// Host-owned classification applies before count/limit/cursor/statistics and
			// does not remove scope predicates. Empty alias lists still invoke it.
			calls := 0
			override, err := NewRepository(RepositoryConfig{DB: db, MachineActivityFilter: func(q *bun.SelectQuery, _, _ []string) *bun.SelectQuery {
				calls++
				return q.Where("object_id = ?", "human")
			}})
			require.NoError(t, err)
			filter.MachineDataKeys = nil
			filter.MachineActorTypes = nil
			page, err = override.ListActivity(ctx, filter)
			require.NoError(t, err)
			require.Equal(t, 2, page.Total)
			require.Len(t, page.Records, 1)
			rows, err := override.ListActivityCursor(ctx, filter, nil)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			stats, err = override.ActivityStats(ctx, types.ActivityStatsFilter{Scope: scope, MachineActivityEnabled: &disabled})
			require.NoError(t, err)
			require.Equal(t, 2, stats.Total)
			filter.Scope.OrgID = uuid.New()
			page, err = override.ListActivity(ctx, filter)
			require.NoError(t, err)
			require.Zero(t, page.Total)
			require.GreaterOrEqual(t, calls, 4)
		})
	}
}
