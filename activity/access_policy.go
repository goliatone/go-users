package activity

import (
	"github.com/goliatone/go-auth"
	"github.com/goliatone/go-masker"
	"github.com/goliatone/go-users/pkg/types"
)

// ActivityAccessPolicy applies role-aware constraints and sanitization to activity feeds.
type ActivityAccessPolicy interface {
	Apply(actor *auth.ActorContext, role string, req types.ActivityFilter) (types.ActivityFilter, error)
	Sanitize(actor *auth.ActorContext, role string, records []types.ActivityRecord) []types.ActivityRecord
}

// ActivityStatsPolicy applies role-aware constraints to activity stats.
type ActivityStatsPolicy interface {
	ApplyStats(actor *auth.ActorContext, role string, req types.ActivityStatsFilter) (types.ActivityStatsFilter, error)
}

// AccessPolicyOption customizes the default activity access policy.
type AccessPolicyOption func(*DefaultAccessPolicy)

// DefaultAccessPolicy applies BuildFilterFromActor and sanitizes records on read.
type DefaultAccessPolicy struct {
	filterOptions     []FilterOption
	masker            *masker.Masker
	metadataExposure  MetadataExposureStrategy
	metadataSanitizer MetadataSanitizer
	redactIP          bool
	statsSelfOnly     bool
}

var _ ActivityAccessPolicy = (*DefaultAccessPolicy)(nil)
var _ ActivityStatsPolicy = (*DefaultAccessPolicy)(nil)

// NewDefaultAccessPolicy returns the default policy implementation.
func NewDefaultAccessPolicy(opts ...AccessPolicyOption) *DefaultAccessPolicy {
	policy := &DefaultAccessPolicy{
		masker:           DefaultMasker(),
		metadataExposure: MetadataExposeNone,
		redactIP:         true,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(policy)
		}
	}
	if policy.masker == nil {
		policy.masker = DefaultMasker()
	}
	return policy
}

// WithPolicyFilterOptions configures filter options applied during policy enforcement.
func WithPolicyFilterOptions(opts ...FilterOption) AccessPolicyOption {
	return func(policy *DefaultAccessPolicy) {
		if policy == nil {
			return
		}
		policy.filterOptions = append(policy.filterOptions, opts...)
	}
}

// WithPolicyMasker overrides the masker used for sanitization.
func WithPolicyMasker(masker *masker.Masker) AccessPolicyOption {
	return func(policy *DefaultAccessPolicy) {
		if policy == nil {
			return
		}
		policy.masker = masker
	}
}

// WithMetadataExposure configures how activity metadata is exposed for support roles.
func WithMetadataExposure(strategy MetadataExposureStrategy) AccessPolicyOption {
	return func(policy *DefaultAccessPolicy) {
		if policy == nil {
			return
		}
		policy.metadataExposure = strategy
	}
}

// WithMetadataSanitizer overrides the metadata sanitizer for sanitized exposure mode.
func WithMetadataSanitizer(sanitizer MetadataSanitizer) AccessPolicyOption {
	return func(policy *DefaultAccessPolicy) {
		if policy == nil {
			return
		}
		policy.metadataSanitizer = sanitizer
	}
}

// WithIPRedaction toggles IP redaction for non-superadmin roles.
func WithIPRedaction(enabled bool) AccessPolicyOption {
	return func(policy *DefaultAccessPolicy) {
		if policy == nil {
			return
		}
		policy.redactIP = enabled
	}
}

// WithPolicyStatsSelfOnly toggles self-only stats for non-admin roles.
func WithPolicyStatsSelfOnly(enabled bool) AccessPolicyOption {
	return func(policy *DefaultAccessPolicy) {
		if policy == nil {
			return
		}
		policy.statsSelfOnly = enabled
	}
}

// Apply enforces role-aware scope/visibility rules on the requested filter.
func (p *DefaultAccessPolicy) Apply(actor *auth.ActorContext, role string, req types.ActivityFilter) (types.ActivityFilter, error) {
	return BuildFilterFromActor(actor, role, req, p.filterOptions...)
}

