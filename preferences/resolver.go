package preferences

import (
	"context"
	"fmt"
	"maps"
	"strings"

	i18n "github.com/goliatone/go-i18n"
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
	UserID          uuid.UUID
	Scope           types.ScopeFilter
	Levels          []types.PreferenceLevel
	Keys            []string
	Base            map[string]any
	OutputMode      types.PreferenceOutputMode
	IncludeVersions bool
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
	outputMode, err := normalizeOutputMode(input.OutputMode)
	if err != nil {
		return types.PreferenceSnapshot{}, err
	}
	order := filterLevels(resolutionOrder(input.Levels), input)
	layerData, err := r.buildResolutionLayers(ctx, input, order)
	if err != nil {
		return types.PreferenceSnapshot{}, err
	}
	stack, err := opts.NewStack(layerData.layers...)
	if err != nil {
		return types.PreferenceSnapshot{}, err
	}
	merged, err := stack.Merge()
	if err != nil {
		return types.PreferenceSnapshot{}, err
	}
	effective := transformEffective(cloneMap(merged.Value), outputMode)
	traces, err := buildTraces(order, input.Keys, layerData.keyRecords, layerData.keyVersions, layerData.scopeMeta, layerData.values, outputMode)
	if err != nil {
		return types.PreferenceSnapshot{}, err
	}
	var effectiveVersions map[string]int
	if input.IncludeVersions {
		effectiveVersions = buildEffectiveVersions(traces)
	}
	return types.PreferenceSnapshot{
		Effective:         effective,
		EffectiveVersions: effectiveVersions,
		Traces:            traces,
	}, nil
}

type resolutionLayerData struct {
	layers      []opts.Layer[map[string]any]
	scopeMeta   map[types.PreferenceLevel]scopeValues
	keyRecords  map[types.PreferenceLevel]map[string]uuid.UUID
	keyVersions map[types.PreferenceLevel]map[string]int
	values      map[types.PreferenceLevel]map[string]any
}

func (r *Resolver) buildResolutionLayers(ctx context.Context, input ResolveInput, order []types.PreferenceLevel) (resolutionLayerData, error) {
	data := resolutionLayerData{
		layers:      make([]opts.Layer[map[string]any], 0, len(order)),
		scopeMeta:   make(map[types.PreferenceLevel]scopeValues, len(order)),
		keyRecords:  make(map[types.PreferenceLevel]map[string]uuid.UUID, len(order)),
		keyVersions: make(map[types.PreferenceLevel]map[string]int, len(order)),
		values:      make(map[types.PreferenceLevel]map[string]any, len(order)),
	}
	for _, level := range order {
		if err := r.appendResolutionLayer(ctx, input, level, &data); err != nil {
			return resolutionLayerData{}, err
		}
	}
	return data, nil
}

func (r *Resolver) appendResolutionLayer(ctx context.Context, input ResolveInput, level types.PreferenceLevel, data *resolutionLayerData) error {
	recs, err := r.repo.ListPreferences(ctx, types.PreferenceFilter{
		UserID: input.UserID,
		Scope:  input.Scope,
		Level:  level,
		Keys:   input.Keys,
	})
	if err != nil {
		return err
	}
	snapshot, idMap, versionMap := snapshotFromRecords(recs)
	payload := resolutionPayload(level, snapshot, r.defaults, input.Base)
	meta, err := scopeIDs(level, input.UserID, input.Scope)
	if err != nil {
		return err
	}
	data.keyRecords[level] = idMap
	data.keyVersions[level] = versionMap
	data.scopeMeta[level] = meta
	data.values[level] = cloneMap(payload)
	data.layers = append(data.layers, newResolutionLayer(level, payload, meta))
	return nil
}

func resolutionPayload(level types.PreferenceLevel, snapshot, defaults, base map[string]any) map[string]any {
	payload := snapshot
	if level == types.PreferenceLevelSystem {
		payload = mergeMaps(defaults, base)
		maps.Copy(payload, snapshot)
	}
	if payload == nil {
		payload = make(map[string]any)
	}
	return normalizeLocalePreferencePayload(payload)
}

