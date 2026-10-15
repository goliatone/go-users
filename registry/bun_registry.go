package registry

import (
	"context"
	"errors"
	"maps"
	"strings"

	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// ErrUnsupportedRoleScope indicates that a custom label was supplied to a role
// catalog operation. Definitions use tenant/org namespaces; labels belong on
// assignments to those definitions.
var ErrUnsupportedRoleScope = errors.New("bun role registry: role definitions do not support scope labels")

// RoleRegistryConfig configures the Bun-backed role registry.
type RoleRegistryConfig struct {
	DB          *bun.DB
	Roles       repository.Repository[*CustomRole]
	Assignments repository.Repository[*RoleAssignment]
	Clock       types.Clock
	Hooks       types.Hooks
	Logger      types.Logger
	IDGenerator types.IDGenerator
}

// RoleRegistry persists custom roles and assignments using Bun repositories.
type RoleRegistry struct {
	transactional bool
	db            *bun.DB
	roles         repository.Repository[*CustomRole]
	assignments   repository.Repository[*RoleAssignment]
	clock         types.Clock
	hooks         types.Hooks
	logger        types.Logger
	idGen         types.IDGenerator
}

// NewRoleRegistry constructs the default registry. Either DB or both repositories
// must be provided; when DB is supplied the repositories are created automatically.
func NewRoleRegistry(cfg RoleRegistryConfig) (*RoleRegistry, error) {
	clock := cfg.Clock
	if clock == nil {
		clock = types.SystemClock{}
	}
	logger := cfg.Logger
	if logger == nil {
		logger = types.NopLogger{}
	}
	idGen := cfg.IDGenerator
	if idGen == nil {
		idGen = types.UUIDGenerator{}
	}

	rolesRepo, assignRepo, err := resolveRoleRepositories(cfg)
	if err != nil {
		return nil, err
	}

	return &RoleRegistry{
		db:          cfg.DB,
		roles:       rolesRepo,
		assignments: assignRepo,
		clock:       clock,
		hooks:       cfg.Hooks,
		logger:      logger,
		idGen:       idGen,
	}, nil
}

func resolveRoleRepositories(cfg RoleRegistryConfig) (repository.Repository[*CustomRole], repository.Repository[*RoleAssignment], error) {
	rolesRepo := cfg.Roles
	assignRepo := cfg.Assignments
	if rolesRepo != nil && assignRepo != nil {
		return rolesRepo, assignRepo, nil
	}
	if cfg.DB == nil {
		return nil, nil, errors.New("bun role registry: db or repositories must be provided")
	}
	if rolesRepo == nil {
		rolesRepo = repository.NewRepository(cfg.DB, repository.ModelHandlers[*CustomRole]{
			NewRecord: func() *CustomRole { return &CustomRole{} },
			GetID:     customRoleID,
			SetID:     setCustomRoleID,
		})
	}
	if assignRepo == nil {
		assignRepo = repository.NewRepository(cfg.DB, repository.ModelHandlers[*RoleAssignment]{
			NewRecord: func() *RoleAssignment { return &RoleAssignment{} },
			GetID:     func(*RoleAssignment) uuid.UUID { return uuid.Nil },
			SetID:     func(*RoleAssignment, uuid.UUID) {},
		})
	}
	return rolesRepo, assignRepo, nil
}

func customRoleID(role *CustomRole) uuid.UUID {
	if role == nil {
		return uuid.Nil
	}
	return role.ID
}

func setCustomRoleID(role *CustomRole, id uuid.UUID) {
	if role != nil {
		role.ID = id
	}
}

// CreateRole inserts a custom role scoped to the provided tenant/org.
func (r *RoleRegistry) CreateRole(ctx context.Context, input types.RoleMutation) (*types.RoleDefinition, error) {
	if len(input.Scope.Labels) > 0 {
		return nil, ErrUnsupportedRoleScope
	}
	name := normalizeRoleName(input.Name)
	if name == "" {
		return nil, errors.New("role name required")
	}
	now := r.clock.Now()
	role := &CustomRole{
		ID:          r.idGen.UUID(),
		Name:        name,
		Order:       input.Order,
		Description: strings.TrimSpace(input.Description),
		RoleKey:     strings.TrimSpace(input.RoleKey),
		Permissions: copyPermissions(input.Permissions),
		Metadata:    copyMetadata(input.Metadata),
		IsSystem:    input.IsSystem,
		TenantID:    scopeUUID(input.Scope.TenantID),
		OrgID:       scopeUUID(input.Scope.OrgID),
		CreatedAt:   now,
		UpdatedAt:   now,
		CreatedBy:   input.ActorID,
		UpdatedBy:   input.ActorID,
	}
	created, err := r.roles.Create(ctx, role)
	if err != nil {
		return nil, err
	}
	def := toRoleDefinition(created)
	r.emitRoleEvent(ctx, types.RoleEvent{
		RoleID:     def.ID,
		Action:     "role.created",
		ActorID:    input.ActorID,
		Scope:      def.Scope,
		OccurredAt: now,
		Role:       *def,
	})
	return def, nil
}

// UpdateRole updates mutable fields on a custom role.
func (r *RoleRegistry) UpdateRole(ctx context.Context, id uuid.UUID, input types.RoleMutation) (*types.RoleDefinition, error) {
	if len(input.Scope.Labels) > 0 {
		return nil, ErrUnsupportedRoleScope
	}
	role, err := r.roles.GetByID(ctx, id.String(), scopeSelectCriteria(input.Scope))
	if err != nil {
		return nil, err
	}
	if name := normalizeRoleName(input.Name); name != "" {
		role.Name = name
	}
	role.Order = input.Order
	role.Description = strings.TrimSpace(input.Description)
	role.RoleKey = strings.TrimSpace(input.RoleKey)
	if input.Permissions != nil {
		role.Permissions = copyPermissions(input.Permissions)
	}
	if input.Metadata != nil {
		role.Metadata = copyMetadata(input.Metadata)
	}
	if input.IsSystem {
		role.IsSystem = true
	}
	role.UpdatedAt = r.clock.Now()
	role.UpdatedBy = input.ActorID

	updated, err := r.roles.Update(ctx, role)
	if err != nil {
		return nil, err
	}
	def := toRoleDefinition(updated)
	r.emitRoleEvent(ctx, types.RoleEvent{
		RoleID:     def.ID,
		Action:     "role.updated",
		ActorID:    input.ActorID,
		Scope:      def.Scope,
		OccurredAt: role.UpdatedAt,
		Role:       *def,
	})
	return def, nil
}

// DeleteRole removes a custom role (unless marked as system).
func (r *RoleRegistry) DeleteRole(ctx context.Context, id uuid.UUID, scope types.ScopeFilter, actor uuid.UUID) error {
	if len(scope.Labels) > 0 {
		return ErrUnsupportedRoleScope
	}
	role, err := r.roles.GetByID(ctx, id.String(), scopeSelectCriteria(scope))
	if err != nil {
		return err
	}
	if role.IsSystem {
		return errors.New("cannot delete system roles")
	}
	if err := r.roles.Delete(ctx, role); err != nil {
		return err
	}
	r.emitRoleEvent(ctx, types.RoleEvent{
		RoleID:     role.ID,
		Action:     "role.deleted",
		ActorID:    actor,
		Scope:      scopeFromRecord(role),
		OccurredAt: r.clock.Now(),
		Role:       *toRoleDefinition(role),
	})
	return nil
}

// AssignRole creates a user->role assignment scoped to tenant/org and exact labels.
func (r *RoleRegistry) AssignRole(ctx context.Context, userID, roleID uuid.UUID, scope types.ScopeFilter, actor uuid.UUID) error {
	return r.AssignRoleFromCatalog(ctx, types.ScopedRoleGrant{UserID: userID, RoleID: roleID, Scope: scope, CatalogScope: types.ScopeFilter{TenantID: scope.TenantID, OrgID: scope.OrgID}, ActorID: actor})
}

var _ types.ExactScopeRoleRegistry = (*RoleRegistry)(nil)

// AssignRoleFromCatalog reuses a catalog definition in an independent exact scope.
func (r *RoleRegistry) AssignRoleFromCatalog(ctx context.Context, grant types.ScopedRoleGrant) error {
	if len(grant.CatalogScope.Labels) > 0 {
		return ErrUnsupportedRoleScope
	}
	userID, roleID, scope, actor := grant.UserID, grant.RoleID, grant.Scope, grant.ActorID

	scope, key, err := assignmentScope(scope)
	if err != nil {
		return err
	}
	_, err = r.roles.GetByID(ctx, roleID.String(), scopeSelectCriteria(grant.CatalogScope))
	if err != nil {
		return err
	}
	assignment := &RoleAssignment{
		UserID:     userID,
		RoleID:     roleID,
		TenantID:   scopeUUID(scope.TenantID),
		OrgID:      scopeUUID(scope.OrgID),
		ScopeKey:   key,
		Labels:     scope.Labels,
		AssignedAt: r.clock.Now(),
		AssignedBy: actor,
	}
	var criteria []repository.InsertCriteria
	if r.transactional {
		// A duplicate INSERT must not abort the surrounding PostgreSQL transaction.
		criteria = append(criteria, func(q *bun.InsertQuery) *bun.InsertQuery {
			return q.On("CONFLICT (user_id, role_id, tenant_id, org_id, scope_key) DO NOTHING")
		})
	}
	_, err = r.assignments.Create(ctx, assignment, criteria...)
	if err != nil {
		if repository.IsDuplicatedKey(err) {
			return nil
		}
		return err
	}
	r.emitRoleEvent(ctx, types.RoleEvent{
		RoleID:     roleID,
		UserID:     userID,
		Action:     "role.assigned",
		ActorID:    actor,
		Scope:      scope,
		OccurredAt: assignment.AssignedAt,
	})
	return nil
}

// UnassignRole removes an existing user->role assignment.
func (r *RoleRegistry) UnassignRole(ctx context.Context, userID, roleID uuid.UUID, scope types.ScopeFilter, actor uuid.UUID) error {
	scope, key, err := assignmentScope(scope)
	if err != nil {
		return err
	}
	err = r.assignments.DeleteWhere(ctx,
		func(q *bun.DeleteQuery) *bun.DeleteQuery {
			return q.Where("user_id = ? AND role_id = ? AND tenant_id = ? AND org_id = ? AND scope_key = ?",
				userID, roleID, scopeUUID(scope.TenantID), scopeUUID(scope.OrgID), key)
		},
	)
	if err != nil {
		return err
	}
	r.emitRoleEvent(ctx, types.RoleEvent{
		RoleID:     roleID,
		UserID:     userID,
		Action:     "role.unassigned",
		ActorID:    actor,
		Scope:      scope,
		OccurredAt: r.clock.Now(),
	})
	return nil
}

// ListRoles returns paginated roles filtered by scope/keyword.
func (r *RoleRegistry) ListRoles(ctx context.Context, filter types.RoleFilter) (types.RolePage, error) {
	if len(filter.Scope.Labels) > 0 {
		return types.RolePage{}, ErrUnsupportedRoleScope
	}
	pagination := normalizePagination(filter.Pagination, 50, 200)
	criteria := []repository.SelectCriteria{
		scopeSelectCriteria(filter.Scope),
		func(q *bun.SelectQuery) *bun.SelectQuery {
			q = q.OrderExpr("? ASC, LOWER(name) ASC", bun.Ident("order")).
				Limit(pagination.Limit).
				Offset(pagination.Offset)

			if len(filter.RoleIDs) > 0 {
				q = q.Where("id IN (?)", bun.List(filter.RoleIDs))
			}
			if filter.Keyword != "" {
				keyword := "%" + strings.ToLower(strings.TrimSpace(filter.Keyword)) + "%"
				q = q.Where("LOWER(name) LIKE ? OR LOWER(description) LIKE ?", keyword, keyword)
			}
			if filter.RoleKey != "" {
				q = q.Where("role_key = ?", strings.TrimSpace(filter.RoleKey))
			}
			if !filter.IncludeSystem {
				q = q.Where("is_system = FALSE")
			}
			return q
		},
	}

	records, total, err := r.roles.List(ctx, criteria...)
	if err != nil {
		return types.RolePage{}, err
	}
	defs := make([]types.RoleDefinition, 0, len(records))
	for _, record := range records {
		defs = append(defs, *toRoleDefinition(record))
	}
	return types.RolePage{
		Roles:      defs,
		Total:      total,
		NextOffset: pagination.Offset + pagination.Limit,
		HasMore:    pagination.Offset+pagination.Limit < total,
	}, nil
}

// GetRole returns a single role matching the scope constraints.
func (r *RoleRegistry) GetRole(ctx context.Context, id uuid.UUID, scope types.ScopeFilter) (*types.RoleDefinition, error) {
	if len(scope.Labels) > 0 {
		return nil, ErrUnsupportedRoleScope
	}
	role, err := r.roles.GetByID(ctx, id.String(), scopeSelectCriteria(scope))
	if err != nil {
		return nil, err
	}
	return toRoleDefinition(role), nil
}

// ListAssignments returns assignments filtered by scope/user/role.
func (r *RoleRegistry) ListAssignments(ctx context.Context, filter types.RoleAssignmentFilter) ([]types.RoleAssignment, error) {
	scope, key, err := assignmentScope(filter.Scope)
	if err != nil {
		return nil, err
	}
	criteria := []repository.SelectCriteria{
		func(q *bun.SelectQuery) *bun.SelectQuery {
			q = q.Where("tenant_id = ? AND org_id = ? AND scope_key = ?", scopeUUID(scope.TenantID), scopeUUID(scope.OrgID), key)
			if filter.UserID != uuid.Nil {
				q = q.Where("user_id = ?", filter.UserID)
			}
			if filter.RoleID != uuid.Nil {
				q = q.Where("role_id = ?", filter.RoleID)
			}
			if len(filter.UserIDs) > 0 {
				q = q.Where("user_id IN (?)", bun.List(filter.UserIDs))
			}
			if len(filter.RoleIDs) > 0 {
				q = q.Where("role_id IN (?)", bun.List(filter.RoleIDs))
			}
			return q
		},
	}
	records, _, err := r.assignments.List(ctx, criteria...)
	if err != nil {
		return nil, err
	}
	roleNames, err := r.loadRoleNames(ctx, records)
	if err != nil {
		return nil, err
	}
	assignments := make([]types.RoleAssignment, 0, len(records))
	for _, record := range records {
		assignments = append(assignments, types.RoleAssignment{
			UserID:   record.UserID,
			RoleID:   record.RoleID,
			RoleName: roleNames[record.RoleID],
			Scope: types.ScopeFilter{
				TenantID: record.TenantID,
				OrgID:    record.OrgID,
				Labels:   maps.Clone(record.Labels),
			},
			AssignedAt: record.AssignedAt,
			AssignedBy: record.AssignedBy,
		})
	}
	return assignments, nil
}

func assignmentScope(scope types.ScopeFilter) (types.ScopeFilter, string, error) {
	return types.CanonicalScope(scope)
}

func (r *RoleRegistry) loadRoleNames(ctx context.Context, assignments []*RoleAssignment) (map[uuid.UUID]string, error) {
	roleIDs := make([]uuid.UUID, 0, len(assignments))
	seen := make(map[uuid.UUID]struct{}, len(assignments))
	for _, assignment := range assignments {
		if _, ok := seen[assignment.RoleID]; ok {
			continue
		}
		seen[assignment.RoleID] = struct{}{}
		roleIDs = append(roleIDs, assignment.RoleID)
	}
	if len(roleIDs) == 0 {
		return map[uuid.UUID]string{}, nil
	}
	records, _, err := r.roles.List(ctx, func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("id IN (?)", bun.List(roleIDs))
	})
	if err != nil {
		return nil, err
	}
	names := make(map[uuid.UUID]string, len(records))
	for _, record := range records {
		names[record.ID] = record.Name
	}
	return names, nil
}

