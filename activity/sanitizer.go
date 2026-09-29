package activity

import (
	"maps"
	"sync"

	"github.com/goliatone/go-masker"
	"github.com/goliatone/go-users/pkg/types"
)

// SanitizerConfig controls the masker used for activity sanitization.
type SanitizerConfig struct {
	Masker *masker.Masker
}

var defaultActivityMasker = sync.OnceValue(func() *masker.Masker {
	mask, err := masker.NewSecure(
		masker.WithMaskField("actor_email", "hash"),
		masker.WithMaskField("session_id", masker.MaskTypeRedact),
		masker.WithMaskField("sessionId", masker.MaskTypeRedact),
	)
	if err != nil {
		// SanitizeRecord fails closed when no masker is available.
		return nil
	}
	return mask
})

// DefaultMasker returns an independent, frozen Activity masker. Credentials and
// session identifiers are fully redacted regardless of length; host changes to
// the package-global compatibility masker cannot weaken this boundary.
func DefaultMasker() *masker.Masker {
	return defaultActivityMasker()
}

// SanitizeRecord masks sensitive values in the activity record data payload.
func SanitizeRecord(mask *masker.Masker, record types.ActivityRecord) types.ActivityRecord {
	if len(record.Data) == 0 {
		return record
	}
	if mask == nil {
		mask = DefaultMasker()
	}
	if mask == nil {
		record.Data = map[string]any{}
		return record
	}

	cloned := cloneStringMap(record.Data)
	masked, err := mask.Mask(cloned)
	if err != nil {
		record.Data = map[string]any{}
		return record
	}

	switch masked := masked.(type) {
	case map[string]any:
		record.Data = masked
	default:
		record.Data = map[string]any{}
	}
	return record
}

// SanitizeRecords masks sensitive values for every record in the slice.
func SanitizeRecords(mask *masker.Masker, records []types.ActivityRecord) []types.ActivityRecord {
	if len(records) == 0 {
		return records
	}
	out := make([]types.ActivityRecord, 0, len(records))
	for _, record := range records {
		out = append(out, SanitizeRecord(mask, record))
	}
	return out
}

func cloneStringMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	dst := make(map[string]any, len(src))
	maps.Copy(dst, src)
	return dst
}
