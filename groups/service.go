package groups

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/goliatone/go-users/activity"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/registry"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
)

// Config is opt-in. CatalogScope addresses shared definitions, never assignment
// scope. Actions explicitly declares additional resource permission names.
type Config struct {
	DB           *bun.DB
	Resolver     types.ScopeResolver
	Policy       types.AuthorizationPolicy
	CatalogScope types.ScopeFilter
	Actions      []types.PolicyAction
	// RecordPolicy defaults to dropping metadata/IP, preserving identity and scope.
	RecordPolicy *activity.RecordPolicy
}

type Service struct {
	db       *bun.DB
	resolver types.ScopeResolver
	policy   types.AuthorizationPolicy
	catalog  types.ScopeFilter
	actions  []types.PolicyAction
	records  *activity.RecordPolicy
	roles    *registry.RoleRegistry
}

// NewStrict rejects absent/typed nil dependencies. No permissive constructor is
// provided for native Groups; legacy scope/service constructors remain unchanged.
func NewStrict(cfg Config) (*Service, error) {
	if cfg.DB == nil || len(cfg.CatalogScope.Labels) > 0 {
		return nil, ErrInvalid
	}
	if _, err := scope.NewStrictGuard(cfg.Resolver, cfg.Policy); err != nil {
		return nil, err
	}
	if _, ok := cfg.Policy.(types.AllowAllAuthorizationPolicy); ok {
		return nil, ErrInvalid
	}
	if _, ok := cfg.Policy.(*types.AllowAllAuthorizationPolicy); ok {
		return nil, ErrInvalid
	}
	actions := []types.PolicyAction{ActionRead, ActionManage, ActionMembers, ActionRoles}
	for _, a := range cfg.Actions {
		if strings.TrimSpace(string(a)) == "" || string(a) != strings.TrimSpace(string(a)) || a == ActionCreate {
			return nil, ErrInvalid
		}
		actions = append(actions, a)
	}
	r, err := registry.NewRoleRegistry(registry.RoleRegistryConfig{DB: cfg.DB})
	if err != nil {
		return nil, err
	}
	return &Service{db: cfg.DB, resolver: cfg.Resolver, policy: cfg.Policy, catalog: cfg.CatalogScope.Clone(), actions: slices.Clone(actions), records: cfg.RecordPolicy, roles: r}, nil
}

// Guard provides a built-in strict guard usable by existing commands/queries.
// Enforce checks current state, but only WithAuthorized orders the protected
// database work with concurrent Group revocations.
func (s *Service) Guard() (*scope.StrictGuard, error) {
	if !s.ready() {
		return nil, ErrInvalid
	}
	return scope.NewStrictGuard(types.ScopeResolverFunc(s.resolve), types.AuthorizationPolicyFunc(func(ctx context.Context, c types.PolicyCheck) error {
		return s.authorized(ctx, c.Actor, c.Scope, c.Action, c.TargetID, nil)
	}))
}

func sameScope(a, b types.ScopeFilter) bool {
	a, ak, ae := types.CanonicalScope(a)
	b, bk, be := types.CanonicalScope(b)
	return ae == nil && be == nil && a.TenantID == b.TenantID && a.OrgID == b.OrgID && ak == bk
}

func (s *Service) resolve(ctx context.Context, actor types.ActorRef, requested types.ScopeFilter) (types.ScopeFilter, error) {
	if !s.ready() || actor.ID == uuid.Nil {
		return types.ScopeFilter{}, ErrDenied
	}
	normalized, _, err := types.CanonicalScope(requested)
	if err != nil {
		return types.ScopeFilter{}, err
	}
	if normalized.TenantID != uuid.Nil || normalized.OrgID != uuid.Nil || len(normalized.Labels) != 1 || normalized.Label("group") == uuid.Nil {
		return types.ScopeFilter{}, ErrDenied
	}
	resolved, err := s.resolver.ResolveScope(ctx, actor, normalized.Clone())
	if err != nil {
		return types.ScopeFilter{}, err
	}
	if !sameScope(normalized, resolved) {
		return types.ScopeFilter{}, ErrDenied
	}
	return normalized, nil
}

// locked runs every Group mutation/decision through the same database lock.
// SQLite obtains a writer lock before reads. PostgreSQL locks a Group row.
func (s *Service) locked(ctx context.Context, id uuid.UUID, fn func(context.Context, bun.Tx, *Group) error) error {
	if !s.ready() || id == uuid.Nil {
		return ErrInvalid
	}
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if s.db.Dialect().Name() != dialect.PG {
			if _, err := tx.NewUpdate().Model((*Group)(nil)).Set("updated_at = updated_at").Where("id = ?", id).Exec(ctx); err != nil {
				return err
			}
		}
		g := new(Group)
		q := tx.NewSelect().Model(g).Where("id = ?", id)
		if s.db.Dialect().Name() == dialect.PG {
			q = q.For("UPDATE")
		}
		if err := q.Scan(ctx); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrDenied
			}
			return err
		}
		return fn(ctx, tx, g)
	})
}

