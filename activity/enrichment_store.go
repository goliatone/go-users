package activity

import (
	"context"
	"time"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

// ActivityEnrichmentStore updates activity metadata for backfill jobs.
type ActivityEnrichmentStore interface {
	UpdateActivityData(ctx context.Context, id uuid.UUID, data map[string]any) error
}

// ActivityEnrichmentQuery exposes helper queries for missing/stale enrichment.
type ActivityEnrichmentQuery interface {
	ListActivityForEnrichment(ctx context.Context, filter ActivityEnrichmentFilter) (ActivityEnrichmentPage, error)
}

// ActivityEnrichmentFilter narrows enrichment backfill selection.
type ActivityEnrichmentFilter struct {
	Scope          types.ScopeFilter
	MissingKeys    []string
	EnrichedBefore *time.Time
	Cursor         *ActivityCursor
	Limit          int
}

// ActivityEnrichmentPage returns selected activity records and the next cursor.
type ActivityEnrichmentPage struct {
	Records    []types.ActivityRecord
	NextCursor *ActivityCursor
}
