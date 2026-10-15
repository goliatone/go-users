package goauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	auth "github.com/goliatone/go-auth"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

var (
	ErrScopedPermissionsConfig = errors.New("goauth: scoped permissions require request/backend resolvers and a managed cache key")
	ErrPermissionScopeMismatch = errors.New("goauth: permission scope does not match normalized principal")
)

// ScopedPermissionsRequest binds host-authorized scope to a current principal
// and authorization version. The host must read trusted state, not token hints.
type ScopedPermissionsRequest struct {
	Current auth.CurrentPermissionsRequest
	Scope   types.ScopeFilter
}

// ScopedPermissionsRequestResolver checks current host state on every call,
// including calls that may subsequently hit a permission cache.
type ScopedPermissionsRequestResolver func(context.Context) (ScopedPermissionsRequest, error)

// ScopedPermissionsResolverConfig reuses go-auth's cache configuration. KeyFunc
// must be unset: the adapter supplies complete scope/version cache identity.
type ScopedPermissionsResolverConfig struct {
	RequestResolver ScopedPermissionsRequestResolver
	Cache           auth.CachedPermissionsResolverConfig
}

// ScopedPermissionsResolver validates host state before both go-auth cache layers.
type ScopedPermissionsResolver struct {
	request ScopedPermissionsRequestResolver
	cache   *auth.CachedPermissionsResolver
}

// NewScopedPermissionsResolver creates an opt-in scoped cache adapter without
// altering go-auth defaults. Missing authoritative context denies before caching.
func NewScopedPermissionsResolver(cfg ScopedPermissionsResolverConfig) (*ScopedPermissionsResolver, error) {
	if cfg.RequestResolver == nil || cfg.Cache.Resolver == nil || cfg.Cache.KeyFunc != nil {
		return nil, ErrScopedPermissionsConfig
	}
	cfg.Cache.KeyFunc = scopedPermissionsCacheKey
	return &ScopedPermissionsResolver{request: cfg.RequestResolver, cache: auth.NewCachedPermissionsResolver(cfg.Cache)}, nil
}

// ResolvePermissions obtains a current authorized snapshot before consulting
// either cache layer. The backend should consume that bound snapshot.
func (r *ScopedPermissionsResolver) ResolvePermissions(ctx context.Context) ([]string, error) {
	if r == nil || r.request == nil || r.cache == nil {
		return nil, ErrScopedPermissionsConfig
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	request, err := r.request(ctx)
	if err != nil {
		return nil, err
	}
	// A freshness guard may request an authoritative backend refresh. The host
	// callback still chooses current identity/version, but cannot downgrade it.
	if upstream, ok := auth.CurrentPermissionsRequestFromContext(ctx); ok && upstream.ForceRefresh {
		request.Current.ForceRefresh = true
	}
	request.Scope, err = request.Scope.NormalizeLabels()
	if err != nil {
		return nil, err
	}
	if !sameScopeID(request.Current.Principal.TenantID(), request.Scope.TenantID) || !sameScopeID(request.Current.Principal.OrganizationID(), request.Scope.OrgID) {
		return nil, ErrPermissionScopeMismatch
	}
	currentCtx, err := auth.WithCurrentPermissionsRequest(ctx, request.Current)
	if err != nil {
		return nil, err
	}
	request.Current, _ = auth.CurrentPermissionsRequestFromContext(currentCtx)
	return r.cache.ResolvePermissions(context.WithValue(currentCtx, scopedPermissionsContextKey{}, request))
}

// ResolverFunc adapts the scoped resolver to go-auth's existing resolver API.
func (r *ScopedPermissionsResolver) ResolverFunc() auth.PermissionResolverFunc {
	return r.ResolvePermissions
}

// InvalidateScope reuses go-auth's bounded principal/tenant/session invalidation
// across custom scopes. Callers repeat while more=true; request-local entries
// require a new authoritative version to become obsolete.
func (r *ScopedPermissionsResolver) InvalidateScope(ctx context.Context, invalidation auth.PermissionInvalidationScope, limit int) (int, bool, error) {
	if r == nil || r.cache == nil {
		return 0, false, ErrScopedPermissionsConfig
	}
	return r.cache.InvalidateScope(ctx, invalidation, limit)
}

type scopedPermissionsContextKey struct{}

// ScopedPermissionsRequestFromContext returns the bound snapshot with detached
// labels/principal metadata. Only the scoped adapter supplies this context value.
func ScopedPermissionsRequestFromContext(ctx context.Context) (ScopedPermissionsRequest, bool) {
	if ctx == nil {
		return ScopedPermissionsRequest{}, false
	}
	request, ok := ctx.Value(scopedPermissionsContextKey{}).(ScopedPermissionsRequest)
	if !ok {
		return ScopedPermissionsRequest{}, false
	}
	request.Current.Principal = request.Current.Principal.Clone()
	request.Scope = request.Scope.Clone()
	return request, true
}

func scopedPermissionsCacheKey(ctx context.Context) (string, bool) {
	request, ok := ScopedPermissionsRequestFromContext(ctx)
	if !ok {
		return "", false
	}
	base, ok := auth.DefaultPermissionsCacheKeyFromContext(ctx)
	if !ok {
		return "", false
	}
	encoded, err := json.Marshal(struct {
		Identity string
		Scope    types.ScopeFilter
	}{Identity: base, Scope: request.Scope})
	if err != nil {
		return "", false
	}
	digest := sha256.Sum256(encoded)
	return "go-users:permissions:v1:" + hex.EncodeToString(digest[:]), true
}

func sameScopeID(principalID string, scopeID uuid.UUID) bool {
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return scopeID == uuid.Nil
	}
	id, err := uuid.Parse(principalID)
	return err == nil && id == scopeID
}