func (s *Service) check(ctx context.Context, tx bun.Tx, g *Group, actor types.ActorRef, requested types.ScopeFilter, action types.PolicyAction, target uuid.UUID) error {
	if !g.Active || actor.ID == uuid.Nil || !slices.Contains(s.actions, action) {
		return ErrDenied
	}
	var member Membership
	if err := tx.NewSelect().Model(&member).Where("group_id = ? AND principal_id = ? AND active = ?", g.ID, actor.ID, true).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrDenied
		}
		return err
	}
	r := s.roles.BindTx(tx)
	grants, err := r.ListAssignments(ctx, types.RoleAssignmentFilter{UserID: actor.ID, Scope: requested})
	if err != nil {
		return err
	}
	permitted := false
	for _, grant := range grants {
		role, err := r.GetRole(ctx, grant.RoleID, s.catalog)
		if err != nil {
			return err
		}
		if slices.Contains(role.Permissions, string(action)) {
			permitted = true
			break
		}
	}
	if !permitted {
		return ErrDenied
	}
	return s.policy.Authorize(ctx, types.PolicyCheck{Actor: actor, Scope: requested.Clone(), Action: action, TargetID: target})
}

func (s *Service) authorized(ctx context.Context, actor types.ActorRef, requested types.ScopeFilter, action types.PolicyAction, target uuid.UUID, fn func(context.Context, bun.Tx) error) error {
	resolved, err := s.resolve(ctx, actor, requested)
	if err != nil {
		return err
	}
	if !slices.Contains(s.actions, action) {
		return ErrDenied
	}
	return s.locked(ctx, resolved.Label("group"), func(ctx context.Context, tx bun.Tx, g *Group) error {
		if err := s.check(ctx, tx, g, actor, resolved, action, target); err != nil {
			return err
		}
		if fn != nil {
			return fn(ctx, tx)
		}
		return nil
	})
}

// WithAuthorized keeps authorization and protected database work in the same
// transaction/Group lock as member removal, archive and role revocation. Use tx
// for all callback writes; do not call this service recursively in a callback.
func (s *Service) WithAuthorized(ctx context.Context, actor types.ActorRef, requested types.ScopeFilter, action types.PolicyAction, target uuid.UUID, fn func(context.Context, bun.Tx) error) error {
	if fn == nil {
		return ErrInvalid
	}
	return s.authorized(ctx, actor, requested, action, target, fn)
}

// Create requires explicit bootstrap policy authority and creates no memberships
// or grants. Initial authorities can be provisioned through trusted SQL/registry
// in a deployment transaction; routine mutations use this guarded service.
func (s *Service) Create(ctx context.Context, actor types.ActorRef, name string) (*Group, error) {
	if !s.ready() || actor.ID == uuid.Nil || strings.TrimSpace(name) == "" {
		return nil, ErrInvalid
	}
	g := &Group{ID: uuid.New(), Name: strings.TrimSpace(name), Active: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	requested := Scope(g.ID)
	if err := s.policy.Authorize(ctx, types.PolicyCheck{Actor: actor, Scope: requested, Action: ActionCreate, TargetID: g.ID}); err != nil {
		return nil, err
	}
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(g).Exec(ctx); err != nil {
			return err
		}
		return s.audit(ctx, tx, actor, g.ID, "group.created", uuid.Nil, uuid.Nil)
	})
	if err != nil {
		return nil, err
	}
	return g, nil
}

func (s *Service) SetActive(ctx context.Context, actor types.ActorRef, id uuid.UUID, active bool) error {
	// Reactivation must still be approved by a live manager grant/member. The
	// inactive Group is checked with a temporary active view solely for this action.
	requested, err := s.resolve(ctx, actor, Scope(id))
	if err != nil {
		return err
	}
	return s.locked(ctx, id, func(ctx context.Context, tx bun.Tx, g *Group) error {
		view := *g
		view.Active = true
		if err := s.check(ctx, tx, &view, actor, requested, ActionManage, id); err != nil {
			return err
		}
		if _, err := tx.NewUpdate().Model((*Group)(nil)).Set("active = ?", active).Set("updated_at = ?", time.Now().UTC()).Where("id = ?", id).Exec(ctx); err != nil {
			return err
		}
		return s.audit(ctx, tx, actor, id, "group.state.changed", uuid.Nil, uuid.Nil)
	})
}

