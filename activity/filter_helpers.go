package activity

import (
	"errors"
	"strings"

	"github.com/goliatone/go-auth"
	"github.com/goliatone/go-users/pkg/authctx"
	"github.com/goliatone/go-users/pkg/types"
)

// FilterConfig controls how BuildFilterFromActor applies role and channel rules.
type FilterConfig struct {
	ChannelAllowlist []string
	ChannelDenylist  []string

	// Machine activity settings are reserved for policy layers that can inspect
	// records (actor type/data); BuildFilterFromActor does not use them directly.
	MachineActivityEnabled bool
	MachineActorTypes      []string
	MachineDataKeys        []string

	SuperadminScope bool

	AdminRoleAliases      []string
	SuperadminRoleAliases []string
}

// FilterOption mutates the filter configuration.
type FilterOption func(*FilterConfig)

// DefaultAdminRoleAliases returns the default admin role aliases.
func DefaultAdminRoleAliases() []string {
	return cloneStrings(defaultAdminRoleAliases)
}

// DefaultSuperadminRoleAliases returns the default superadmin role aliases.
func DefaultSuperadminRoleAliases() []string {
	return cloneStrings(defaultSuperadminRoleAliases)
}

// DefaultMachineActorTypes returns the default machine actor type identifiers.
func DefaultMachineActorTypes() []string {
	return cloneStrings(defaultMachineActorTypes)
}

// DefaultMachineDataKeys returns the default machine data keys.
func DefaultMachineDataKeys() []string {
	return cloneStrings(defaultMachineDataKeys)
}

// WithChannelAllowlist restricts results to the provided channels.
func WithChannelAllowlist(channels ...string) FilterOption {
	return func(cfg *FilterConfig) {
		cfg.ChannelAllowlist = normalizeChannels(channels)
	}
}

// WithChannelDenylist excludes the provided channels.
func WithChannelDenylist(channels ...string) FilterOption {
	return func(cfg *FilterConfig) {
		cfg.ChannelDenylist = normalizeChannels(channels)
	}
}

// WithMachineActivityEnabled toggles machine activity visibility.
func WithMachineActivityEnabled(enabled bool) FilterOption {
	return func(cfg *FilterConfig) {
		cfg.MachineActivityEnabled = enabled
	}
}

// WithMachineActorTypes overrides the machine actor type identifiers.
func WithMachineActorTypes(types ...string) FilterOption {
	return func(cfg *FilterConfig) {
		cfg.MachineActorTypes = normalizeIdentifiers(types)
	}
}

// WithMachineDataKeys overrides the data keys used to flag machine activity.
func WithMachineDataKeys(keys ...string) FilterOption {
	return func(cfg *FilterConfig) {
		cfg.MachineDataKeys = normalizeIdentifiers(keys)
	}
}

// WithSuperadminScope allows superadmins to widen scope beyond actor context.
func WithSuperadminScope(enabled bool) FilterOption {
	return func(cfg *FilterConfig) {
		cfg.SuperadminScope = enabled
	}
}

// WithRoleAliases overrides the admin/superadmin role alias lists.
func WithRoleAliases(adminAliases, superadminAliases []string) FilterOption {
	return func(cfg *FilterConfig) {
		cfg.AdminRoleAliases = normalizeIdentifiers(adminAliases)
		cfg.SuperadminRoleAliases = normalizeIdentifiers(superadminAliases)
	}
}

// BuildFilterFromActor constructs a safe ActivityFilter using the auth actor context
// plus role-aware constraints and optional channel rules.
func BuildFilterFromActor(actor *auth.ActorContext, role string, req types.ActivityFilter, opts ...FilterOption) (types.ActivityFilter, error) {
	cfg := defaultFilterConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	ref, err := authctx.ActorRefFromActorContext(actor)
	if err != nil {
		return types.ActivityFilter{}, err
	}
	scope := authctx.ScopeFromActorContext(actor)

	roleName := normalizeIdentifier(role)
	if roleName == "" {
		roleName = normalizeIdentifier(actor.Role)
	}
	if roleName == "" {
		roleName = normalizeIdentifier(ref.Type)
	}

	isSuperadmin := roleMatches(roleName, cfg.SuperadminRoleAliases)
	isAdmin := isSuperadmin || roleMatches(roleName, cfg.AdminRoleAliases)

	filter := req
	filter.Actor = ref

	if isSuperadmin && cfg.SuperadminScope {
		filter.Scope = req.Scope
	} else {
		filter.Scope = scope
	}

	if !isAdmin {
		filter.UserID = ref.ID
		filter.ActorID = ref.ID
	}

	filter, err = applyChannelOptions(filter, cfg)
	if err != nil {
		return types.ActivityFilter{}, err
	}
	return filter, nil
}

