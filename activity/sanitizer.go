package activity

import (
	"sync"

	"github.com/goliatone/go-masker"
	"github.com/goliatone/go-users/pkg/types"
)

// SanitizerConfig controls the masker used for activity sanitization.
type SanitizerConfig struct {
	Masker *masker.Masker
}

var defaultMaskerOnce sync.Once

// DefaultMasker returns a configured masker instance with the default denylist.
func DefaultMasker() *masker.Masker {
	defaultMaskerOnce.Do(func() {
		if masker.Default == nil {
			return
		}
		registerDefaultMaskFields(masker.Default)
	})
	return masker.Default
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

func registerDefaultMaskFields(mask *masker.Masker) {
	if mask == nil {
		return
	}
	mask.RegisterMaskField("Secret", "filled4")
	mask.RegisterMaskField("secret", "filled4")
}

func cloneStringMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}
