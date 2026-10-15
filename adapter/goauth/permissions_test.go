package goauth

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	auth "github.com/goliatone/go-auth"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type selectedPermissionScope struct{}

func scopedContext(ctx context.Context, scope types.ScopeFilter) context.Context {
	return context.WithValue(ctx, selectedPermissionScope{}, scope)
}

func TestGoAuthDefaultIdentityDoesNotSupplyCustomScopeIsolation(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	actor := &auth.ActorContext{ActorID: uuid.NewString(), Role: "member", Metadata: map[string]any{auth.PermissionsVersionMetadataKey: "1"}}
	base := auth.WithActorContext(context.Background(), actor)
	resolver := auth.NewCachedPermissionsResolver(auth.CachedPermissionsResolverConfig{
		TTL: time.Hour,
		Resolver: func(ctx context.Context) ([]string, error) {
			scope := ctx.Value(selectedPermissionScope{}).(types.ScopeFilter)
			if scope.Label("group") == a {
				return []string{"users:read", "users:write"}, nil
			}
			return []string{"users:read"}, nil
		},
	})
	request := auth.WithResolvedPermissionsCache(base)
	first, err := resolver.ResolvePermissions(scopedContext(request, types.ScopeFilter{}.WithLabel("group", a)))
	require.NoError(t, err)
	second, err := resolver.ResolvePermissions(scopedContext(request, types.ScopeFilter{}.WithLabel("group", b)))
	require.NoError(t, err)
	require.Equal(t, first, second, "Group-blind request-local identity reuses A permissions in B")
	second, err = resolver.ResolvePermissions(scopedContext(base, types.ScopeFilter{}.WithLabel("group", b)))
	require.NoError(t, err)
	require.Equal(t, first, second, "the default cross-request key has the same limitation")
}

type scopedPermissionHost struct {
	principal                  auth.AuthenticatedPrincipal
	versions                   *auth.InMemoryAuthorizationVersionStore
	mu                         sync.RWMutex
	permissions                map[uuid.UUID][]string
	denied                     bool
	requestCalls, backendCalls atomic.Int64
}

func newScopedPermissionHost(t *testing.T) *scopedPermissionHost {
	t.Helper()
	principal, err := auth.NewAuthenticatedPrincipal(auth.AuthenticatedPrincipalInput{ApplicationSubject: uuid.NewString(), Provider: "host", ProviderSubject: "linked", LocalSessionID: "one-session"})
	require.NoError(t, err)
	host := &scopedPermissionHost{principal: principal, versions: auth.NewInMemoryAuthorizationVersionStore(nil), permissions: map[uuid.UUID][]string{}}
	_, err = host.versions.AdvanceAuthorizationVersion(context.Background(), principal.ApplicationSubject(), "", "initial", func(context.Context) error { return nil })
	require.NoError(t, err)
	return host
}

func (h *scopedPermissionHost) request(ctx context.Context) (ScopedPermissionsRequest, error) {
	h.requestCalls.Add(1)
	h.mu.RLock()
	denied := h.denied
	h.mu.RUnlock()
	if denied {
		return ScopedPermissionsRequest{}, types.ErrUnauthorizedScope
	}
	scope, ok := ctx.Value(selectedPermissionScope{}).(types.ScopeFilter)
	if !ok {
		return ScopedPermissionsRequest{}, types.ErrUnauthorizedScope
	}
	version, err := h.versions.CurrentAuthorizationVersion(ctx, h.principal.ApplicationSubject(), h.principal.TenantID())
	if err != nil {
		return ScopedPermissionsRequest{}, err
	}
	return ScopedPermissionsRequest{Current: auth.CurrentPermissionsRequest{Principal: h.principal, Version: version.Version, Role: "member"}, Scope: scope}, nil
}

