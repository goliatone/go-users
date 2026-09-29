package goauth

import (
	"context"
	"fmt"
	"strings"
	"time"

	auth "github.com/goliatone/go-auth"
	"github.com/goliatone/go-users/activity"
	userstypes "github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

// ActivitySinkConfig binds authentication events to a trusted application scope.
// Identifier retention is disabled unless explicitly enabled by the host.
type ActivitySinkConfig struct {
	Sink                        userstypes.ActivitySink
	Scope                       userstypes.ScopeFilter
	AnonymousActorID            uuid.UUID
	RetainFailedLoginIdentifier bool
}

// ActivitySink adapts go-auth observations without identity lookups or copying
// arbitrary event metadata. The host owns read authorization and retention.
type ActivitySink struct{ config ActivitySinkConfig }

var _ auth.ActivitySink = (*ActivitySink)(nil)

func NewActivitySink(config ActivitySinkConfig) (*ActivitySink, error) {
	if config.Sink == nil || config.Scope.TenantID == uuid.Nil || config.Scope.OrgID == uuid.Nil || config.AnonymousActorID == uuid.Nil {
		return nil, fmt.Errorf("goauth: Activity sink, exact scope and anonymous actor are required")
	}
	return &ActivitySink{config: config}, nil
}

func (sink *ActivitySink) Record(ctx context.Context, event auth.ActivityEvent) error {
	if sink == nil || sink.config.Sink == nil || sink.config.Scope.TenantID == uuid.Nil || sink.config.Scope.OrgID == uuid.Nil || sink.config.AnonymousActorID == uuid.Nil {
		return fmt.Errorf("authentication Activity sink is not ready")
	}
	verb := strings.TrimSpace(string(event.EventType))
	if verb == "" {
		return fmt.Errorf("authentication Activity event type is required")
	}

	targetID := parseActivityUUID(event.UserID)
	actorID := parseActivityUUID(event.Actor.ID)
	if actorID == uuid.Nil && targetID != uuid.Nil {
		actorID = targetID
	}
	if actorID == uuid.Nil {
		actorID = sink.config.AnonymousActorID
	}
	objectType := "authentication"
	objectID := ""
	if targetID != uuid.Nil {
		objectType = "user"
		objectID = targetID.String()
	}

	data := map[string]any{
		"action":      verb,
		"outcome":     authenticationOutcome(event.EventType),
		"target_id":   objectID,
		"target_type": objectType,
	}
	if sink.config.RetainFailedLoginIdentifier && event.EventType == auth.ActivityEventLoginFailure {
		if identifier := activity.SanitizeAttemptedIdentifier(event.Metadata["identifier"]); identifier != "" {
			data[activity.DataKeyAttemptedIdentifier] = identifier
		}
	}
	if event.FromStatus != "" {
		data["previous_status"] = string(event.FromStatus)
	}
	if event.ToStatus != "" {
		data["current_status"] = string(event.ToStatus)
	}
	occurredAt := event.OccurredAt.UTC()
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	return sink.config.Sink.Log(ctx, userstypes.ActivityRecord{
		ID:         uuid.New(),
		UserID:     targetID,
		ActorID:    actorID,
		TenantID:   sink.config.Scope.TenantID,
		OrgID:      sink.config.Scope.OrgID,
		Verb:       verb,
		ObjectType: objectType,
		ObjectID:   objectID,
		Channel:    activity.AuthenticationChannel,
		Data:       data,
		OccurredAt: occurredAt,
	})
}

func parseActivityUUID(value string) uuid.UUID {
	id, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return uuid.Nil
	}
	return id
}

func authenticationOutcome(eventType auth.ActivityEventType) string {
	value := strings.ToLower(strings.TrimSpace(string(eventType)))
	switch {
	case strings.HasSuffix(value, ".success"), value == string(auth.ActivityEventPasswordResetSuccess):
		return "succeeded"
	case strings.HasSuffix(value, ".failure"):
		return "failed"
	case strings.HasSuffix(value, ".denied"), strings.HasSuffix(value, ".deny"):
		return "denied"
	case strings.HasSuffix(value, ".revoked"):
		return "revoked"
	case strings.HasSuffix(value, ".uncertain"):
		return "uncertain"
	default:
		return "recorded"
	}
}
