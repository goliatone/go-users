package crudsvc

import (
	"context"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-crud"
	goerrors "github.com/goliatone/go-errors"
	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-users/command"
	"github.com/goliatone/go-users/crudguard"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/query"
	"github.com/goliatone/go-users/registry"
	"github.com/google/uuid"
)

// RoleServiceConfig wires dependencies for the role controller adapter.
type RoleServiceConfig struct {
	Guard    GuardAdapter
	Create   gocommand.Commander[command.CreateRoleInput]
	Update   gocommand.Commander[command.UpdateRoleInput]
	Delete   gocommand.Commander[command.DeleteRoleInput]
	List     gocommand.Querier[types.RoleFilter, types.RolePage]
	Detail   gocommand.Querier[query.RoleDetailInput, *types.RoleDefinition]
	Registry types.RoleRegistry
}

// RoleService adapts custom role CRUD flows to the go-crud service interface.
type RoleService struct {
	guard   GuardAdapter
	create  gocommand.Commander[command.CreateRoleInput]
	update  gocommand.Commander[command.UpdateRoleInput]
	delete  gocommand.Commander[command.DeleteRoleInput]
	list    gocommand.Querier[types.RoleFilter, types.RolePage]
	detail  gocommand.Querier[query.RoleDetailInput, *types.RoleDefinition]
	emitter ActivityEmitter
	logger  types.Logger
}

// NewRoleService constructs the adapter.
func NewRoleService(cfg RoleServiceConfig, opts ...ServiceOption) *RoleService {
	options := applyOptions(opts)
	return &RoleService{
		guard:   cfg.Guard,
		create:  cfg.Create,
		update:  cfg.Update,
		delete:  cfg.Delete,
		list:    cfg.List,
		detail:  cfg.Detail,
		emitter: options.emitter,
		logger:  options.logger,
	}
}

func (s *RoleService) Create(ctx crud.Context, record *registry.CustomRole) (*registry.CustomRole, error) {
	if s.create == nil {
		return nil, goerrors.New("role create command missing", goerrors.CategoryInternal).WithCode(goerrors.CodeInternal)
	}
	res, err := s.guard.Enforce(crudguard.GuardInput{
		Context:   ctx,
		Operation: crud.OpCreate,
		Scope: types.ScopeFilter{
			TenantID: record.TenantID,
			OrgID:    record.OrgID,
		},
	})
	if err != nil {
		return nil, err
	}
	result := types.RoleDefinition{}
	input := command.CreateRoleInput{
		Name:        record.Name,
		Description: record.Description,
		RoleKey:     record.RoleKey,
		Permissions: append([]string{}, record.Permissions...),
		Metadata:    record.Metadata,
		IsSystem:    record.IsSystem,
		Scope:       res.Scope,
		Actor:       res.Actor,
		Result:      &result,
	}
	if err := s.create.Execute(ctx.UserContext(), input); err != nil {
		return nil, err
	}
	s.emit(ctx.UserContext(), res, result.ID, "role.created")
	return registry.DefinitionToCustomRole(&result), nil
}

func (s *RoleService) CreateBatch(crud.Context, []*registry.CustomRole) ([]*registry.CustomRole, error) {
	return nil, notSupported(crud.OpCreateBatch)
}

func (s *RoleService) Update(ctx crud.Context, record *registry.CustomRole) (*registry.CustomRole, error) {
	if s.update == nil {
		return nil, goerrors.New("role update command missing", goerrors.CategoryInternal).WithCode(goerrors.CodeInternal)
	}
	res, err := s.guard.Enforce(crudguard.GuardInput{
		Context:   ctx,
		Operation: crud.OpUpdate,
		Scope: types.ScopeFilter{
			TenantID: record.TenantID,
			OrgID:    record.OrgID,
		},
		TargetID: record.ID,
	})
	if err != nil {
		return nil, err
	}
	result := types.RoleDefinition{}
	input := command.UpdateRoleInput{
		RoleID:      record.ID,
		Name:        record.Name,
		Description: record.Description,
		RoleKey:     record.RoleKey,
		Permissions: append([]string{}, record.Permissions...),
		Metadata:    record.Metadata,
		IsSystem:    record.IsSystem,
		Scope:       res.Scope,
		Actor:       res.Actor,
		Result:      &result,
	}
	if err := s.update.Execute(ctx.UserContext(), input); err != nil {
		return nil, err
	}
	s.emit(ctx.UserContext(), res, result.ID, "role.updated")
	return registry.DefinitionToCustomRole(&result), nil
}