func (h *scopedPermissionHost) backend(ctx context.Context) ([]string, error) {
	h.backendCalls.Add(1)
	request, ok := ScopedPermissionsRequestFromContext(ctx)
	if !ok {
		return nil, errors.New("missing bound permission request")
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return slices.Clone(h.permissions[request.Scope.Label("group")]), nil
}

func (h *scopedPermissionHost) resolver(t *testing.T, ttl time.Duration) *ScopedPermissionsResolver {
	t.Helper()
	resolver, err := NewScopedPermissionsResolver(ScopedPermissionsResolverConfig{RequestResolver: h.request, Cache: auth.CachedPermissionsResolverConfig{Resolver: h.backend, TTL: ttl}})
	require.NoError(t, err)
	return resolver
}

func TestScopedPermissionsSeparatesRequestAndCrossRequestScopes(t *testing.T) {
	for _, ttl := range []time.Duration{0, time.Hour} {
		t.Run(ttl.String(), func(t *testing.T) {
			host := newScopedPermissionHost(t)
			a, b := uuid.New(), uuid.New()
			host.permissions[a], host.permissions[b] = []string{"users:read", "users:write"}, []string{"users:read"}
			resolver := host.resolver(t, ttl)
			parent := auth.WithResolvedPermissionsCache(context.Background())
			scopeA, scopeB := types.ScopeFilter{}.WithLabel("group", a), types.ScopeFilter{}.WithLabel("group", b)
			for _, scope := range []types.ScopeFilter{scopeA, scopeB, scopeA} {
				permissions, err := resolver.ResolvePermissions(scopedContext(parent, scope))
				require.NoError(t, err)
				require.Equal(t, host.permissions[scope.Label("group")], permissions)
			}
			require.Equal(t, int64(2), host.backendCalls.Load(), "both scope keys participate in request-local caching")
			_, err := resolver.ResolvePermissions(scopedContext(parent, types.ScopeFilter{Labels: map[string]uuid.UUID{" Group ": a}}))
			require.NoError(t, err)
			require.Equal(t, int64(2), host.backendCalls.Load(), "equivalent aliases use the canonical identity")
			// Additional labels must partition permissions even when the Group is unchanged.
			_, err = resolver.ResolvePermissions(scopedContext(parent, scopeA.WithLabel("team", uuid.New())))
			require.NoError(t, err)
			require.Equal(t, int64(3), host.backendCalls.Load())
			next := auth.WithResolvedPermissionsCache(context.Background())
			permissions, err := resolver.ResolvePermissions(scopedContext(next, scopeB))
			require.NoError(t, err)
			require.Equal(t, []string{"users:read"}, permissions)
			if ttl > 0 {
				require.Equal(t, int64(3), host.backendCalls.Load())
			} else {
				require.Equal(t, int64(4), host.backendCalls.Load())
			}
			require.Equal(t, int64(6), host.requestCalls.Load(), "authoritative request resolution precedes every cache lookup")
		})
	}
}

func TestScopedPermissionsVersionAdvanceAndInvalidation(t *testing.T) {
	host := newScopedPermissionHost(t)
	group := uuid.New()
	host.permissions[group] = []string{"users:write"}
	resolver := host.resolver(t, time.Hour)
	ctx := scopedContext(auth.WithResolvedPermissionsCache(context.Background()), types.ScopeFilter{}.WithLabel("group", group))
	permissions, err := resolver.ResolvePermissions(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"users:write"}, permissions)
	_, err = host.versions.AdvanceAuthorizationVersion(ctx, host.principal.ApplicationSubject(), "", "revoke", func(context.Context) error {
		host.mu.Lock()
		defer host.mu.Unlock()
		delete(host.permissions, group)
		return nil
	})
	require.NoError(t, err)
	permissions, err = resolver.ResolvePermissions(ctx) // Same parent request cache, new authoritative version.
	require.NoError(t, err)
	require.Empty(t, permissions)
	require.Equal(t, int64(2), host.backendCalls.Load())
	host.mu.Lock()
	host.denied = true
	host.mu.Unlock()
	_, err = resolver.ResolvePermissions(ctx)
	require.ErrorIs(t, err, types.ErrUnauthorizedScope, "current-state denial cannot be skipped by a cache hit")
	require.Equal(t, int64(2), host.backendCalls.Load())
	host.mu.Lock()
	host.denied = false
	host.mu.Unlock()
	deleted, more, err := resolver.InvalidateScope(context.Background(), auth.PermissionInvalidationScope{ApplicationSubject: host.principal.ApplicationSubject()}, 100)
	require.NoError(t, err)
	require.Positive(t, deleted)
	require.False(t, more)
	_, err = resolver.ResolvePermissions(scopedContext(context.Background(), types.ScopeFilter{}.WithLabel("group", group)))
	require.NoError(t, err)
	require.Equal(t, int64(3), host.backendCalls.Load(), "invalidation removes all cross-request scope entries for the principal")
}

func TestScopedPermissionsConcurrentContextsDoNotShareGroups(t *testing.T) {
	host := newScopedPermissionHost(t)
	a, b := uuid.New(), uuid.New()
	host.permissions[a], host.permissions[b] = []string{"write"}, []string{"read"}
	resolver := host.resolver(t, time.Hour)
	parent := auth.WithResolvedPermissionsCache(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		group, want := a, "write"
		if i%2 == 1 {
			group, want = b, "read"
		}
		wg.Add(1)
		go func(group uuid.UUID, want string) {
			defer wg.Done()
			permissions, err := resolver.ResolvePermissions(scopedContext(parent, types.ScopeFilter{}.WithLabel("group", group)))
			if err != nil || !slices.Equal(permissions, []string{want}) {
				t.Errorf("scope %s returned %v, %v", group, permissions, err)
			}
		}(group, want)
	}
	wg.Wait()
	require.Equal(t, int64(2), host.backendCalls.Load(), "same-scope calls deduplicate while scopes remain independent")
}

