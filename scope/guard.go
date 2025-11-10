package scope

import (
	"context"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

// Guard enforces resolved scopes and authorization policies for commands and
// queries. It is intentionally small so callers can swap custom guards in
// tests if needed.
type Guard interface {
	Enforce(ctx context.Context, actor types.ActorRef, requested types.ScopeFilter, action types.PolicyAction, target uuid.UUID) (types.ScopeFilter, error)
}

type guard struct {
	resolver types.ScopeResolver
	policy   types.AuthorizationPolicy
}

// NewGuard builds a Guard from the supplied resolver and policy. Nil
// dependencies are treated as no-ops.
func NewGuard(resolver types.ScopeResolver, policy types.AuthorizationPolicy) Guard {
	return guard{
		resolver: resolver,
		policy:   policy,
	}
}

// Ensure returns a non-nil guard so command/query constructors can accept nil
// guards when tests instantiate them directly.
func Ensure(g Guard) Guard {
	if g == nil {
		return guard{}
	}
	return g
}

// NopGuard returns a guard that leaves scopes unchanged and never blocks.
func NopGuard() Guard {
	return guard{}
}

// Enforce resolves and authorizes the requested scope for the action.
func (g guard) Enforce(ctx context.Context, actor types.ActorRef, requested types.ScopeFilter, action types.PolicyAction, target uuid.UUID) (types.ScopeFilter, error) {
	scope := requested
	if g.resolver != nil {
		resolved, err := g.resolver.ResolveScope(ctx, actor, requested)
		if err != nil {
			return types.ScopeFilter{}, err
		}
		scope = resolved
	}
	if g.policy != nil && action != "" {
		check := types.PolicyCheck{
			Actor:    actor,
			Scope:    scope,
			Action:   action,
			TargetID: target,
		}
		if err := g.policy.Authorize(ctx, check); err != nil {
			return types.ScopeFilter{}, err
		}
	}
	return scope, nil
}
