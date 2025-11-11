package types

import "strings"

const (
	// ActorRoleSystemAdmin represents site-wide administrators with unrestricted access.
	ActorRoleSystemAdmin = "system_admin"
	// ActorRoleTenantAdmin represents administrators scoped to a tenant/org.
	ActorRoleTenantAdmin = "tenant_admin"
	// ActorRoleOrgAdmin is reserved for nested org/workspace administrators.
	ActorRoleOrgAdmin = "org_admin"
	// ActorRoleSupport represents support agents that should be limited to self/owner scopes.
	ActorRoleSupport = "support"
)

// RoleName normalizes the actor role for comparisons.
func (a ActorRef) RoleName() string {
	return normalizeRole(a.Type)
}

// IsRole reports whether the actor matches the provided role.
func (a ActorRef) IsRole(role string) bool {
	role = normalizeRole(role)
	if role == "" {
		return a.RoleName() == ""
	}
	return a.RoleName() == role
}

// IsSupport reports whether the actor should be treated as a support agent.
func (a ActorRef) IsSupport() bool {
	return a.IsRole(ActorRoleSupport)
}

// IsTenantAdmin reports whether the actor is scoped as a tenant administrator.
func (a ActorRef) IsTenantAdmin() bool {
	return a.IsRole(ActorRoleTenantAdmin)
}

// IsSystemAdmin reports whether the actor is a global/system administrator.
func (a ActorRef) IsSystemAdmin() bool {
	return a.IsRole(ActorRoleSystemAdmin)
}

func normalizeRole(role string) string {
	return strings.ToLower(strings.TrimSpace(role))
}
