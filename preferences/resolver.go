package preferences

import (
	"context"
	"fmt"
	"strings"

	opts "github.com/goliatone/go-options"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

// ResolverConfig wires dependencies for the preference resolver.
type ResolverConfig struct {
	Repository types.PreferenceRepository
	Defaults   map[string]any
}

// Resolver merges scoped preference layers via go-options.
type Resolver struct {
	repo     types.PreferenceRepository
	defaults map[string]any
}

// ResolveInput controls which scopes participate in the resolution process.
type ResolveInput struct {
	UserID uuid.UUID
	Scope  types.ScopeFilter
	Levels []types.PreferenceLevel
	Keys   []string
	Base   map[string]any
}

// NewResolver constructs a preference resolver.
func NewResolver(cfg ResolverConfig) (*Resolver, error) {
	if cfg.Repository == nil {
		return nil, fmt.Errorf("preferences: repository required")
	}
	return &Resolver{
		repo:     cfg.Repository,
		defaults: cloneMap(cfg.Defaults),
	}, nil
}

// Resolve builds the effective preference snapshot for the supplied scope chain.
func (r *Resolver) Resolve(ctx context.Context, input ResolveInput) (types.PreferenceSnapshot, error) {
	order := filterLevels(resolutionOrder(input.Levels), input)
	layers := make([]opts.Layer[map[string]any], 0, len(order))
	layerScopeMeta := make(map[types.PreferenceLevel]scopeValues, len(order))
	keyRecords := make(map[types.PreferenceLevel]map[string]uuid.UUID, len(order))
	layerValues := make(map[types.PreferenceLevel]map[string]any, len(order))

	for _, level := range order {
		filter := types.PreferenceFilter{
			UserID: input.UserID,
			Scope:  input.Scope,
			Level:  level,
			Keys:   input.Keys,
		}
		recs, err := r.repo.ListPreferences(ctx, filter)
		if err != nil {
			return types.PreferenceSnapshot{}, err
		}
		snapshot, idMap := snapshotFromRecords(recs)
		keyRecords[level] = idMap

		var payload map[string]any
		switch level {
		case types.PreferenceLevelSystem:
			payload = mergeMaps(r.defaults, input.Base)
			for k, v := range snapshot {
				payload[k] = v
			}
		default:
			payload = snapshot
		}
		if payload == nil {
			payload = make(map[string]any)
		}
		meta, err := scopeIDs(level, input.UserID, input.Scope)
		if err != nil {
			return types.PreferenceSnapshot{}, err
		}
		layerScopeMeta[level] = meta
		layerValues[level] = cloneMap(payload)

		scope := opts.NewScope(scopeName(level), scopePriority(level),
			opts.WithScopeLabel(scopeLabel(level)),
			opts.WithScopeMetadata(scopeMetadata(meta)))
		layer := opts.NewLayer(scope, payload, opts.WithSnapshotID[map[string]any](scope.Name))
		layers = append(layers, layer)
	}

	stack, err := opts.NewStack(layers...)
	if err != nil {
		return types.PreferenceSnapshot{}, err
	}
	merged, err := stack.Merge()
	if err != nil {
		return types.PreferenceSnapshot{}, err
	}
	effective := cloneMap(merged.Value)
	traces, err := buildTraces(order, input.Keys, keyRecords, layerScopeMeta, layerValues)
	if err != nil {
		return types.PreferenceSnapshot{}, err
	}
	return types.PreferenceSnapshot{
		Effective: effective,
		Traces:    traces,
	}, nil
}

func resolutionOrder(levels []types.PreferenceLevel) []types.PreferenceLevel {
	if len(levels) > 0 {
		return append([]types.PreferenceLevel(nil), levels...)
	}
	return []types.PreferenceLevel{
		types.PreferenceLevelSystem,
		types.PreferenceLevelTenant,
		types.PreferenceLevelOrg,
		types.PreferenceLevelUser,
	}
}

func filterLevels(levels []types.PreferenceLevel, input ResolveInput) []types.PreferenceLevel {
	filtered := make([]types.PreferenceLevel, 0, len(levels))
	for _, level := range levels {
		switch level {
		case types.PreferenceLevelUser:
			if input.UserID == uuid.Nil {
				continue
			}
		case types.PreferenceLevelOrg:
			if input.Scope.OrgID == uuid.Nil {
				continue
			}
		case types.PreferenceLevelTenant:
			if input.Scope.TenantID == uuid.Nil {
				continue
			}
		}
		filtered = append(filtered, level)
	}
	if len(filtered) == 0 {
		return []types.PreferenceLevel{types.PreferenceLevelSystem}
	}
	return filtered
}

func snapshotFromRecords(records []types.PreferenceRecord) (map[string]any, map[string]uuid.UUID) {
	if len(records) == 0 {
		return nil, nil
	}
	values := make(map[string]any, len(records))
	index := make(map[string]uuid.UUID, len(records))
	for _, rec := range records {
		values[rec.Key] = cloneMap(rec.Value)
		index[rec.Key] = rec.ID
	}
	return values, index
}

func mergeMaps(base map[string]any, overlay map[string]any) map[string]any {
	out := make(map[string]any)
	if len(base) > 0 {
		for k, v := range base {
			out[k] = v
		}
	}
	if len(overlay) > 0 {
		if len(out) == 0 {
			out = make(map[string]any, len(overlay))
		}
		for k, v := range overlay {
			out[k] = v
		}
	}
	return out
}

func scopeName(level types.PreferenceLevel) string {
	switch level {
	case types.PreferenceLevelTenant:
		return "tenant"
	case types.PreferenceLevelOrg:
		return "org"
	case types.PreferenceLevelUser:
		return "user"
	default:
		return "system"
	}
}

func scopeLabel(level types.PreferenceLevel) string {
	switch level {
	case types.PreferenceLevelTenant:
		return "Tenant"
	case types.PreferenceLevelOrg:
		return "Organization"
	case types.PreferenceLevelUser:
		return "User"
	default:
		return "System Defaults"
	}
}

func scopePriority(level types.PreferenceLevel) int {
	switch level {
	case types.PreferenceLevelUser:
		return opts.ScopePriorityUser
	case types.PreferenceLevelOrg:
		return opts.ScopePriorityOrg
	case types.PreferenceLevelTenant:
		return opts.ScopePriorityTenant
	default:
		return opts.ScopePrioritySystem
	}
}

func scopeMetadata(values scopeValues) map[string]any {
	meta := map[string]any{
		"user_id":   values.user.String(),
		"tenant_id": values.tenant.String(),
		"org_id":    values.org.String(),
	}
	return meta
}

func buildTraces(order []types.PreferenceLevel, keys []string, keyRecords map[types.PreferenceLevel]map[string]uuid.UUID, scopes map[types.PreferenceLevel]scopeValues, values map[types.PreferenceLevel]map[string]any) ([]types.PreferenceTrace, error) {
	keySet := make(map[string]struct{})
	for _, key := range keys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		keySet[key] = struct{}{}
	}
	if len(keySet) == 0 {
		for _, levelValues := range values {
			for key := range levelValues {
				keySet[key] = struct{}{}
			}
		}
	}
	traces := make([]types.PreferenceTrace, 0, len(keySet))
	for key := range keySet {
		layers := make([]types.PreferenceTraceLayer, 0, len(order))
		for _, level := range order {
			scopeVals := scopes[level]
			layer := types.PreferenceTraceLayer{
				Level:      level,
				UserID:     scopeVals.user,
				Scope:      types.ScopeFilter{TenantID: scopeVals.tenant, OrgID: scopeVals.org},
				SnapshotID: lookupSnapshotID(level, keyRecords, key),
			}
			if levelValues := values[level]; levelValues != nil {
				if v, ok := levelValues[key]; ok {
					layer.Value = v
					layer.Found = true
				}
			}
			layers = append(layers, layer)
		}
		traces = append(traces, types.PreferenceTrace{
			Key:    key,
			Layers: layers,
		})
	}
	return traces, nil
}

func lookupSnapshotID(level types.PreferenceLevel, keyRecords map[types.PreferenceLevel]map[string]uuid.UUID, key string) string {
	if records := keyRecords[level]; records != nil {
		if id, ok := records[key]; ok {
			return id.String()
		}
	}
	return ""
}

func toLevel(name string) types.PreferenceLevel {
	switch name {
	case "user":
		return types.PreferenceLevelUser
	case "org":
		return types.PreferenceLevelOrg
	case "tenant":
		return types.PreferenceLevelTenant
	default:
		return types.PreferenceLevelSystem
	}
}
