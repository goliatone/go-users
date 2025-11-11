package main

import (
	"context"
	"sync"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

const workspaceScopeLabel = "workspace"

// workspaceDirectory maintains tenant/workspace assignments per actor so the
// shared scope guard can enforce multi-tenant boundaries.
type workspaceDirectory struct {
	mu               sync.RWMutex
	tenants          map[uuid.UUID]uuid.UUID
	workspaces       map[uuid.UUID]uuid.UUID
	defaultTenant    uuid.UUID
	defaultWorkspace uuid.UUID
}

func newWorkspaceDirectory(defaultTenant, defaultWorkspace uuid.UUID) *workspaceDirectory {
	return &workspaceDirectory{
		tenants:          make(map[uuid.UUID]uuid.UUID),
		workspaces:       make(map[uuid.UUID]uuid.UUID),
		defaultTenant:    defaultTenant,
		defaultWorkspace: defaultWorkspace,
	}
}

func (d *workspaceDirectory) bind(actorID, tenantID, workspaceID uuid.UUID) {
	if actorID == uuid.Nil {
		return
	}
	if tenantID == uuid.Nil {
		tenantID = d.defaultTenant
	}
	if workspaceID == uuid.Nil {
		workspaceID = d.defaultWorkspace
	}
	d.mu.Lock()
	d.tenants[actorID] = tenantID
	d.workspaces[actorID] = workspaceID
	d.mu.Unlock()
}

func (d *workspaceDirectory) scopeFor(actorID uuid.UUID) types.ScopeFilter {
	d.mu.RLock()
	tenant := d.tenants[actorID]
	workspace := d.workspaces[actorID]
	d.mu.RUnlock()

	if tenant == uuid.Nil {
		tenant = d.defaultTenant
	}
	if workspace == uuid.Nil {
		workspace = d.defaultWorkspace
	}

	scope := types.ScopeFilter{
		TenantID: tenant,
		OrgID:    workspace,
	}
	return scope.WithLabel(workspaceScopeLabel, workspace)
}

func (d *workspaceDirectory) Resolver() types.ScopeResolver {
	return types.ScopeResolverFunc(func(_ context.Context, actor types.ActorRef, requested types.ScopeFilter) (types.ScopeFilter, error) {
		scope := requested.Clone()
		if actor.ID == uuid.Nil {
			return scope, nil
		}
		assigned := d.scopeFor(actor.ID)
		if scope.TenantID == uuid.Nil {
			scope.TenantID = assigned.TenantID
		}
		if scope.OrgID == uuid.Nil {
			scope.OrgID = assigned.OrgID
		}
		if scope.Label(workspaceScopeLabel) == uuid.Nil {
			scope = scope.WithLabel(workspaceScopeLabel, assigned.Label(workspaceScopeLabel))
		}
		return scope, nil
	})
}

func (d *workspaceDirectory) Policy() types.AuthorizationPolicy {
	return types.AuthorizationPolicyFunc(func(_ context.Context, check types.PolicyCheck) error {
		if check.Actor.ID == uuid.Nil {
			return nil
		}
		expected := d.scopeFor(check.Actor.ID)
		if expected.TenantID != uuid.Nil && check.Scope.TenantID != uuid.Nil && check.Scope.TenantID != expected.TenantID {
			return types.ErrUnauthorizedScope
		}
		label := check.Scope.Label(workspaceScopeLabel)
		expectedLabel := expected.Label(workspaceScopeLabel)
		if expectedLabel != uuid.Nil && label != uuid.Nil && label != expectedLabel {
			return types.ErrUnauthorizedScope
		}
		return nil
	})
}

func (d *workspaceDirectory) Bind(actorID, tenantID, workspaceID uuid.UUID) {
	d.bind(actorID, tenantID, workspaceID)
}

func (d *workspaceDirectory) Ensure(actorID, tenantID, workspaceID uuid.UUID) {
	d.mu.RLock()
	_, ok := d.tenants[actorID]
	d.mu.RUnlock()
	if ok {
		return
	}
	d.bind(actorID, tenantID, workspaceID)
}

func (d *workspaceDirectory) Scope(actorID uuid.UUID) types.ScopeFilter {
	return d.scopeFor(actorID)
}
