package activity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
)

func TestUsersActivityHookMapsEnvelopePhases(t *testing.T) {
	sink := &recordingHookSink{}
	hook := &UsersActivityHook{
		Sink:   sink,
		Mapper: EnvelopeMapper{},
	}

	actorID := uuid.New()
	tenantID := uuid.New()
	occurredAt := time.Date(2026, time.February, 23, 8, 30, 0, 0, time.UTC)
	baseMetadata := map[string]any{"custom": "value"}

	for _, phase := range []string{"attempted", "committed", "rejected"} {
		err := hook.Notify(context.Background(), ActivityEnvelope{
			Channel:    "fsm",
			Verb:       fmt.Sprintf("fsm.transition.%s", phase),
			ObjectType: "fsm.machine",
			ObjectID:   "order-1",
			ActorID:    actorID.String(),
			TenantID:   tenantID.String(),
			OccurredAt: occurredAt,
			Metadata:   baseMetadata,
		})
		require.NoError(t, err)
	}

	require.Len(t, sink.records, 3)
	require.Equal(t, "value", baseMetadata["custom"])
	require.NotContains(t, baseMetadata, DataKeySessionID)

	require.Equal(t, "fsm.transition.attempted", sink.records[0].Verb)
	require.Equal(t, "fsm.transition.committed", sink.records[1].Verb)
	require.Equal(t, "fsm.transition.rejected", sink.records[2].Verb)

	for _, record := range sink.records {
		require.Equal(t, "fsm", record.Channel)
		require.Equal(t, "fsm.machine", record.ObjectType)
		require.Equal(t, "order-1", record.ObjectID)
		require.Equal(t, actorID, record.ActorID)
		require.Equal(t, tenantID, record.TenantID)
	}
	require.Equal(t, occurredAt, sink.records[0].OccurredAt)
}

func TestUsersActivityHookSessionAndEnricherIntegration(t *testing.T) {
	sink := &recordingHookSink{}
	hook := &UsersActivityHook{
		Sink:   sink,
		Mapper: EnvelopeMapper{},
		SessionProvider: SessionIDProviderFunc(func(context.Context) (string, bool) {
			return "session-123", true
		}),
		Enricher: EnricherFunc(func(_ context.Context, record types.ActivityRecord) (types.ActivityRecord, error) {
			out := record
			out.Data = cloneMetadata(record.Data)
			out.Data["enriched"] = "ok"
			return out, nil
		}),
	}

	err := hook.Notify(context.Background(), ActivityEnvelope{
		Channel:    "fsm",
		Verb:       "fsm.transition.committed",
		ObjectType: "fsm.machine",
		ObjectID:   "payment-1",
		ActorID:    uuid.NewString(),
	})
	require.NoError(t, err)
	require.Len(t, sink.records, 1)
	require.Equal(t, "session-123", sink.records[0].Data[DataKeySessionID])
	require.Equal(t, "ok", sink.records[0].Data["enriched"])
}

func TestUsersActivityHookEnrichmentErrorHandling(t *testing.T) {
	event := ActivityEnvelope{
		Channel:    "fsm",
		Verb:       "fsm.transition.committed",
		ObjectType: "fsm.machine",
		ObjectID:   "order-1",
	}

	failFastSink := &recordingHookSink{}
	failFast := &UsersActivityHook{
		Sink:   failFastSink,
		Mapper: EnvelopeMapper{},
		Enricher: EnricherFunc(func(context.Context, types.ActivityRecord) (types.ActivityRecord, error) {
			return types.ActivityRecord{}, errors.New("enrichment failed")
		}),
	}
	err := failFast.Notify(context.Background(), event)
	require.Error(t, err)
	require.Len(t, failFastSink.records, 0)

	bestEffortSink := &recordingHookSink{}
	bestEffort := &UsersActivityHook{
		Sink:   bestEffortSink,
		Mapper: EnvelopeMapper{},
		Enricher: EnricherFunc(func(context.Context, types.ActivityRecord) (types.ActivityRecord, error) {
			return types.ActivityRecord{}, errors.New("enrichment failed")
		}),
		ErrorHandler: func(_ context.Context, _ error, _ ActivityEnricher, current types.ActivityRecord, _ types.ActivityRecord) (types.ActivityRecord, error) {
			return current, nil
		},
	}
	err = bestEffort.Notify(context.Background(), event)
	require.NoError(t, err)
	require.Len(t, bestEffortSink.records, 1)
}

