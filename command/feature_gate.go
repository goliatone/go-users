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
	scopeSet := featureScopeSet(scope, userID)
	if scopeSet == nil {
		return gate.Enabled(ctx, key)
	}
	return gate.Enabled(ctx, key, featuregate.WithScopeSet(*scopeSet))
}

func featureScopeSet(scope types.ScopeFilter, userID uuid.UUID) *featuregate.ScopeSet {
	tenantID := ""
	orgID := ""
	if scope.TenantID != uuid.Nil {
		tenantID = scope.TenantID.String()
	}
	if scope.OrgID != uuid.Nil {
		orgID = scope.OrgID.String()
	}

	user := ""
	if userID != uuid.Nil {
		user = userID.String()
	}

	if tenantID == "" && orgID == "" && user == "" {
		return nil
	}
	return &featuregate.ScopeSet{
		System:   true,
		TenantID: tenantID,
		OrgID:    orgID,
		UserID:   user,
	}
}