func newResolutionLayer(level types.PreferenceLevel, payload map[string]any, meta scopeValues) opts.Layer[map[string]any] {
	scope := opts.NewScope(scopeName(level), scopePriority(level),
		opts.WithScopeLabel(scopeLabel(level)),
		opts.WithScopeMetadata(scopeMetadata(meta)))
	return opts.NewLayer(scope, payload, opts.WithSnapshotID[map[string]any](scope.Name))
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

func snapshotFromRecords(records []types.PreferenceRecord) (map[string]any, map[string]uuid.UUID, map[string]int) {
	if len(records) == 0 {
		return nil, nil, nil
	}
	values := make(map[string]any, len(records))
	index := make(map[string]uuid.UUID, len(records))
	versions := make(map[string]int, len(records))
	for _, rec := range records {
		values[rec.Key] = cloneMap(rec.Value)
		index[rec.Key] = rec.ID
		versions[rec.Key] = rec.Version
	}
	return values, index, versions
}

func mergeMaps(base map[string]any, overlay map[string]any) map[string]any {
	out := make(map[string]any)
	if len(base) > 0 {
		maps.Copy(out, base)
	}
	if len(overlay) > 0 {
		if len(out) == 0 {
			out = make(map[string]any, len(overlay))
		}
		maps.Copy(out, overlay)
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

func buildTraces(order []types.PreferenceLevel, keys []string, keyRecords map[types.PreferenceLevel]map[string]uuid.UUID, keyVersions map[types.PreferenceLevel]map[string]int, scopes map[types.PreferenceLevel]scopeValues, values map[types.PreferenceLevel]map[string]any, outputMode types.PreferenceOutputMode) ([]types.PreferenceTrace, error) {
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
				Version:    lookupVersion(level, keyVersions, key),
			}
			if levelValues := values[level]; levelValues != nil {
				if v, ok := levelValues[key]; ok {
					layer.Value = transformValue(v, outputMode)
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

func lookupVersion(level types.PreferenceLevel, keyVersions map[types.PreferenceLevel]map[string]int, key string) int {
	if versions := keyVersions[level]; versions != nil {
		return versions[key]
	}
	return 0
}

func normalizeOutputMode(mode types.PreferenceOutputMode) (types.PreferenceOutputMode, error) {
	if mode == "" {
		return types.PreferenceOutputEnvelope, nil
	}
	switch mode {
	case types.PreferenceOutputEnvelope, types.PreferenceOutputRawValue:
		return mode, nil
	default:
		return "", types.ErrUnsupportedPreferenceOutputMode
	}
}

func transformEffective(input map[string]any, mode types.PreferenceOutputMode) map[string]any {
	if len(input) == 0 {
		return nil
	}
	if mode == types.PreferenceOutputEnvelope {
		return input
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = transformValue(value, mode)
	}
	return output
}

func transformValue(value any, mode types.PreferenceOutputMode) any {
	if mode != types.PreferenceOutputRawValue {
		return value
	}
	typed, ok := value.(map[string]any)
	if !ok || len(typed) != 1 {
		return value
	}
	raw, ok := typed["value"]
	if !ok {
		return value
	}
	return raw
}

func normalizeLocalePreferencePayload(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return payload
	}
	out := cloneMap(payload)
	for key, value := range out {
		if !strings.EqualFold(strings.TrimSpace(key), "locale") {
			continue
		}
		out[key] = normalizeLocalePreferenceValue(value)
	}
	return out
}

func normalizeLocalePreferenceValue(value any) any {
	switch typed := value.(type) {
	case string:
		return i18n.NormalizeLocale(typed)
	case map[string]any:
		out := cloneMap(typed)
		for key, nested := range out {
			out[key] = normalizeLocaleFieldValue(key, nested)
		}
		return out
	default:
		return value
	}
}

func normalizeLocaleFieldValue(field string, value any) any {
	switch typed := value.(type) {
	case string:
		if isLocaleField(field) {
			return i18n.NormalizeLocale(typed)
		}
		return value
	case map[string]any:
		return normalizeLocalePreferenceValue(typed)
	default:
		return value
	}
}

func isLocaleField(field string) bool {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "value", "locale", "language", "default_locale", "preferred_locale", "source_locale", "target_locale", "fallback_locale":
		return true
	default:
		return false
	}
}

func buildEffectiveVersions(traces []types.PreferenceTrace) map[string]int {
	if len(traces) == 0 {
		return nil
	}
	versions := make(map[string]int, len(traces))
	for _, trace := range traces {
		for _, layer := range trace.Layers {
			if !layer.Found {
				continue
			}
			versions[trace.Key] = layer.Version
		}
	}
	if len(versions) == 0 {
		return nil
	}
	return versions
}
