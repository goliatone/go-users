package crudsvc

import (
	"context"
	"strings"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-crud"
	goerrors "github.com/goliatone/go-errors"
	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-users/activity"
	"github.com/goliatone/go-users/command"
	"github.com/goliatone/go-users/crudguard"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

// ActivityServiceConfig wires dependencies for the CRUD-backed activity service.
type ActivityServiceConfig struct {
	Guard      GuardAdapter
	LogCommand gocommand.Commander[command.ActivityLogInput]
	FeedQuery  gocommand.Querier[types.ActivityFilter, types.ActivityPage]
}

// ActivityService adapts the go-users activity command/query layer to a go-crud
// controller.
type ActivityService struct {
	guard   GuardAdapter
	logCmd  gocommand.Commander[command.ActivityLogInput]
	feed    gocommand.Querier[types.ActivityFilter, types.ActivityPage]
	emitter ActivityEmitter
	logger  types.Logger
}

// NewActivityService constructs the adapter.
func NewActivityService(cfg ActivityServiceConfig, opts ...ServiceOption) *ActivityService {
	options := applyOptions(opts)
	service := &ActivityService{
		guard:   cfg.Guard,
		logCmd:  cfg.LogCommand,
		feed:    cfg.FeedQuery,
		emitter: options.emitter,
		logger:  options.logger,
	}
	return service
}

func (s *ActivityService) Create(ctx crud.Context, record *activity.LogEntry) (*activity.LogEntry, error) {
	if s.logCmd == nil {
		return nil, goerrors.New("activity logging disabled", goerrors.CategoryInternal).WithCode(goerrors.CodeInternal)
	}
	payload := activity.ToActivityRecord(record)
	requestedScope := types.ScopeFilter{
		TenantID: payload.TenantID,
		OrgID:    payload.OrgID,
	}
	res, err := s.guard.Enforce(crudguard.GuardInput{
		Context:   ctx,
		Operation: crud.OpCreate,
		Scope:     requestedScope,
		TargetID:  record.UserID,
	})
	if err != nil {
		return nil, err
	}
	if err := enforceActivityOwnership(res.Actor, payload.UserID); err != nil {
		return nil, err
	}

	payload.ActorID = res.Actor.ID
	payload.TenantID = res.Scope.TenantID
	payload.OrgID = res.Scope.OrgID

	input := command.ActivityLogInput{Record: payload}
	if err := s.logCmd.Execute(ctx.UserContext(), input); err != nil {
		return nil, err
	}
	s.emit(ctx.UserContext(), payload)
	return activity.FromActivityRecord(payload), nil
}

func (s *ActivityService) CreateBatch(ctx crud.Context, records []*activity.LogEntry) ([]*activity.LogEntry, error) {
	created := make([]*activity.LogEntry, 0, len(records))
	for _, record := range records {
		rec, err := s.Create(ctx, record)
		if err != nil {
			return nil, err
		}
		created = append(created, rec)
	}
	return created, nil
}

func (s *ActivityService) Update(crud.Context, *activity.LogEntry) (*activity.LogEntry, error) {
	return nil, notSupported(crud.OpUpdate)
}

func (s *ActivityService) UpdateBatch(crud.Context, []*activity.LogEntry) ([]*activity.LogEntry, error) {
	return nil, notSupported(crud.OpUpdateBatch)
}

func (s *ActivityService) Delete(crud.Context, *activity.LogEntry) error {
	return notSupported(crud.OpDelete)
}

func (s *ActivityService) DeleteBatch(crud.Context, []*activity.LogEntry) error {
	return notSupported(crud.OpDeleteBatch)
}

func (s *ActivityService) Index(ctx crud.Context, _ []repository.SelectCriteria) ([]*activity.LogEntry, int, error) {
	if s.feed == nil {
		return nil, 0, goerrors.New("activity feed query unavailable", goerrors.CategoryInternal).WithCode(goerrors.CodeInternal)
	}
	res, err := s.guard.Enforce(crudguard.GuardInput{
		Context:   ctx,
		Operation: crud.OpList,
	})
	if err != nil {
		return nil, 0, err
	}

	filter := types.ActivityFilter{
		Actor:      res.Actor,
		Scope:      res.Scope,
		UserID:     queryUUID(ctx, "user_id"),
		ActorID:    queryUUID(ctx, "actor_id"),
		Verbs:      queryStringSlice(ctx, "verb"),
		ObjectType: strings.TrimSpace(ctx.Query("object_type")),
		ObjectID:   strings.TrimSpace(ctx.Query("object_id")),
		Channel:    strings.TrimSpace(ctx.Query("channel")),
		Since:      queryTime(ctx, "since"),
		Until:      queryTime(ctx, "until"),
		Keyword:    ctx.Query("q"),
		Pagination: types.Pagination{
			Limit:  queryInt(ctx, "limit", 50),
			Offset: queryInt(ctx, "offset", 0),
		},
	}
	applyActivityRowPolicy(&filter, res.Actor)
	page, err := s.feed.Query(ctx.UserContext(), filter)
	if err != nil {
		return nil, 0, err
	}
	filtered := filterActivityRecords(page.Records, res.Actor)
	records := make([]*activity.LogEntry, 0, len(filtered))
	for _, record := range filtered {
		records = append(records, applyActivityFieldPolicy(activity.FromActivityRecord(record), res.Actor))
	}
	return records, len(filtered), nil
}

func (s *ActivityService) Show(crud.Context, string, []repository.SelectCriteria) (*activity.LogEntry, error) {
	return nil, notSupported(crud.OpRead)
}

func (s *ActivityService) emit(ctx context.Context, record types.ActivityRecord) {
	if s.emitter == nil {
		return
	}
	if err := s.emitter.Emit(ctx, record); err != nil && s.logger != nil {
		s.logger.Error("activity emitter failed", err)
	}
}

func applyActivityRowPolicy(filter *types.ActivityFilter, actor types.ActorRef) {
	if filter == nil {
		return
	}
	if actor.IsSupport() {
		filter.UserID = actor.ID
		filter.ActorID = actor.ID
	}
}

func filterActivityRecords(records []types.ActivityRecord, actor types.ActorRef) []types.ActivityRecord {
	if !actor.IsSupport() {
		return records
	}
	filtered := make([]types.ActivityRecord, 0, len(records))
	for _, record := range records {
		if record.UserID == actor.ID || record.ActorID == actor.ID {
			filtered = append(filtered, record)
		}
	}
	return filtered
}

func applyActivityFieldPolicy(entry *activity.LogEntry, actor types.ActorRef) *activity.LogEntry {
	if entry == nil {
		return nil
	}
	if !actor.IsSystemAdmin() {
		entry.IP = ""
	}
	if actor.IsSupport() && len(entry.Data) > 0 {
		entry.Data = nil
	}
	return entry
}

func enforceActivityOwnership(actor types.ActorRef, target uuid.UUID) error {
	if !actor.IsSupport() || target == uuid.Nil || target == actor.ID {
		return nil
	}
	return goerrors.New("go-users: support actors can only log their own activity", goerrors.CategoryAuthz).
		WithCode(goerrors.CodeForbidden)
}
