package crudsvc

import (
	"context"
	"strings"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-crud"
	goerrors "github.com/goliatone/go-errors"
	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-users/command"
	"github.com/goliatone/go-users/crudguard"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/preferences"
	"github.com/google/uuid"
)

type preferenceStore interface {
	GetByID(ctx context.Context, id string, criteria ...repository.SelectCriteria) (*preferences.Record, error)
}

// PreferenceServiceConfig wires dependencies for the preference CRUD adapter.
type PreferenceServiceConfig struct {
	Guard  GuardAdapter
	Repo   types.PreferenceRepository
	Store  preferenceStore
	Upsert gocommand.Commander[command.PreferenceUpsertInput]
	Delete gocommand.Commander[command.PreferenceDeleteInput]
}

// PreferenceService routes go-crud operations through preference commands and
// repositories so invariants (guard enforcement, hooks, activity) remain intact.
type PreferenceService struct {
	guard   GuardAdapter
	repo    types.PreferenceRepository
	store   preferenceStore
	upsert  gocommand.Commander[command.PreferenceUpsertInput]
	del     gocommand.Commander[command.PreferenceDeleteInput]
	emitter ActivityEmitter
	logger  types.Logger
}

// NewPreferenceService constructs the adapter.
func NewPreferenceService(cfg PreferenceServiceConfig, opts ...ServiceOption) *PreferenceService {
	options := applyOptions(opts)
	return &PreferenceService{
		guard:   cfg.Guard,
		repo:    cfg.Repo,
		store:   cfg.Store,
		upsert:  cfg.Upsert,
		del:     cfg.Delete,
		emitter: options.emitter,
		logger:  options.logger,
	}
}

func (s *PreferenceService) Create(ctx crud.Context, record *preferences.Record) (*preferences.Record, error) {
	return s.upsertRecord(ctx, crud.OpCreate, record)
}

func (s *PreferenceService) CreateBatch(ctx crud.Context, records []*preferences.Record) ([]*preferences.Record, error) {
	created := make([]*preferences.Record, 0, len(records))
	for _, record := range records {
		rec, err := s.upsertRecord(ctx, crud.OpCreateBatch, record)
		if err != nil {
			return nil, err
		}
		created = append(created, rec)
	}
	return created, nil
}

func (s *PreferenceService) Update(ctx crud.Context, record *preferences.Record) (*preferences.Record, error) {
	return s.upsertRecord(ctx, crud.OpUpdate, record)
}

func (s *PreferenceService) UpdateBatch(ctx crud.Context, records []*preferences.Record) ([]*preferences.Record, error) {
	updated := make([]*preferences.Record, 0, len(records))
	for _, record := range records {
		rec, err := s.upsertRecord(ctx, crud.OpUpdateBatch, record)
		if err != nil {
			return nil, err
		}
		updated = append(updated, rec)
	}
	return updated, nil
}

func (s *PreferenceService) Delete(ctx crud.Context, record *preferences.Record) error {
	if s.del == nil {
		return goerrors.New("preference delete command not wired", goerrors.CategoryInternal).WithCode(goerrors.CodeInternal)
	}
	domain := preferences.ToPreferenceRecord(record)
	res, err := s.guard.Enforce(crudguard.GuardInput{
		Context:   ctx,
		Operation: crud.OpDelete,
		Scope:     domain.Scope,
		TargetID:  domain.UserID,
	})
	if err != nil {
		return err
	}
	if err := enforcePreferenceUserAccess(res.Actor, domain.UserID); err != nil {
		return err
	}
	level := domain.Level
	if level == "" {
		level = types.PreferenceLevelUser
	}
	input := command.PreferenceDeleteInput{
		UserID: domain.UserID,
		Scope:  res.Scope,
		Level:  level,
		Key:    domain.Key,
		Actor:  res.Actor,
	}
	if err := s.del.Execute(ctx.UserContext(), input); err != nil {
		return err
	}
	s.emit(ctx.UserContext(), res, domain.UserID, domain.Key, "preference.delete")
	return nil
}

func (s *PreferenceService) DeleteBatch(ctx crud.Context, records []*preferences.Record) error {
	for _, record := range records {
		if err := s.Delete(ctx, record); err != nil {
			return err
		}
	}
	return nil
}

