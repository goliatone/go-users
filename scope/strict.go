package scope

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

var (
	ErrScopeResolverRequired       = errors.New("go-users: strict scope guard requires a resolver")
	ErrAuthorizationPolicyRequired = errors.New("go-users: strict scope guard requires an authorization policy")
	ErrStrictGuardRequired         = errors.New("go-users: a validated strict scope guard is required")
	ErrPolicyActionRequired        = errors.New("go-users: strict scope guard requires a policy action")
)

// StrictGuard requires explicit resolution and authorization dependencies. Its
// zero and nil values deny enforcement. Use NewStrictGuard to validate at startup.
type StrictGuard struct {
	resolver types.ScopeResolver
	policy   types.AuthorizationPolicy
}

// NewStrictGuard constructs a guard that requires both dependencies. It rejects
// typed nil pointers/functions as well as nil interfaces. On error, the returned
// guard denies enforcement even if a caller accidentally ignores the error.
func NewStrictGuard(resolver types.ScopeResolver, policy types.AuthorizationPolicy) (*StrictGuard, error) {
	g := &StrictGuard{resolver: resolver, policy: policy}
	return g, g.validate()
}

// ValidateStrict verifies built-in strict guard wiring. A nonnil permissive or
// custom guard does not establish this contract. Policies still own access rules.
func ValidateStrict(g Guard) error {
	strict, ok := g.(*StrictGuard)
	if !ok {
		return ErrStrictGuardRequired
	}
	return strict.validate()
}

func (g *StrictGuard) validate() error {
	if g == nil {
		return errors.Join(ErrScopeResolverRequired, ErrAuthorizationPolicyRequired)
	}
	var missing []error
	if nilDependency(g.resolver) {
		missing = append(missing, ErrScopeResolverRequired)
	}
	if nilDependency(g.policy) {
		missing = append(missing, ErrAuthorizationPolicyRequired)
	}
	return errors.Join(missing...)
}

func nilDependency(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// Enforce validates configuration before resolving and authorizing the scope.
func (g *StrictGuard) Enforce(ctx context.Context, actor types.ActorRef, requested types.ScopeFilter, action types.PolicyAction, target uuid.UUID) (types.ScopeFilter, error) {
	if err := g.validate(); err != nil {
		return types.ScopeFilter{}, err
	}
	if strings.TrimSpace(string(action)) == "" {
		return types.ScopeFilter{}, ErrPolicyActionRequired
	}
	return (guard{resolver: g.resolver, policy: g.policy}).Enforce(ctx, actor, requested, action, target)
}