func TestUsersActivityHookValidatesDependencies(t *testing.T) {
	hook := &UsersActivityHook{Sink: &recordingHookSink{}}
	err := hook.Notify(context.Background(), ActivityEnvelope{})
	require.ErrorIs(t, err, ErrMissingActivityMapper)

	hook = &UsersActivityHook{Mapper: EnvelopeMapper{}}
	err = hook.Notify(context.Background(), ActivityEnvelope{})
	require.ErrorIs(t, err, types.ErrMissingActivitySink)
}

func TestUsersActivityHookMapperErrorPropagates(t *testing.T) {
	hook := &UsersActivityHook{
		Sink:   &recordingHookSink{},
		Mapper: EnvelopeMapper{},
	}
	err := hook.Notify(context.Background(), struct{}{})
	require.ErrorIs(t, err, ErrUnsupportedActivityEvent)
}

func TestUsersActivityHookReadSideCompatibility(t *testing.T) {
	ctx := context.Background()
	db := newUsersHookActivityTestDB(t)
	applyUsersHookActivityMigration(t, db)

	store, err := NewRepository(RepositoryConfig{DB: db})
	require.NoError(t, err)

	hook := &UsersActivityHook{
		Sink:   store,
		Mapper: EnvelopeMapper{},
		SessionProvider: SessionIDProviderFunc(func(context.Context) (string, bool) {
			return "sess-feed", true
		}),
	}

	err = hook.Notify(ctx, ActivityEnvelope{
		Channel:    "fsm",
		Verb:       "fsm.transition.committed",
		ObjectType: "fsm.machine",
		ObjectID:   "order-99",
		ActorID:    uuid.NewString(),
		Metadata: map[string]any{
			"machine_id": "orders",
			"source":     "fsm",
		},
	})
	require.NoError(t, err)

	page, err := store.ListActivity(ctx, types.ActivityFilter{
		Channel:    "fsm",
		ObjectType: "fsm.machine",
		ObjectID:   "order-99",
		Verbs:      []string{"fsm.transition.committed"},
		Pagination: types.Pagination{Limit: 5},
	})
	require.NoError(t, err)
	require.Len(t, page.Records, 1)
	require.Equal(t, "order-99", page.Records[0].ObjectID)
	require.Equal(t, "orders", page.Records[0].Data["machine_id"])
	require.Equal(t, "sess-feed", page.Records[0].Data[DataKeySessionID])
	require.Equal(t, "fsm", page.Records[0].Data["source"])
}

func TestEnvelopeMapperDefaults(t *testing.T) {
	mapper := EnvelopeMapper{
		DefaultChannel:    "fallback",
		DefaultObjectType: "generic.object",
	}
	record, err := mapper.Map(context.Background(), ActivityEnvelope{
		Verb:     "custom.action",
		ObjectID: "obj-1",
	})
	require.NoError(t, err)
	require.Equal(t, "fallback", record.Channel)
	require.Equal(t, "generic.object", record.ObjectType)
}

type recordingHookSink struct {
	records []types.ActivityRecord
}

func (s *recordingHookSink) Log(_ context.Context, record types.ActivityRecord) error {
	out := record
	out.Data = cloneMetadata(record.Data)
	s.records = append(s.records, out)
	return nil
}

func newUsersHookActivityTestDB(t *testing.T) *bun.DB {
	sqlDB, err := sql.Open("sqlite3", ":memory:?cache=shared")
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	db := bun.NewDB(sqlDB, sqlitedialect.New())
	t.Cleanup(func() {
		_ = db.Close()
		_ = sqlDB.Close()
	})
	return db
}

func applyUsersHookActivityMigration(t *testing.T, db *bun.DB) {
	content, err := os.ReadFile("../data/sql/migrations/sqlite/00004_user_activity.up.sql")
	require.NoError(t, err)

	for _, stmt := range splitUsersHookSQLStatements(string(content)) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
}

func splitUsersHookSQLStatements(sqlText string) []string {
	lines := strings.Split(sqlText, "\n")
	var builder strings.Builder
	var statements []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		builder.WriteString(line)
		if strings.HasSuffix(line, ";") {
			stmt := strings.TrimSpace(strings.TrimSuffix(builder.String(), ";"))
			statements = append(statements, stmt)
			builder.Reset()
		} else {
			builder.WriteString(" ")
		}
	}
	if builder.Len() > 0 {
		statements = append(statements, strings.TrimSpace(builder.String()))
	}
	return statements
}
