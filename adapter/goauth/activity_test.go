package goauth

import (
	"context"
	"errors"
	"testing"
	"time"

	auth "github.com/goliatone/go-auth"
	"github.com/goliatone/go-users/activity"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type auditCaptureSink struct {
	records []types.ActivityRecord
	err     error
}

func (s *auditCaptureSink) Log(_ context.Context, r types.ActivityRecord) error {
	s.records = append(s.records, r)
	return s.err
}

func TestActivitySinkMappingAndOptIn(t *testing.T) {
	target, actor, anonymous := uuid.New(), uuid.New(), uuid.New()
	scope := types.ScopeFilter{TenantID: uuid.New(), OrgID: uuid.New()}
	occurred := time.Date(2026, 9, 28, 12, 0, 0, 0, time.FixedZone("test", 3600))
	for _, retain := range []bool{false, true} {
		for _, tc := range []struct {
			name        string
			event       auth.ActivityEventType
			user, actor string
			wantActor   uuid.UUID
		}{
			{"unknown", auth.ActivityEventLoginFailure, "", "", anonymous},
			{"malformed ids", auth.ActivityEventLoginFailure, "invalid", "invalid", anonymous},
			{"known target", auth.ActivityEventLoginFailure, target.String(), "", target},
			{"separate actor", auth.ActivityEventLoginFailure, target.String(), actor.String(), actor},
			{"success", auth.ActivityEventLoginSuccess, target.String(), actor.String(), actor},
			{"recovery", auth.ActivityEventPasswordResetSuccess, target.String(), actor.String(), actor},
		} {
			t.Run(tc.name+map[bool]string{true: "/retain", false: "/drop"}[retain], func(t *testing.T) {
				destination := &auditCaptureSink{}
				sink, err := NewActivitySink(ActivitySinkConfig{Sink: destination, Scope: scope, AnonymousActorID: anonymous, RetainFailedLoginIdentifier: retain})
				require.NoError(t, err)
				event := auth.ActivityEvent{EventType: tc.event, UserID: tc.user, Actor: auth.ActorRef{ID: tc.actor}, OccurredAt: occurred, Metadata: map[string]any{"identifier": " Mixed.User\n", "error": "raw provider error", "password": "secret", "tenant_id": uuid.New().String()}}
				require.NoError(t, sink.Record(context.Background(), event))
				require.Len(t, destination.records, 1)
				got := destination.records[0]
				require.Equal(t, tc.wantActor, got.ActorID)
				require.Equal(t, scope.TenantID, got.TenantID)
				require.Equal(t, scope.OrgID, got.OrgID)
				require.Equal(t, string(tc.event), got.Verb)
				require.Equal(t, activity.AuthenticationChannel, got.Channel)
				require.True(t, occurred.Equal(got.OccurredAt))
				require.Equal(t, time.UTC, got.OccurredAt.Location())
				require.NotEqual(t, uuid.Nil, got.ID)
				require.Empty(t, got.IP)
				for _, key := range []string{"identifier", "password", "error", "tenant_id"} {
					require.NotContains(t, got.Data, key)
				}
				if retain && tc.event == auth.ActivityEventLoginFailure {
					require.Equal(t, "Mixed.User", got.Data[activity.DataKeyAttemptedIdentifier])
				} else {
					require.NotContains(t, got.Data, activity.DataKeyAttemptedIdentifier)
				}
				if tc.user == target.String() {
					require.Equal(t, target, got.UserID)
					require.Equal(t, "user", got.ObjectType)
					require.Equal(t, target.String(), got.ObjectID)
				} else {
					require.Equal(t, uuid.Nil, got.UserID)
					require.Equal(t, "authentication", got.ObjectType)
					require.Empty(t, got.ObjectID)
				}
				require.Equal(t, " Mixed.User\n", event.Metadata["identifier"])
			})
		}
	}
}

func TestActivitySinkValidationAndErrors(t *testing.T) {
	valid := ActivitySinkConfig{Sink: &auditCaptureSink{}, Scope: types.ScopeFilter{TenantID: uuid.New(), OrgID: uuid.New()}, AnonymousActorID: uuid.New()}
	for _, modify := range []func(*ActivitySinkConfig){func(c *ActivitySinkConfig) { c.Sink = nil }, func(c *ActivitySinkConfig) { c.Scope.TenantID = uuid.Nil }, func(c *ActivitySinkConfig) { c.Scope.OrgID = uuid.Nil }, func(c *ActivitySinkConfig) { c.AnonymousActorID = uuid.Nil }} {
		c := valid
		modify(&c)
		_, err := NewActivitySink(c)
		require.Error(t, err)
	}
	var nilSink *ActivitySink
	require.Error(t, nilSink.Record(context.Background(), auth.ActivityEvent{}))
	sink, err := NewActivitySink(valid)
	require.NoError(t, err)
	require.Error(t, sink.Record(context.Background(), auth.ActivityEvent{}))
	destination := valid.Sink.(*auditCaptureSink)
	destination.err = errors.New("storage unavailable")
	require.ErrorIs(t, sink.Record(context.Background(), auth.ActivityEvent{EventType: auth.ActivityEventLoginFailure}), destination.err)
	require.False(t, destination.records[0].OccurredAt.IsZero())
}