func (s *PreferenceService) Index(ctx crud.Context, _ []repository.SelectCriteria) ([]*preferences.Record, int, error) {
	if s.repo == nil {
		return nil, 0, goerrors.New("preference repository missing", goerrors.CategoryInternal).WithCode(goerrors.CodeInternal)
	}
	res, err := s.guard.Enforce(crudguard.GuardInput{
		Context:   ctx,
		Operation: crud.OpList,
	})
	if err != nil {
		return nil, 0, err
	}

	filter := types.PreferenceFilter{
		UserID: queryUUID(ctx, "user_id"),
		Scope:  res.Scope,
		Level:  parsePreferenceLevel(ctx.Query("level")),
		Keys:   queryStringSlice(ctx, "key"),
	}
	if filter.Level == "" {
		filter.Level = types.PreferenceLevelUser
	}
	applyPreferenceRowPolicy(&filter, res.Actor)
	records, err := s.repo.ListPreferences(ctx.UserContext(), filter)
	if err != nil {
		return nil, 0, err
	}
	filtered := filterPreferenceRecords(records, res.Actor)
	out := make([]*preferences.Record, 0, len(filtered))
	for _, record := range filtered {
		out = append(out, preferences.FromPreferenceRecord(record))
	}
	return out, len(out), nil
}

func (s *PreferenceService) Show(ctx crud.Context, id string, _ []repository.SelectCriteria) (*preferences.Record, error) {
	if s.store == nil {
		return nil, goerrors.New("preference store missing", goerrors.CategoryInternal).WithCode(goerrors.CodeInternal)
	}
	record, err := s.store.GetByID(ctx.UserContext(), id)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, goerrors.New("preference not found", goerrors.CategoryNotFound).WithCode(goerrors.CodeNotFound)
	}
	domain := preferences.ToPreferenceRecord(record)
	res, err := s.guard.Enforce(crudguard.GuardInput{
		Context:   ctx,
		Operation: crud.OpRead,
		Scope:     domain.Scope,
		TargetID:  domain.UserID,
	})
	if err != nil {
		return nil, err
	}
	if err := enforcePreferenceUserAccess(res.Actor, domain.UserID); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *PreferenceService) upsertRecord(ctx crud.Context, op crud.CrudOperation, record *preferences.Record) (*preferences.Record, error) {
	if s.upsert == nil {
		return nil, goerrors.New("preference upsert command missing", goerrors.CategoryInternal).WithCode(goerrors.CodeInternal)
	}
	domain := preferences.ToPreferenceRecord(record)
	scope := domain.Scope
	res, err := s.guard.Enforce(crudguard.GuardInput{
		Context:   ctx,
		Operation: op,
		Scope:     scope,
		TargetID:  domain.UserID,
	})
	if err != nil {
		return nil, err
	}
	if err := enforcePreferenceUserAccess(res.Actor, domain.UserID); err != nil {
		return nil, err
	}
	domain.Scope = res.Scope
	level := domain.Level
	if level == "" {
		level = types.PreferenceLevelUser
	}
	result := domain
	input := command.PreferenceUpsertInput{
		UserID: domain.UserID,
		Scope:  res.Scope,
		Level:  level,
		Key:    strings.TrimSpace(domain.Key),
		Value:  copyPreferenceMap(domain.Value),
		Actor:  res.Actor,
		Result: &result,
	}
	if err := s.upsert.Execute(ctx.UserContext(), input); err != nil {
		return nil, err
	}
	s.emit(ctx.UserContext(), res, domain.UserID, domain.Key, "preference.upsert")
	return preferences.FromPreferenceRecord(result), nil
}

func (s *PreferenceService) emit(ctx context.Context, guardResult crudguard.GuardResult, userID uuid.UUID, key, action string) {
	if s.emitter == nil {
		return
	}
	record := types.ActivityRecord{
		UserID:     userID,
		ActorID:    guardResult.Actor.ID,
		TenantID:   guardResult.Scope.TenantID,
		OrgID:      guardResult.Scope.OrgID,
		Verb:       action,
		ObjectType: "preference",
		ObjectID:   key,
		Channel:    "preferences",
		Data: map[string]any{
			"key": key,
		},
	}
	if err := s.emitter.Emit(ctx, record); err != nil && s.logger != nil {
		s.logger.Error("preference activity emit failed", err)
	}
}

func copyPreferenceMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func applyPreferenceRowPolicy(filter *types.PreferenceFilter, actor types.ActorRef) {
	if filter == nil {
		return
	}
	if actor.IsSupport() {
		filter.UserID = actor.ID
	}
}

func filterPreferenceRecords(records []types.PreferenceRecord, actor types.ActorRef) []types.PreferenceRecord {
	if !actor.IsSupport() {
		return records
	}
	filtered := make([]types.PreferenceRecord, 0, len(records))
	for _, record := range records {
		if record.UserID == actor.ID {
			filtered = append(filtered, record)
		}
	}
	return filtered
}

func enforcePreferenceUserAccess(actor types.ActorRef, target uuid.UUID) error {
	if !actor.IsSupport() || target == actor.ID {
		return nil
	}
	return goerrors.New("go-users: support actors can only manage their own preferences", goerrors.CategoryAuthz).
		WithCode(goerrors.CodeForbidden)
}