func TestScopedPermissionsRejectsInvalidStateBeforeCacheLookup(t *testing.T) {
	host := newScopedPermissionHost(t)
	group := uuid.New()
	host.permissions[group] = []string{"users:write"}
	valid, err := host.request(scopedContext(context.Background(), types.ScopeFilter{}.WithLabel("group", group)))
	require.NoError(t, err)
	outage := errors.New("version store unavailable")
	for _, tc := range []struct {
		name    string
		mutate  func(*ScopedPermissionsRequest)
		failure error
		want    error
	}{
		{name: "missing version", mutate: func(r *ScopedPermissionsRequest) { r.Current.Version = " " }, want: auth.ErrPermissionVersionMissing},
		{name: "missing principal", mutate: func(r *ScopedPermissionsRequest) { r.Current.Principal = auth.AuthenticatedPrincipal{} }, want: auth.ErrPermissionVersionMissing},
		{name: "scope mismatch", mutate: func(r *ScopedPermissionsRequest) { r.Scope.TenantID = uuid.New() }, want: ErrPermissionScopeMismatch},
		{name: "invalid labels", mutate: func(r *ScopedPermissionsRequest) { r.Scope.Labels = map[string]uuid.UUID{" ": group} }, want: types.ErrInvalidScopeLabels},
		{name: "outage", failure: outage, want: outage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fail := false
			resolver, err := NewScopedPermissionsResolver(ScopedPermissionsResolverConfig{RequestResolver: func(context.Context) (ScopedPermissionsRequest, error) {
				r := valid
				r.Scope = valid.Scope.Clone()
				if fail {
					if tc.failure != nil {
						return ScopedPermissionsRequest{}, tc.failure
					}
					tc.mutate(&r)
				}
				return r, nil
			}, Cache: auth.CachedPermissionsResolverConfig{Resolver: host.backend, TTL: time.Hour}})
			require.NoError(t, err)
			ctx := auth.WithResolvedPermissionsCache(context.Background())
			_, err = resolver.ResolvePermissions(ctx)
			require.NoError(t, err)
			before := host.backendCalls.Load()
			fail = true
			permissions, err := resolver.ResolvePermissions(ctx)
			require.ErrorIs(t, err, tc.want)
			require.Nil(t, permissions)
			require.Equal(t, before, host.backendCalls.Load())
		})
	}
}

func TestScopedPermissionsConfigurationAndDefensiveSnapshot(t *testing.T) {
	host := newScopedPermissionHost(t)
	backend := auth.PermissionResolverFunc(host.backend)
	for _, cfg := range []ScopedPermissionsResolverConfig{
		{}, {RequestResolver: host.request}, {Cache: auth.CachedPermissionsResolverConfig{Resolver: backend}},
		{RequestResolver: host.request, Cache: auth.CachedPermissionsResolverConfig{Resolver: backend, KeyFunc: auth.DefaultPermissionsCacheKeyFromContext}},
	} {
		_, err := NewScopedPermissionsResolver(cfg)
		require.ErrorIs(t, err, ErrScopedPermissionsConfig)
	}
	var nilResolver *ScopedPermissionsResolver
	_, err := nilResolver.ResolverFunc()(context.Background())
	require.ErrorIs(t, err, ErrScopedPermissionsConfig)
	group := uuid.New()
	resolver, err := NewScopedPermissionsResolver(ScopedPermissionsResolverConfig{RequestResolver: host.request, Cache: auth.CachedPermissionsResolverConfig{Resolver: func(ctx context.Context) ([]string, error) {
		first, ok := ScopedPermissionsRequestFromContext(ctx)
		require.True(t, ok)
		first.Scope.Labels["group"] = uuid.New()
		second, ok := ScopedPermissionsRequestFromContext(ctx)
		require.True(t, ok)
		require.Equal(t, group, second.Scope.Label("group"))
		return []string{"read"}, nil
	}}})
	require.NoError(t, err)
	_, err = resolver.ResolvePermissions(scopedContext(context.Background(), types.ScopeFilter{}.WithLabel("group", group)))
	require.NoError(t, err)
}

func TestScopedPermissionsPreservesUpstreamForcedRefresh(t *testing.T) {
	host := newScopedPermissionHost(t)
	group := uuid.New()
	host.permissions[group] = []string{"users:write"}
	resolver := host.resolver(t, time.Hour)
	ctx := scopedContext(auth.WithResolvedPermissionsCache(context.Background()), types.ScopeFilter{}.WithLabel("group", group))
	permissions, err := resolver.ResolvePermissions(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"users:write"}, permissions)
	host.mu.Lock()
	host.permissions[group] = []string{"users:read"}
	host.mu.Unlock()
	// Model the go-auth freshness guard demanding a backend read at this version.
	request, err := host.request(ctx)
	require.NoError(t, err)
	request.Current.ForceRefresh = true
	refreshCtx, err := auth.WithCurrentPermissionsRequest(ctx, request.Current)
	require.NoError(t, err)
	permissions, err = resolver.ResolvePermissions(refreshCtx)
	require.NoError(t, err)
	require.Equal(t, []string{"users:read"}, permissions)
	require.Equal(t, int64(2), host.backendCalls.Load())
}
