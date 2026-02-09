package command

import (
	"strings"

	"github.com/goliatone/go-users/pkg/types"
)

func normalizePreferenceLevel(level types.PreferenceLevel) (types.PreferenceLevel, error) {
	if level == "" {
		return types.PreferenceLevelUser, nil
	}
	switch level {
	case types.PreferenceLevelSystem, types.PreferenceLevelTenant, types.PreferenceLevelOrg, types.PreferenceLevelUser:
		return level, nil
	default:
		return "", types.ErrUnsupportedPreferenceLevel
	}
}

func normalizePreferenceBulkMode(mode types.PreferenceBulkMode) (types.PreferenceBulkMode, error) {
	if mode == "" {
		return types.PreferenceBulkModeBestEffort, nil
	}
	switch mode {
	case types.PreferenceBulkModeBestEffort, types.PreferenceBulkModeTransactional:
		return mode, nil
	default:
		return "", types.ErrUnsupportedPreferenceBulkMode
	}
}

func coercePreferencePayload(value any) (map[string]any, error) {
	if value == nil {
		return nil, ErrPreferenceValueRequired
	}
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed), nil
	default:
		return map[string]any{"value": typed}, nil
	}
}

func normalizePreferenceKeys(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	result := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
