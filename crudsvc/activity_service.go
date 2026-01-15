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
	"github.com/goliatone/go-users/pkg/authctx"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

// ActivityServiceConfig wires dependencies for the CRUD-backed activity service.
type ActivityServiceConfig struct {
	Guard      GuardAdapter
	LogCommand gocommand.Commander[command.ActivityLogInput]
	FeedQuery  gocommand.Querier[types.ActivityFilter, types.ActivityPage]
	Policy     activity.ActivityAccessPolicy
}

// ActivityService adapts the go-users activity command/query layer to a go-crud
// controller.
type ActivityService struct {
	guard   GuardAdapter
	logCmd  gocommand.Commander[command.ActivityLogInput]
	feed    gocommand.Querier[types.ActivityFilter, types.ActivityPage]
	policy  activity.ActivityAccessPolicy
	emitter ActivityEmitter
	logger  types.Logger
}

// NewActivityService constructs the adapter.
func NewActivityService(cfg ActivityServiceConfig, opts ...ServiceOption) *ActivityService {
	options := applyOptions(opts)
	policy := cfg.Policy
	if policy == nil {
		policy = activity.NewDefaultAccessPolicy()
	}
	service := &ActivityService{
		guard:   cfg.Guard,
		logCmd:  cfg.LogCommand,
		feed:    cfg.FeedQuery,
		policy:  policy,
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

	actorCtx, err := authctx.ResolveActorContext(ctx.UserContext())
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
		Channels:   queryStringSlice(ctx, "channels"),
		ChannelDenylist: func() []string {
			if values := queryStringSlice(ctx, "channel_denylist"); len(values) > 0 {
				return values
			}
			return queryStringSlice(ctx, "channelDenylist")
		}(),
		Since:   queryTime(ctx, "since"),
		Until:   queryTime(ctx, "until"),
		Keyword: ctx.Query("q"),
		Pagination: types.Pagination{
			Limit:  queryInt(ctx, "limit", 50),
			Offset: queryInt(ctx, "offset", 0),
		},
	}
	if s.policy != nil {
		filter, err = s.policy.Apply(actorCtx, res.Actor.Type, filter)
		if err != nil {
			return nil, 0, err
		}
	}
	page, err := s.feed.Query(ctx.UserContext(), filter)
	if err != nil {
		return nil, 0, err
	}
	records := page.Records
	if s.policy != nil {
		records = s.policy.Sanitize(actorCtx, res.Actor.Type, records)
	}
	entries := make([]*activity.LogEntry, 0, len(records))
	for _, record := range records {
		entries = append(entries, activity.FromActivityRecord(record))
	}
	return entries, page.Total, nil
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

func enforceActivityOwnership(actor types.ActorRef, target uuid.UUID) error {
	if !actor.IsSupport() || target == uuid.Nil || target == actor.ID {
		return nil
	}
	return goerrors.New("go-users: support actors can only log their own activity", goerrors.CategoryAuthz).
		WithCode(goerrors.CodeForbidden)
}