func (r *RoleRegistry) emitRoleEvent(ctx context.Context, event types.RoleEvent) {
	if r.hooks.AfterRoleChange == nil {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error("role hook panic", errors.New("panic in AfterRoleChange"), "panic", rec)
		}
	}()
	r.hooks.AfterRoleChange(ctx, event)
}

func scopeSelectCriteria(scope types.ScopeFilter) repository.SelectCriteria {
	return func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("tenant_id = ? AND org_id = ?", scopeUUID(scope.TenantID), scopeUUID(scope.OrgID))
	}
}

func scopeUUID(value uuid.UUID) uuid.UUID {
	if value == uuid.Nil {
		return uuid.Nil
	}
	return value
}

func normalizeRoleName(name string) string {
	return strings.TrimSpace(name)
}

func copyPermissions(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func copyMetadata(values map[string]any) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(values))
	maps.Copy(out, values)
	return out
}

func toRoleDefinition(record *CustomRole) *types.RoleDefinition {
	if record == nil {
		return nil
	}
	return &types.RoleDefinition{
		ID:          record.ID,
		Name:        record.Name,
		Order:       record.Order,
		Description: record.Description,
		RoleKey:     record.RoleKey,
		Permissions: append([]string{}, record.Permissions...),
		Metadata:    copyMetadata(record.Metadata),
		IsSystem:    record.IsSystem,
		Scope:       scopeFromRecord(record),
		CreatedAt:   record.CreatedAt,
		UpdatedAt:   record.UpdatedAt,
		CreatedBy:   record.CreatedBy,
		UpdatedBy:   record.UpdatedBy,
	}
}