// ApplyStats enforces role-aware scope/visibility rules on stats filters.
func (p *DefaultAccessPolicy) ApplyStats(actor *auth.ActorContext, role string, req types.ActivityStatsFilter) (types.ActivityStatsFilter, error) {
	filter, err := BuildFilterFromActor(actor, role, types.ActivityFilter{
		Actor: req.Actor,
		Scope: req.Scope,
	}, p.filterOptions...)
	if err != nil {
		return types.ActivityStatsFilter{}, err
	}
	out := req
	out.Actor = filter.Actor
	out.Scope = filter.Scope
	if p.statsSelfOnly {
		out.UserID = filter.UserID
		out.ActorID = filter.ActorID
	}
	out.MachineActivityEnabled = filter.MachineActivityEnabled
	out.MachineActorTypes = cloneStrings(filter.MachineActorTypes)
	out.MachineDataKeys = cloneStrings(filter.MachineDataKeys)
	return out, nil
}

// Sanitize applies masking rules and IP redaction to activity records.
func (p *DefaultAccessPolicy) Sanitize(actor *auth.ActorContext, role string, records []types.ActivityRecord) []types.ActivityRecord {
	if len(records) == 0 {
		return records
	}
	cfg := defaultFilterConfig()
	for _, opt := range p.filterOptions {
		if opt != nil {
			opt(&cfg)
		}
	}
	roleName := resolveRoleName(actor, role)
	isSuperadmin := roleMatches(roleName, cfg.SuperadminRoleAliases)
	isSupport := roleMatches(roleName, []string{types.ActorRoleSupport})

	mask := p.masker
	if mask == nil {
		mask = DefaultMasker()
	}

	out := make([]types.ActivityRecord, 0, len(records))
	for _, record := range records {
		rec := record
		if isSupport {
			switch p.metadataExposure {
			case MetadataExposeAll:
				// keep metadata as-is
			case MetadataExposeSanitized:
				if p.metadataSanitizer != nil {
					rec.Data = p.metadataSanitizer(actor, role, record)
				} else {
					rec = SanitizeRecord(mask, record)
				}
			default:
				rec.Data = nil
			}
		} else {
			rec = SanitizeRecord(mask, record)
		}

		if p.redactIP && !isSuperadmin {
			rec.IP = ""
		}

		out = append(out, rec)
	}
	return out
}

func resolveRoleName(actor *auth.ActorContext, role string) string {
	roleName := normalizeIdentifier(role)
	if roleName != "" {
		return roleName
	}
	if actor == nil {
		return ""
	}
	roleName = normalizeIdentifier(actor.Role)
	if roleName != "" {
		return roleName
	}
	return normalizeIdentifier(actor.Subject)
}

func filterMachineRecords(records []types.ActivityRecord, cfg FilterConfig) []types.ActivityRecord {
	if len(records) == 0 {
		return records
	}
	out := make([]types.ActivityRecord, 0, len(records))
	for _, record := range records {
		if isMachineRecord(record, cfg) {
			continue
		}
		out = append(out, record)
	}
	return out
}

func isMachineRecord(record types.ActivityRecord, cfg FilterConfig) bool {
	if len(cfg.MachineActorTypes) > 0 {
		if actorType := actorTypeFromData(record.Data); actorType != "" {
			if containsString(cfg.MachineActorTypes, actorType) {
				return true
			}
		}
	}
	if len(cfg.MachineDataKeys) == 0 || len(record.Data) == 0 {
		return false
	}
	for key, value := range record.Data {
		if !containsString(cfg.MachineDataKeys, normalizeIdentifier(key)) {
			continue
		}
		if isTruthy(value) {
			return true
		}
	}
	return false
}

func actorTypeFromData(data map[string]any) string {
	if len(data) == 0 {
		return ""
	}
	if value, ok := data["actor_type"]; ok {
		return normalizeIdentifier(stringValue(value))
	}
	if value, ok := data["actorType"]; ok {
		return normalizeIdentifier(stringValue(value))
	}
	if actor, ok := data["actor"].(map[string]any); ok {
		if value, ok := actor["type"]; ok {
			return normalizeIdentifier(stringValue(value))
		}
	}
	return ""
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func isTruthy(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return normalizeIdentifier(v) == "true"
	default:
		return false
	}
}