var (
	defaultSuperadminRoleAliases = []string{
		types.ActorRoleSystemAdmin,
		"superadmin",
	}
	defaultAdminRoleAliases = []string{
		types.ActorRoleTenantAdmin,
		"admin",
		types.ActorRoleOrgAdmin,
	}
	defaultMachineActorTypes = []string{"system", "machine", "job", "task"}
	defaultMachineDataKeys   = []string{"system", "machine"}
)

func defaultFilterConfig() FilterConfig {
	return FilterConfig{
		ChannelAllowlist:       nil,
		ChannelDenylist:        nil,
		MachineActivityEnabled: true,
		MachineActorTypes:      cloneStrings(defaultMachineActorTypes),
		MachineDataKeys:        cloneStrings(defaultMachineDataKeys),
		SuperadminScope:        false,
		AdminRoleAliases:       normalizeIdentifiers(defaultAdminRoleAliases),
		SuperadminRoleAliases:  normalizeIdentifiers(defaultSuperadminRoleAliases),
	}
}

func applyChannelOptions(filter types.ActivityFilter, cfg FilterConfig) (types.ActivityFilter, error) {
	allow := normalizeChannels(cfg.ChannelAllowlist)
	deny := normalizeChannels(cfg.ChannelDenylist)
	reqDeny := normalizeChannels(filter.ChannelDenylist)
	deny = uniqueStrings(append(deny, reqDeny...))

	reqChannels := normalizeChannels(filter.Channels)
	reqChannel := normalizeChannel(filter.Channel)

	if len(reqChannels) > 0 {
		filter.Channels = reqChannels
		filter.Channel = ""
	} else if reqChannel != "" {
		filter.Channel = reqChannel
		filter.Channels = nil
	} else {
		filter.Channel = ""
		filter.Channels = nil
	}

	if len(allow) > 0 {
		if len(filter.Channels) > 0 {
			filter.Channels = intersectStrings(filter.Channels, allow)
			if len(filter.Channels) == 0 {
				return types.ActivityFilter{}, errors.New("activity: channel allowlist excludes requested channels")
			}
		} else if filter.Channel != "" {
			if !containsString(allow, filter.Channel) {
				return types.ActivityFilter{}, errors.New("activity: channel allowlist excludes requested channel")
			}
		} else {
			filter.Channels = allow
		}
	}

	if len(deny) > 0 {
		if filter.Channel != "" && containsString(deny, filter.Channel) {
			return types.ActivityFilter{}, errors.New("activity: channel denylist excludes requested channel")
		}
		if len(filter.Channels) > 0 {
			filter.Channels = subtractStrings(filter.Channels, deny)
			if len(filter.Channels) == 0 {
				return types.ActivityFilter{}, errors.New("activity: channel denylist excludes all channels")
			}
		}
		filter.ChannelDenylist = deny
	} else if len(reqDeny) > 0 {
		filter.ChannelDenylist = reqDeny
	}

	return filter, nil
}

func roleMatches(role string, aliases []string) bool {
	if role == "" {
		return false
	}
	for _, alias := range aliases {
		if normalizeIdentifier(alias) == role {
			return true
		}
	}
	return false
}

func normalizeIdentifiers(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeIdentifier(value)
		if normalized == "" {
			continue
		}
		out = append(out, normalized)
	}
	return uniqueStrings(out)
}

func normalizeIdentifier(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeChannels(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeChannel(value)
		if normalized == "" {
			continue
		}
		out = append(out, normalized)
	}
	return uniqueStrings(out)
}

func normalizeChannel(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func intersectStrings(a, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(b))
	for _, value := range b {
		seen[value] = struct{}{}
	}
	out := make([]string, 0, len(a))
	for _, value := range a {
		if _, ok := seen[value]; ok {
			out = append(out, value)
		}
	}
	return uniqueStrings(out)
}

func subtractStrings(values, deny []string) []string {
	if len(values) == 0 {
		return nil
	}
	if len(deny) == 0 {
		return values
	}
	denySet := make(map[string]struct{}, len(deny))
	for _, value := range deny {
		denySet[value] = struct{}{}
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, blocked := denySet[value]; blocked {
			continue
		}
		out = append(out, value)
	}
	return uniqueStrings(out)
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}