func (s *RoleService) UpdateBatch(crud.Context, []*registry.CustomRole) ([]*registry.CustomRole, error) {
	return nil, notSupported(crud.OpUpdateBatch)
}

func (s *RoleService) Delete(ctx crud.Context, record *registry.CustomRole) error {
	if s.delete == nil {
		return goerrors.New("role delete command missing", goerrors.CategoryInternal).WithCode(goerrors.CodeInternal)
	}
	res, err := s.guard.Enforce(crudguard.GuardInput{
		Context:   ctx,
		Operation: crud.OpDelete,
		Scope: types.ScopeFilter{
			TenantID: record.TenantID,
			OrgID:    record.OrgID,
		},
		TargetID: record.ID,
	})
	if err != nil {
		return err
	}
	input := command.DeleteRoleInput{
		RoleID: record.ID,
		Scope:  res.Scope,
		Actor:  res.Actor,
	}
	if err := s.delete.Execute(ctx.UserContext(), input); err != nil {
		return err
	}
	s.emit(ctx.UserContext(), res, record.ID, "role.deleted")
	return nil
}

func (s *RoleService) DeleteBatch(crud.Context, []*registry.CustomRole) error {
	return notSupported(crud.OpDeleteBatch)
}

func (s *RoleService) Index(ctx crud.Context, _ []repository.SelectCriteria) ([]*registry.CustomRole, int, error) {
	if s.list == nil {
		return nil, 0, goerrors.New("role list query missing", goerrors.CategoryInternal).WithCode(goerrors.CodeInternal)
	}
	res, err := s.guard.Enforce(crudguard.GuardInput{
		Context:   ctx,
		Operation: crud.OpList,
	})
	if err != nil {
		return nil, 0, err
	}
	includeSystem, _ := queryBool(ctx, "include_system")
	filter := types.RoleFilter{
		Actor:         res.Actor,
		Scope:         res.Scope,
		Keyword:       ctx.Query("q"),
		IncludeSystem: includeSystem,
		Pagination: types.Pagination{
			Limit:  queryInt(ctx, "limit", 50),
			Offset: queryInt(ctx, "offset", 0),
		},
	}
	page, err := s.list.Query(ctx.UserContext(), filter)
	if err != nil {
		return nil, 0, err
	}
	records := make([]*registry.CustomRole, 0, len(page.Roles))
	for _, role := range page.Roles {
		records = append(records, registry.DefinitionToCustomRole(&role))
	}
	return records, page.Total, nil
}

func (s *RoleService) Show(ctx crud.Context, id string, _ []repository.SelectCriteria) (*registry.CustomRole, error) {
	if s.detail == nil {
		return nil, goerrors.New("role detail query missing", goerrors.CategoryInternal).WithCode(goerrors.CodeInternal)
	}
	roleID, err := uuid.Parse(id)
	if err != nil {
		return nil, goerrors.New("invalid role id", goerrors.CategoryValidation).WithCode(goerrors.CodeBadRequest)
	}
	res, err := s.guard.Enforce(crudguard.GuardInput{
		Context:   ctx,
		Operation: crud.OpRead,
		TargetID:  roleID,
	})
	if err != nil {
		return nil, err
	}
	detail, err := s.detail.Query(ctx.UserContext(), query.RoleDetailInput{
		RoleID: roleID,
		Scope:  res.Scope,
		Actor:  res.Actor,
	})
	if err != nil {
		return nil, err
	}
	return registry.DefinitionToCustomRole(detail), nil
}

func (s *RoleService) emit(ctx context.Context, guardResult crudguard.GuardResult, roleID uuid.UUID, verb string) {
	if s.emitter == nil {
		return
	}
	record := types.ActivityRecord{
		UserID:     guardResult.Actor.ID,
		ActorID:    guardResult.Actor.ID,
		TenantID:   guardResult.Scope.TenantID,
		OrgID:      guardResult.Scope.OrgID,
		Verb:       verb,
		ObjectType: "role",
		ObjectID:   roleID.String(),
		Channel:    "roles",
	}
	if err := s.emitter.Emit(ctx, record); err != nil && s.logger != nil {
		s.logger.Error("role activity emit failed", err)
	}
}