func scopeFromRecord(record *CustomRole) types.ScopeFilter {
	if record == nil {
		return types.ScopeFilter{}
	}
	return types.ScopeFilter{
		TenantID: record.TenantID,
		OrgID:    record.OrgID,
	}
}

// CustomRoleToDefinition exposes the conversion logic for consumers that need to
// translate Bun models into domain role definitions.
func CustomRoleToDefinition(record *CustomRole) *types.RoleDefinition {
	return toRoleDefinition(record)
}

// DefinitionToCustomRole converts a domain role definition into the Bun model.
func DefinitionToCustomRole(definition *types.RoleDefinition) *CustomRole {
	if definition == nil {
		return nil
	}
	return &CustomRole{
		ID:          definition.ID,
		Name:        definition.Name,
		Order:       definition.Order,
		Description: definition.Description,
		RoleKey:     definition.RoleKey,
		Permissions: append([]string{}, definition.Permissions...),
		Metadata:    copyMetadata(definition.Metadata),
		IsSystem:    definition.IsSystem,
		TenantID:    scopeUUID(definition.Scope.TenantID),
		OrgID:       scopeUUID(definition.Scope.OrgID),
		CreatedAt:   definition.CreatedAt,
		UpdatedAt:   definition.UpdatedAt,
		CreatedBy:   definition.CreatedBy,
		UpdatedBy:   definition.UpdatedBy,
	}
}

func normalizePagination(p types.Pagination, def, max int) types.Pagination {
	if p.Limit <= 0 {
		p.Limit = def
	}
	if p.Limit > max {
		p.Limit = max
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	return p
}
