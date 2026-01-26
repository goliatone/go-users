package command

import (
	"context"

	featuregate "github.com/goliatone/go-featuregate/gate"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

const (
	featureUsersInvite        = "users.invite"
	featureUsersPasswordReset = "users.password_reset"
)

func featureEnabled(ctx context.Context, gate featuregate.FeatureGate, key string, scope types.ScopeFilter, userID uuid.UUID) (bool, error) {
	if gate == nil {
		return true, nil
	}
	chain := featureScopeChain(scope, userID)
	if len(chain) == 0 {
		return gate.Enabled(ctx, key)
	}
	return gate.Enabled(ctx, key, featuregate.WithScopeChain(chain))
}

func featureScopeChain(scope types.ScopeFilter, userID uuid.UUID) featuregate.ScopeChain {
	chain := featuregate.ScopeChain{}
	tenantID := ""
	orgID := ""
	if scope.TenantID != uuid.Nil {
		tenantID = scope.TenantID.String()
	}
	if scope.OrgID != uuid.Nil {
		orgID = scope.OrgID.String()
	}

	if userID != uuid.Nil {
		chain = append(chain, featuregate.ScopeRef{
			Kind:     featuregate.ScopeUser,
			ID:       userID.String(),
			TenantID: tenantID,
			OrgID:    orgID,
		})
	}
	if orgID != "" {
		chain = append(chain, featuregate.ScopeRef{
			Kind:     featuregate.ScopeOrg,
			ID:       orgID,
			TenantID: tenantID,
			OrgID:    orgID,
		})
	}
	if tenantID != "" {
		chain = append(chain, featuregate.ScopeRef{
			Kind:     featuregate.ScopeTenant,
			ID:       tenantID,
			TenantID: tenantID,
		})
	}
	if len(chain) == 0 {
		return nil
	}
	return append(chain, featuregate.ScopeRef{Kind: featuregate.ScopeSystem})
}
