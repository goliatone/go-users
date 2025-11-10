package types

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// PolicyAction enumerates the supported authorization actions enforced by the
// multi-tenant scope guard. Host applications can remap these actions to their
// own policies or ACL systems.
type PolicyAction string

const (
	PolicyActionUsersRead        PolicyAction = "users:read"
	PolicyActionUsersWrite       PolicyAction = "users:write"
	PolicyActionRolesRead        PolicyAction = "roles:read"
	PolicyActionRolesWrite       PolicyAction = "roles:write"
	PolicyActionActivityRead     PolicyAction = "activity:read"
	PolicyActionActivityWrite    PolicyAction = "activity:write"
	PolicyActionPreferencesRead  PolicyAction = "preferences:read"
	PolicyActionPreferencesWrite PolicyAction = "preferences:write"
	PolicyActionProfilesRead     PolicyAction = "profiles:read"
	PolicyActionProfilesWrite    PolicyAction = "profiles:write"
)

// PolicyCheck captures the authorization context for a single command/query.
type PolicyCheck struct {
	Actor    ActorRef
	Scope    ScopeFilter
	Action   PolicyAction
	TargetID uuid.UUID
}

// ScopeResolver resolves requested scopes into canonical tenant/org values
// based on the actor and host application rules.
type ScopeResolver interface {
	ResolveScope(ctx context.Context, actor ActorRef, requested ScopeFilter) (ScopeFilter, error)
}

// ScopeResolverFunc adapts bare functions to ScopeResolver.
type ScopeResolverFunc func(ctx context.Context, actor ActorRef, requested ScopeFilter) (ScopeFilter, error)

// ResolveScope implements ScopeResolver.
func (f ScopeResolverFunc) ResolveScope(ctx context.Context, actor ActorRef, requested ScopeFilter) (ScopeFilter, error) {
	return f(ctx, actor, requested)
}

// AuthorizationPolicy governs whether an actor can access the requested scope
// for the supplied action.
type AuthorizationPolicy interface {
	Authorize(ctx context.Context, check PolicyCheck) error
}

// AuthorizationPolicyFunc adapts bare functions to AuthorizationPolicy.
type AuthorizationPolicyFunc func(ctx context.Context, check PolicyCheck) error

// Authorize implements AuthorizationPolicy.
func (f AuthorizationPolicyFunc) Authorize(ctx context.Context, check PolicyCheck) error {
	return f(ctx, check)
}

var (
	// ErrUnauthorizedScope indicates the supplied scope is not visible to the
	// actor according to the configured authorization policy.
	ErrUnauthorizedScope = errors.New("go-users: actor not authorized for scope")
)

// PassthroughScopeResolver returns the requested scope as-is. This is used
// when host applications do not provide a custom resolver.
type PassthroughScopeResolver struct{}

// ResolveScope implements ScopeResolver.
func (PassthroughScopeResolver) ResolveScope(_ context.Context, _ ActorRef, requested ScopeFilter) (ScopeFilter, error) {
	return requested, nil
}

// AllowAllAuthorizationPolicy allows every action/scope combination.
type AllowAllAuthorizationPolicy struct{}

// Authorize implements AuthorizationPolicy.
func (AllowAllAuthorizationPolicy) Authorize(context.Context, PolicyCheck) error {
	return nil
}
