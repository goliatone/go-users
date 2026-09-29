package activity

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type policyCaptureSink struct {
	record types.ActivityRecord
	err    error
}

func (s *policyCaptureSink) Log(_ context.Context, r types.ActivityRecord) error {
	s.record = r
	return s.err
}

func TestSanitizeAttemptedIdentifier(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input any
		want  string
	}{
		{"case", "  Mixed.User@example.test  ", "Mixed.User@example.test"},
		{"empty", " \t\n", ""}, {"controls", "ab\x00\n\t\u202ecd\u200b", "abcd"},
		{"invalid", "ab\xffcd", "abcd"}, {"markup", "<img src=x onerror=alert(1)>", "<img src=x onerror=alert(1)>"},
		{"ascii", strings.Repeat("a", 300), strings.Repeat("a", 254)},
		{"unicode", strings.Repeat("界", 300), strings.Repeat("界", 254)},
		{"object", map[string]any{"password": "secret"}, ""}, {"number", 123, ""},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, SanitizeAttemptedIdentifier(tc.input)) })
	}
}

func TestRecordPolicyEventRestrictionAcrossSinkHookAndRead(t *testing.T) {
	for _, retain := range []bool{false, true} {
		for _, event := range []struct {
			verb, channel string
			eligible      bool
		}{
			{LoginFailureVerb, AuthenticationChannel, true}, {"auth.login.success", AuthenticationChannel, false},
			{"password.reset.request", AuthenticationChannel, false}, {LoginFailureVerb, "admin", false},
		} {
			t.Run(event.verb+"/"+event.channel+"/"+map[bool]string{true: "retain", false: "drop"}[retain], func(t *testing.T) {
				policy := NewRecordPolicy(RecordPolicyConfig{RetainFailedLoginIdentifier: retain, DataSanitizer: func(r types.ActivityRecord) map[string]any {
					return map[string]any{"outcome": r.Data["outcome"], DataKeyAttemptedIdentifier: "callback cannot bypass event restriction"}
				}})
				original := types.ActivityRecord{ID: uuid.New(), ActorID: uuid.New(), UserID: uuid.New(), TenantID: uuid.New(), OrgID: uuid.New(), Verb: event.verb, Channel: event.channel, IP: "127.0.0.1", Data: map[string]any{DataKeyAttemptedIdentifier: " user@example.test\n", "password": "secret", "outcome": "failed"}}
				destination := &policyCaptureSink{}
				require.NoError(t, (SanitizingSink{Next: destination, Policy: policy}).Log(context.Background(), original))
				var hooked types.ActivityRecord
				policy.SanitizeHooks(types.Hooks{AfterActivity: func(_ context.Context, r types.ActivityRecord) { hooked = r }}).AfterActivity(context.Background(), original)
				for _, got := range []types.ActivityRecord{destination.record, hooked, policy.Sanitize(original), policy.Sanitize(destination.record)} {
					require.Equal(t, original.ActorID, got.ActorID)
					require.Equal(t, original.UserID, got.UserID)
					require.Equal(t, original.TenantID, got.TenantID)
					require.Equal(t, original.OrgID, got.OrgID)
					require.Equal(t, original.Verb, got.Verb)
					require.Empty(t, got.IP)
					require.NotContains(t, got.Data, "password")
					if retain && event.eligible {
						require.Equal(t, "user@example.test", got.Data[DataKeyAttemptedIdentifier])
					} else {
						require.NotContains(t, got.Data, DataKeyAttemptedIdentifier)
					}
				}
				require.Equal(t, " user@example.test\n", original.Data[DataKeyAttemptedIdentifier])
			})
		}
	}
}

func TestRecordPolicyDefaultsAndDetachedMetadata(t *testing.T) {
	source := types.ActivityRecord{IP: "ip", Data: map[string]any{"nested": map[string]any{"items": []any{map[string]any{"value": "original"}}}, DataKeyAttemptedIdentifier: "user"}, Verb: LoginFailureVerb, Channel: AuthenticationChannel}
	var nilPolicy *RecordPolicy
	for _, policy := range []*RecordPolicy{nilPolicy, NewRecordPolicy(RecordPolicyConfig{})} {
		got := policy.Sanitize(source)
		require.Empty(t, got.Data)
		require.Empty(t, got.IP)
	}
	policy := NewRecordPolicy(RecordPolicyConfig{PreserveIP: true, DataSanitizer: func(r types.ActivityRecord) map[string]any {
		r.Data["nested"].(map[string]any)["items"].([]any)[0].(map[string]any)["value"] = "callback"
		return r.Data
	}})
	got := policy.Sanitize(source)
	require.Equal(t, "ip", got.IP)
	got.Data["nested"].(map[string]any)["items"].([]any)[0].(map[string]any)["value"] = "consumer"
	require.Equal(t, "original", source.Data["nested"].(map[string]any)["items"].([]any)[0].(map[string]any)["value"])
	destination := &policyCaptureSink{err: errors.New("storage unavailable")}
	require.ErrorIs(t, (SanitizingSink{Next: destination, Policy: policy}).Log(context.Background(), source), destination.err)
	require.Error(t, (SanitizingSink{Policy: policy}).Log(context.Background(), source))
	require.Nil(t, policy.SanitizeHooks(types.Hooks{}).AfterActivity)
}

func TestRecordPolicyTransactionalPersistence(t *testing.T) {
	db := newTestActivityDB(t)
	applyActivityDDL(t, db)
	repo, err := NewRepository(RepositoryConfig{DB: db})
	require.NoError(t, err)
	policy := NewRecordPolicy(RecordPolicyConfig{RetainFailedLoginIdentifier: true})
	for _, rollback := range []bool{true, false} {
		tx, beginErr := db.BeginTx(context.Background(), nil)
		require.NoError(t, beginErr)
		record := policy.Sanitize(types.ActivityRecord{ID: uuid.New(), ActorID: uuid.New(), Verb: LoginFailureVerb, Channel: AuthenticationChannel, Data: map[string]any{DataKeyAttemptedIdentifier: " user\n", "password": "secret"}})
		_, err = repo.CreateTx(context.Background(), tx, &LogEntry{ID: record.ID, ActorID: record.ActorID, Verb: record.Verb, Channel: record.Channel, Data: record.Data})
		require.NoError(t, err)
		if rollback {
			require.NoError(t, tx.Rollback())
		} else {
			require.NoError(t, tx.Commit())
		}
	}
	page, err := repo.ListActivity(context.Background(), types.ActivityFilter{Pagination: types.Pagination{Limit: 10}})
	require.NoError(t, err)
	require.Len(t, page.Records, 1)
	require.Equal(t, map[string]any{DataKeyAttemptedIdentifier: "user"}, page.Records[0].Data)
}
