package crudguard

import (
	"errors"
	"fmt"

	auth "github.com/goliatone/go-auth"
	"github.com/goliatone/go-crud"
	goerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-users/pkg/authctx"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

const (
	textCodeScopeDenied          = "SCOPE_DENIED"
	textCodeScopeEnforcementFail = "SCOPE_ENFORCEMENT_FAILED"
	textCodeMissingPolicy        = "SCOPE_POLICY_MISSING"
	textCodeMissingContext       = "CONTEXT_MISSING"
)

// ScopeExtractor builds a requested ScopeFilter from the crud context prior to
// guard evaluation. Implementations may inspect query parameters or request
// bodies to derive tenant/org filters.
type ScopeExtractor func(ctx crud.Context, actor *auth.ActorContext) (types.ScopeFilter, error)

// Config drives Adapter construction.
type Config struct {
	Guard          scope.Guard
	Logger         types.Logger
	PolicyMap      map[crud.CrudOperation]types.PolicyAction
	ScopeExtractor ScopeExtractor
	FallbackAction types.PolicyAction
}

// Adapter turns go-crud operations into scope guard enforcement calls.
type Adapter struct {
	guard          scope.Guard
	logger         types.Logger
	scopeExtractor ScopeExtractor
	policyMap      map[crud.CrudOperation]types.PolicyAction
	fallbackAction types.PolicyAction
}

// GuardInput captures per-request parameters supplied by transports.
type GuardInput struct {
	Context   crud.Context
	Operation crud.CrudOperation
	TargetID  uuid.UUID
	Scope     types.ScopeFilter
	Bypass    *BypassConfig
}

// GuardResult reports the resolved scope and actor metadata returned by the
// adapter.
type GuardResult struct {
	Actor        types.ActorRef
	Scope        types.ScopeFilter
	Operation    crud.CrudOperation
	Bypassed     bool
	BypassReason string
}

// BypassConfig explicitly allows guard skips for whitelisted routes (e.g.
// schema exports). It must never be enabled by default.
type BypassConfig struct {
	Enabled bool
	Reason  string
}

// DefaultScopeExtractor builds the requested scope from the actor context.
func DefaultScopeExtractor(_ crud.Context, actor *auth.ActorContext) (types.ScopeFilter, error) {
	return authctx.ScopeFromActorContext(actor), nil
}

// NewAdapter constructs a Guard adapter and validates the supplied config.
func NewAdapter(cfg Config) (*Adapter, error) {
	if cfg.Guard == nil {
		return nil, goerrors.New("go-users: scope guard is required", goerrors.CategoryInternal).
			WithCode(goerrors.CodeInternal).
			WithTextCode(textCodeScopeEnforcementFail)
	}
	if len(cfg.PolicyMap) == 0 && cfg.FallbackAction == "" {
		return nil, goerrors.New("go-users: policy map or fallback action must be provided", goerrors.CategoryInternal).
			WithCode(goerrors.CodeInternal).
			WithTextCode(textCodeMissingPolicy)
	}

	scopeExtractor := cfg.ScopeExtractor
	if scopeExtractor == nil {
		scopeExtractor = DefaultScopeExtractor
	}

	logger := cfg.Logger
	if logger == nil {
		logger = types.NopLogger{}
	}

	return &Adapter{
		guard:          scope.Ensure(cfg.Guard),
		logger:         logger,
		scopeExtractor: scopeExtractor,
		policyMap:      clonePolicyMap(cfg.PolicyMap),
		fallbackAction: cfg.FallbackAction,
	}, nil
}

// Enforce resolves the actor, derives the requested scope, optionally bypasses,
// and finally enforces the scope guard with the mapped PolicyAction.
func (a *Adapter) Enforce(in GuardInput) (GuardResult, error) {
	if in.Context == nil {
		return GuardResult{}, goerrors.New("go-users: crudguard requires a context", goerrors.CategoryInternal).
			WithCode(goerrors.CodeInternal).
			WithTextCode(textCodeMissingContext)
	}

	ctx := in.Context.UserContext()
	actorRef, actorCtx, err := authctx.ResolveActor(ctx)
	if err != nil {
		return GuardResult{}, err
	}

	requested, err := a.scopeExtractor(in.Context, actorCtx)
	if err != nil {
		return GuardResult{}, err
	}
	requested = mergeScopeFilters(requested, in.Scope)

	if in.Bypass != nil && in.Bypass.Enabled {
		a.logger.Info("crudguard: bypassing guard enforcement", "operation", string(in.Operation), "reason", in.Bypass.Reason)
		if requested.TenantID == uuid.Nil && requested.OrgID == uuid.Nil {
			requested = mergeScopeFilters(requested, authctx.ScopeFromActorContext(actorCtx))
		}
		return GuardResult{
			Actor:        actorRef,
			Scope:        requested,
			Operation:    in.Operation,
			Bypassed:     true,
			BypassReason: in.Bypass.Reason,
		}, nil
	}

	action, err := a.actionForOperation(in.Operation)
	if err != nil {
		return GuardResult{}, err
	}

	resolved, err := a.guard.Enforce(ctx, actorRef, requested, action, in.TargetID)
	if err != nil {
		return GuardResult{}, wrapGuardError(err, action)
	}

	return GuardResult{
		Actor:     actorRef,
		Scope:     resolved,
		Operation: in.Operation,
	}, nil
}

func (a *Adapter) actionForOperation(op crud.CrudOperation) (types.PolicyAction, error) {
	if act, ok := a.policyMap[op]; ok && act != "" {
		return act, nil
	}
	if a.fallbackAction != "" {
		return a.fallbackAction, nil
	}
	return "", goerrors.New(fmt.Sprintf("go-users: no policy action configured for %s", op), goerrors.CategoryInternal).
		WithCode(goerrors.CodeInternal).
		WithTextCode(textCodeMissingPolicy)
}

func mergeScopeFilters(base, override types.ScopeFilter) types.ScopeFilter {
	result := base.Clone()
	if override.TenantID != uuid.Nil {
		result.TenantID = override.TenantID
	}
	if override.OrgID != uuid.Nil {
		result.OrgID = override.OrgID
	}
	if len(override.Labels) > 0 {
		if result.Labels == nil {
			result.Labels = make(map[string]uuid.UUID, len(override.Labels))
		}
		for k, v := range override.Labels {
			if v == uuid.Nil {
				continue
			}
			result.Labels[k] = v
		}
	}
	return result
}

func wrapGuardError(err error, action types.PolicyAction) error {
	if errors.Is(err, types.ErrUnauthorizedScope) {
		return goerrors.Wrap(err, goerrors.CategoryAuthz, "go-users: scope guard rejected the request").
			WithCode(goerrors.CodeForbidden).
			WithTextCode(textCodeScopeDenied)
	}
	return goerrors.Wrap(err, goerrors.CategoryInternal, fmt.Sprintf("go-users: scope guard failed for action %s", action)).
		WithCode(goerrors.CodeInternal).
		WithTextCode(textCodeScopeEnforcementFail)
}
