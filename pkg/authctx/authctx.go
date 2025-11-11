package authctx

import (
	"context"

	auth "github.com/goliatone/go-auth"
	"github.com/goliatone/go-errors"
	"github.com/goliatone/go-router"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

const (
	textCodeActorMissing = "ACTOR_CONTEXT_MISSING"
	textCodeActorInvalid = "ACTOR_CONTEXT_INVALID"
)

// ActorFromContext is a thin wrapper around go-auth helpers so callers do not
// need to import auth directly when they only need the actor payload.
func ActorFromContext(ctx context.Context) (*auth.ActorContext, bool) {
	return auth.ActorFromContext(ctx)
}

// ActorFromRouterContext extracts the actor payload from router contexts using
// go-auth helpers.
func ActorFromRouterContext(ctx router.Context) (*auth.ActorContext, bool) {
	return auth.ActorFromRouterContext(ctx)
}

// ResolveActorContext returns the actor metadata stored by go-auth middleware
// or rebuilds it from JWT claims when the ContextEnricher hook was not
// configured.
func ResolveActorContext(ctx context.Context) (*auth.ActorContext, error) {
	if ctx == nil {
		return nil, errors.New("go-users: missing request context", errors.CategoryAuth).
			WithCode(errors.CodeUnauthorized).
			WithTextCode(textCodeActorMissing)
	}

	if actor, ok := auth.ActorFromContext(ctx); ok && actor != nil {
		return actor, nil
	}

	if claims, ok := auth.GetClaims(ctx); ok && claims != nil {
		if actor := auth.ActorContextFromClaims(claims); actor != nil {
			return actor, nil
		}
	}

	return nil, errors.New("go-users: auth actor context not found on request", errors.CategoryAuth).
		WithCode(errors.CodeUnauthorized).
		WithTextCode(textCodeActorMissing)
}

// ResolveActorContextFromRouter mirrors ResolveActorContext for router
// transports where middleware stores actor metadata directly in the router
// context.
func ResolveActorContextFromRouter(ctx router.Context) (*auth.ActorContext, error) {
	if ctx == nil {
		return nil, errors.New("go-users: missing router context", errors.CategoryAuth).
			WithCode(errors.CodeUnauthorized).
			WithTextCode(textCodeActorMissing)
	}

	if actor, ok := auth.ActorFromRouterContext(ctx); ok && actor != nil {
		return actor, nil
	}

	return ResolveActorContext(ctx.Context())
}

// ResolveActor returns both the actor reference used by go-users commands and
// the richer auth.ActorContext payload for transports that need tenant/org
// metadata.
func ResolveActor(ctx context.Context) (types.ActorRef, *auth.ActorContext, error) {
	actorCtx, err := ResolveActorContext(ctx)
	if err != nil {
		return types.ActorRef{}, nil, err
	}
	ref, err := ActorRefFromActorContext(actorCtx)
	if err != nil {
		return types.ActorRef{}, nil, err
	}
	return ref, actorCtx, nil
}

// ActorRefFromActorContext converts the auth middleware payload into the
// smaller ActorRef consumed across go-users.
func ActorRefFromActorContext(actor *auth.ActorContext) (types.ActorRef, error) {
	if actor == nil {
		return types.ActorRef{}, errors.New("go-users: actor context is nil", errors.CategoryAuth).
			WithCode(errors.CodeUnauthorized).
			WithTextCode(textCodeActorInvalid)
	}
	if actor.ActorID == "" {
		return types.ActorRef{}, errors.New("go-users: actor context missing actor_id", errors.CategoryAuth).
			WithCode(errors.CodeUnauthorized).
			WithTextCode(textCodeActorInvalid)
	}

	actorID, err := uuid.Parse(actor.ActorID)
	if err != nil {
		return types.ActorRef{}, errors.Wrap(err, errors.CategoryAuth, "go-users: invalid actor_id on auth context").
			WithCode(errors.CodeUnauthorized).
			WithTextCode(textCodeActorInvalid)
	}

	ref := types.ActorRef{
		ID:   actorID,
		Type: actor.Role,
	}
	if ref.Type == "" && actor.Subject != "" {
		ref.Type = actor.Subject
	}
	return ref, nil
}

// ScopeFromActorContext builds a ScopeFilter using the normalized tenant/org
// identifiers stored by go-auth middleware.
func ScopeFromActorContext(actor *auth.ActorContext) types.ScopeFilter {
	if actor == nil {
		return types.ScopeFilter{}
	}

	scope := types.ScopeFilter{}
	if tenant := parseUUID(actor.TenantID); tenant != uuid.Nil {
		scope.TenantID = tenant
	}
	if org := parseUUID(actor.OrganizationID); org != uuid.Nil {
		scope.OrgID = org
	}
	return scope
}

func parseUUID(raw string) uuid.UUID {
	if raw == "" {
		return uuid.Nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil
	}
	return id
}