func (s *Service) SetMember(ctx context.Context, actor types.ActorRef, id, principal uuid.UUID, active bool) error {
	if principal == uuid.Nil {
		return ErrInvalid
	}
	return s.authorized(ctx, actor, Scope(id), ActionMembers, principal, func(ctx context.Context, tx bun.Tx) error {
		now := time.Now().UTC()
		m := &Membership{GroupID: id, PrincipalID: principal, Active: active, CreatedAt: now, UpdatedAt: now}
		if _, err := tx.NewInsert().Model(m).On("CONFLICT (group_id, principal_id) DO UPDATE").Set("active = EXCLUDED.active").Set("updated_at = EXCLUDED.updated_at").Exec(ctx); err != nil {
			return err
		}
		// Removal destroys grants in this exact Group scope. Rejoin never recreates them.
		if !active {
			grants, err := s.roles.BindTx(tx).ListAssignments(ctx, types.RoleAssignmentFilter{UserID: principal, Scope: Scope(id)})
			if err != nil {
				return err
			}
			for _, grant := range grants {
				if err := s.roles.BindTx(tx).UnassignRole(ctx, principal, grant.RoleID, grant.Scope, actor.ID); err != nil {
					return err
				}
				if err := s.audit(ctx, tx, actor, id, "role.unassigned", principal, grant.RoleID); err != nil {
					return err
				}
			}
		}
		verb := "group.member.added"
		if !active {
			verb = "group.member.removed"
		}
		return s.audit(ctx, tx, actor, id, verb, principal, uuid.Nil)
	})
}

// SetRole reuses an existing catalog role. Membership is necessary but never
// sufficient for access; callers cannot delegate permissions they do not hold.
func (s *Service) SetRole(ctx context.Context, actor types.ActorRef, id, principal, roleID uuid.UUID, assign bool) error {
	if principal == uuid.Nil || roleID == uuid.Nil {
		return ErrInvalid
	}
	return s.authorized(ctx, actor, Scope(id), ActionRoles, principal, func(ctx context.Context, tx bun.Tx) error {
		r := s.roles.BindTx(tx)
		if !assign {
			if err := r.UnassignRole(ctx, principal, roleID, Scope(id), actor.ID); err != nil {
				return err
			}
			return s.audit(ctx, tx, actor, id, "role.unassigned", principal, roleID)
		}
		var member Membership
		if err := tx.NewSelect().Model(&member).Where("group_id = ? AND principal_id = ? AND active = ?", id, principal, true).Scan(ctx); err != nil {
			return ErrDenied
		}
		role, err := r.GetRole(ctx, roleID, s.catalog)
		if err != nil {
			return err
		}
		// Require each proposed permission under the same live lock and host policy.
		for _, permission := range role.Permissions {
			if err := s.check(ctx, tx, &Group{ID: id, Active: true}, actor, Scope(id), types.PolicyAction(permission), principal); err != nil {
				return err
			}
		}
		if err := r.AssignRoleFromCatalog(ctx, types.ScopedRoleGrant{UserID: principal, RoleID: roleID, CatalogScope: s.catalog, Scope: Scope(id), ActorID: actor.ID}); err != nil {
			return err
		}
		return s.audit(ctx, tx, actor, id, "role.assigned", principal, roleID)
	})
}

// Discover lists only active Groups in which the principal has active membership
// and an effective groups:read grant. Native state is evaluated on every call.
func (s *Service) Discover(ctx context.Context, actor types.ActorRef) ([]Group, error) {
	if !s.ready() || actor.ID == uuid.Nil {
		return nil, ErrDenied
	}
	var candidates []Group
	if err := s.db.NewSelect().Model(&candidates).Where("active = ?", true).Where("id IN (SELECT group_id FROM user_group_memberships WHERE principal_id = ? AND active = ?)", actor.ID, true).Order("id").Scan(ctx); err != nil {
		return nil, err
	}
	out := make([]Group, 0, len(candidates))
	for _, g := range candidates {
		err := s.authorized(ctx, actor, Scope(g.ID), ActionRead, g.ID, nil)
		if errors.Is(err, ErrDenied) || errors.Is(err, types.ErrUnauthorizedScope) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

func (s *Service) audit(ctx context.Context, tx bun.Tx, actor types.ActorRef, id uuid.UUID, verb string, principal, role uuid.UUID) error {
	record := types.ActivityRecord{ID: uuid.New(), ActorID: actor.ID, UserID: principal, Labels: Scope(id).Labels, Verb: verb, ObjectType: "group", ObjectID: id.String(), Channel: "groups", OccurredAt: time.Now().UTC()}
	if role != uuid.Nil {
		record.ObjectType = "role"
		record.ObjectID = role.String()
	}
	record = s.records.Sanitize(record)
	entry := activity.FromActivityRecord(record)
	_, err := tx.NewInsert().Model(entry).Exec(ctx)
	return err
}

func (s *Service) ready() bool {
	return s != nil && s.db != nil && s.roles != nil && s.resolver != nil && s.policy != nil
}
