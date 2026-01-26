package activity

import (
	"time"

	"github.com/goliatone/go-users/pkg/types"
)

// StampEnrichment sets enriched_at and enricher_version on the activity record.
func StampEnrichment(record types.ActivityRecord, now time.Time, version string) types.ActivityRecord {
	out := record
	out.Data = cloneMetadata(record.Data)

	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if version == "" {
		version = DefaultEnricherVersion
	}

	out.Data[DataKeyEnrichedAt] = now.Format(time.RFC3339Nano)
	out.Data[DataKeyEnricherVersion] = version
	return out
}
