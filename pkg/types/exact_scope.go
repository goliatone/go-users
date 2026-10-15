package types

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
)

// CanonicalScope returns detached normalized labels and their stable identity.
// TenantID and OrgID remain separate dimensions; an empty label set has key "".
func CanonicalScope(scope ScopeFilter) (ScopeFilter, string, error) {
	normalized, err := scope.NormalizeLabels()
	if err != nil {
		return ScopeFilter{}, "", err
	}
	if len(normalized.Labels) == 0 {
		normalized.Labels = map[string]uuid.UUID{}
		return normalized, "", nil
	}
	encoded, err := json.Marshal(normalized.Labels)
	if err != nil {
		return ScopeFilter{}, "", err
	}
	digest := sha256.Sum256(encoded)
	return normalized, hex.EncodeToString(digest[:]), nil
}

// ExactScopeRoleRegistry is optional. Legacy registries need not implement it.
// CatalogScope selects the definition; Assignment.Scope selects the grant.
type ExactScopeRoleRegistry interface {
	RoleRegistry
	AssignRoleFromCatalog(context.Context, ScopedRoleGrant) error
}

type ScopedRoleGrant struct {
	UserID       uuid.UUID
	RoleID       uuid.UUID
	CatalogScope ScopeFilter
	Scope        ScopeFilter
	ActorID      uuid.UUID
}
