package activity

import (
	"context"

	"github.com/google/uuid"
)

// Enrichment metadata keys. These are flat JSONB keys and should remain stable.
const (
	DataKeyActorDisplay    = "actor_display"
	DataKeyActorEmail      = "actor_email"
	DataKeyActorID         = "actor_id"
	DataKeyActorType       = "actor_type"
	DataKeyObjectDisplay   = "object_display"
	DataKeyObjectType      = "object_type"
	DataKeyObjectID        = "object_id"
	DataKeyObjectDeleted   = "object_deleted"
	DataKeySessionID       = "session_id"
	DataKeyEnrichedAt      = "enriched_at"
	DataKeyEnricherVersion = "enricher_version"
)

// DefaultEnricherVersion is the built-in fallback version string when enrichment is enabled.
const DefaultEnricherVersion = "v1"

// ResolveContext provides request-scoped data used by resolvers.
type ResolveContext struct {
	TenantID uuid.UUID
	ActorID  uuid.UUID
	Verb     string
	Source   string
	Metadata map[string]any
}

// ActorInfo defines enrichment details for an actor.
type ActorInfo struct {
	ID      uuid.UUID
	Type    string
	Display string
	Email   string
}

// ObjectInfo defines enrichment details for an object.
type ObjectInfo struct {
	ID      string
	Type    string
	Display string
	Deleted bool
}

// ActorResolver resolves actor enrichment details in batch.
type ActorResolver interface {
	ResolveActors(ctx context.Context, ids []uuid.UUID, meta ResolveContext) (map[uuid.UUID]ActorInfo, error)
}

// ObjectResolver resolves object enrichment details in batch.
type ObjectResolver interface {
	ResolveObjects(ctx context.Context, objectType string, ids []string, meta ResolveContext) (map[string]ObjectInfo, error)
}
