package registry

import (
	"context"
	"errors"
	"strings"

	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

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
	db          *bun.DB
	roles       repository.Repository[*CustomRole]
	assignments repository.Repository[*RoleAssignment]
	clock       types.Clock
	hooks       types.Hooks
	logger      types.Logger
	idGen       types.IDGenerator
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

	rolesRepo := cfg.Roles
	assignRepo := cfg.Assignments

	if rolesRepo == nil || assignRepo == nil {
		if cfg.DB == nil {
			return nil, errors.New("bun role registry: db or repositories must be provided")
		}
		if rolesRepo == nil {
			rolesRepo = repository.NewRepository(cfg.DB, repository.ModelHandlers[*CustomRole]{
				NewRecord: func() *CustomRole { return &CustomRole{} },
				GetID: func(role *CustomRole) uuid.UUID {
					if role == nil {
						return uuid.Nil
					}
					return role.ID
				},
				SetID: func(role *CustomRole, id uuid.UUID) {
					if role != nil {
						role.ID = id
					}
				},
			})
		}
		if assignRepo == nil {
			assignRepo = repository.NewRepository(cfg.DB, repository.ModelHandlers[*RoleAssignment]{
				NewRecord: func() *RoleAssignment { return &RoleAssignment{} },
				GetID: func(*RoleAssignment) uuid.UUID {
					return uuid.Nil
				},
				SetID: func(*RoleAssignment, uuid.UUID) {},
			})
		}
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

// CreateRole inserts a custom role scoped to the provided tenant/org.
func (r *RoleRegistry) CreateRole(ctx context.Context, input types.RoleMutation) (*types.RoleDefinition, error) {
	name := normalizeRoleName(input.Name)
	if name == "" {
		return nil, errors.New("role name required")
	}
	now := r.clock.Now()
	role := &CustomRole{
		ID:          r.idGen.UUID(),
		Name:        name,
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
	role, err := r.roles.GetByID(ctx, id.String(), scopeSelectCriteria(input.Scope))
	if err != nil {
		return nil, err
	}
	if name := normalizeRoleName(input.Name); name != "" {
		role.Name = name
	}
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

// AssignRole creates a user->role assignment scoped to tenant/org.
func (r *RoleRegistry) AssignRole(ctx context.Context, userID, roleID uuid.UUID, scope types.ScopeFilter, actor uuid.UUID) error {
	_, err := r.roles.GetByID(ctx, roleID.String(), scopeSelectCriteria(scope))
	if err != nil {
		return err
	}
	assignment := &RoleAssignment{
		UserID:     userID,
		RoleID:     roleID,
		TenantID:   scopeUUID(scope.TenantID),
		OrgID:      scopeUUID(scope.OrgID),
		AssignedAt: r.clock.Now(),
		AssignedBy: actor,
	}
	_, err = r.assignments.Create(ctx, assignment)
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
	err := r.assignments.DeleteWhere(ctx,
		func(q *bun.DeleteQuery) *bun.DeleteQuery {
			return q.Where("user_id = ? AND role_id = ? AND tenant_id = ? AND org_id = ?",
				userID, roleID, scopeUUID(scope.TenantID), scopeUUID(scope.OrgID))
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
	pagination := normalizePagination(filter.Pagination, 50, 200)
	criteria := []repository.SelectCriteria{
		scopeSelectCriteria(filter.Scope),
		func(q *bun.SelectQuery) *bun.SelectQuery {
			q = q.OrderExpr("LOWER(name) ASC").
				Limit(pagination.Limit).
				Offset(pagination.Offset)
			if len(filter.RoleIDs) > 0 {
				q = q.Where("id IN (?)", bun.In(filter.RoleIDs))
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
	role, err := r.roles.GetByID(ctx, id.String(), scopeSelectCriteria(scope))
	if err != nil {
		return nil, err
	}
	return toRoleDefinition(role), nil
}

// ListAssignments returns assignments filtered by scope/user/role.
func (r *RoleRegistry) ListAssignments(ctx context.Context, filter types.RoleAssignmentFilter) ([]types.RoleAssignment, error) {
	criteria := []repository.SelectCriteria{
		func(q *bun.SelectQuery) *bun.SelectQuery {
			q = q.Where("tenant_id = ? AND org_id = ?", scopeUUID(filter.Scope.TenantID), scopeUUID(filter.Scope.OrgID))
			if filter.UserID != uuid.Nil {
				q = q.Where("user_id = ?", filter.UserID)
			}
			if filter.RoleID != uuid.Nil {
				q = q.Where("role_id = ?", filter.RoleID)
			}
			if len(filter.UserIDs) > 0 {
				q = q.Where("user_id IN (?)", bun.In(filter.UserIDs))
			}
			if len(filter.RoleIDs) > 0 {
				q = q.Where("role_id IN (?)", bun.In(filter.RoleIDs))
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
			},
			AssignedAt: record.AssignedAt,
			AssignedBy: record.AssignedBy,
		})
	}
	return assignments, nil
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
		return q.Where("id IN (?)", bun.In(roleIDs))
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
	for key, value := range values {
		out[key] = value
	}
	return out
}

func toRoleDefinition(record *CustomRole) *types.RoleDefinition {
	if record == nil {
		return nil
	}
	return &types.RoleDefinition{
		ID:          record.ID,
		Name:        record.Name,
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
