package activity

import (
	"strings"

	"github.com/goliatone/go-auth"
	"github.com/goliatone/go-users/pkg/authctx"
	"github.com/goliatone/go-users/pkg/types"
)

// RecordOption mutates the ActivityRecord produced by BuildRecordFromActor.
type RecordOption func(*types.ActivityRecord)

// WithChannel sets the channel/module field used for downstream filtering.
func WithChannel(channel string) RecordOption {
	return func(record *types.ActivityRecord) {
		record.Channel = strings.TrimSpace(channel)
	}
}

// BuildRecordFromActor constructs an ActivityRecord using the actor metadata
// supplied by go-auth middleware plus verb/object details and optional metadata.
// It normalizes actor, tenant, and org identifiers into UUIDs and defensively
// copies metadata to avoid caller mutation.
func BuildRecordFromActor(actor *auth.ActorContext, verb, objectType, objectID string, metadata map[string]any, opts ...RecordOption) (types.ActivityRecord, error) {
	ref, err := authctx.ActorRefFromActorContext(actor)
	if err != nil {
		return types.ActivityRecord{}, err
	}
	scope := authctx.ScopeFromActorContext(actor)

	record := types.ActivityRecord{
		ActorID:    ref.ID,
		Verb:       strings.TrimSpace(verb),
		ObjectType: strings.TrimSpace(objectType),
		ObjectID:   strings.TrimSpace(objectID),
		Channel:    "",
		TenantID:   scope.TenantID,
		OrgID:      scope.OrgID,
		Data:       cloneMetadata(metadata),
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&record)
		}
	}

	return record, nil
}

func cloneMetadata(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
