package crudsvc

import (
	"strconv"
	"strings"
	"time"

	"github.com/goliatone/go-crud"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

func queryUUID(ctx crud.Context, key string) uuid.UUID {
	raw := strings.TrimSpace(ctx.Query(key))
	if raw == "" {
		return uuid.Nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil
	}
	return id
}

func queryUUIDSlice(ctx crud.Context, key string) []uuid.UUID {
	raw := strings.TrimSpace(ctx.Query(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]uuid.UUID, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if id, err := uuid.Parse(part); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func queryStringSlice(ctx crud.Context, key string) []string {
	raw := strings.TrimSpace(ctx.Query(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func queryBool(ctx crud.Context, key string) (bool, bool) {
	raw := strings.TrimSpace(ctx.Query(key))
	if raw == "" {
		return false, false
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, false
	}
	return parsed, true
}

func queryInt(ctx crud.Context, key string, def int) int {
	if value := ctx.Query(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return def
}

func queryTime(ctx crud.Context, key string) *time.Time {
	raw := strings.TrimSpace(ctx.Query(key))
	if raw == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil
	}
	return &parsed
}

func parseLifecycleStates(ctx crud.Context, key string) []types.LifecycleState {
	values := queryStringSlice(ctx, key)
	if len(values) == 0 {
		return nil
	}
	states := make([]types.LifecycleState, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(value)
		states = append(states, types.LifecycleState(value))
	}
	return states
}

func parsePreferenceLevel(value string) types.PreferenceLevel {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(types.PreferenceLevelSystem):
		return types.PreferenceLevelSystem
	case string(types.PreferenceLevelTenant):
		return types.PreferenceLevelTenant
	case string(types.PreferenceLevelOrg):
		return types.PreferenceLevelOrg
	case string(types.PreferenceLevelUser):
		return types.PreferenceLevelUser
	default:
		return ""
	}
}
