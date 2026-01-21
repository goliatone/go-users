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
	if scopeSet.TenantID == "" && scopeSet.OrgID == "" && scopeSet.UserID == "" {
		return gate.Enabled(ctx, key)
	}
	return gate.Enabled(ctx, key, featuregate.WithScopeSet(scopeSet))
}

func featureScopeSet(scope types.ScopeFilter, userID uuid.UUID) featuregate.ScopeSet {
	set := featuregate.ScopeSet{}
	if scope.TenantID != uuid.Nil {
		set.TenantID = scope.TenantID.String()
	}
	if scope.OrgID != uuid.Nil {
		set.OrgID = scope.OrgID.String()
	}
	if userID != uuid.Nil {
		set.UserID = userID.String()
	}
	return set
}
