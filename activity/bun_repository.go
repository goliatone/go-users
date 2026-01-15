package activity

import (
	"context"
	"errors"
	"strings"

	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
)

// RepositoryConfig wires the Bun-backed activity repository.
type RepositoryConfig struct {
	DB         *bun.DB
	Repository repository.Repository[*LogEntry]
	Clock      types.Clock
	IDGen      types.IDGenerator
}

type activityStore interface {
	repository.Repository[*LogEntry]
}

// Repository persists activity logs and exposes query helpers.
type Repository struct {
	activityStore
	db    *bun.DB
	clock types.Clock
	idGen types.IDGenerator
}

// NewRepository constructs a repository that implements both ActivitySink
// and ActivityRepository interfaces.
func NewRepository(cfg RepositoryConfig) (*Repository, error) {
	if cfg.Repository == nil && cfg.DB == nil {
		return nil, errors.New("activity: db or repository required")
	}
	repo := cfg.Repository
	if repo == nil {
		repo = repository.NewRepository(cfg.DB, repository.ModelHandlers[*LogEntry]{
			NewRecord: func() *LogEntry { return &LogEntry{} },
			GetID: func(entry *LogEntry) uuid.UUID {
				if entry == nil {
					return uuid.Nil
				}
				return entry.ID
			},
			SetID: func(entry *LogEntry, id uuid.UUID) {
				if entry != nil {
					entry.ID = id
				}
			},
		})
	}
	clock := cfg.Clock
	if clock == nil {
		clock = types.SystemClock{}
	}
	idGen := cfg.IDGen
	if idGen == nil {
		idGen = types.UUIDGenerator{}
	}

	return &Repository{
		activityStore: repo,
		db:            cfg.DB,
		clock:         clock,
		idGen:         idGen,
	}, nil
}

var (
	_ repository.Repository[*LogEntry] = (*Repository)(nil)
	_ types.ActivitySink               = (*Repository)(nil)
	_ types.ActivityRepository         = (*Repository)(nil)
)

// Log persists an activity record into the database.
func (r *Repository) Log(ctx context.Context, record types.ActivityRecord) error {
	entry := toLogEntry(record)
	if entry.ID == uuid.Nil {
		entry.ID = r.idGen.UUID()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = r.clock.Now()
	}
	_, err := r.Create(ctx, entry)
	return err
}

// ListActivity returns a paginated feed filtered by the supplied criteria.
func (r *Repository) ListActivity(ctx context.Context, filter types.ActivityFilter) (types.ActivityPage, error) {
	pagination := normalizePagination(filter.Pagination, 50, 200)
	criteria := []repository.SelectCriteria{
		func(q *bun.SelectQuery) *bun.SelectQuery {
			q = q.OrderExpr("created_at DESC").
				Limit(pagination.Limit).
				Offset(pagination.Offset)
			return applyActivityFilter(q, filter)
		},
	}

	rows, total, err := r.List(ctx, criteria...)
	if err != nil {
		return types.ActivityPage{}, err
	}
	records := make([]types.ActivityRecord, 0, len(rows))
	for _, row := range rows {
		records = append(records, toActivityRecord(row))
	}
	return types.ActivityPage{
		Records:    records,
		Total:      total,
		NextOffset: pagination.Offset + pagination.Limit,
		HasMore:    pagination.Offset+pagination.Limit < total,
	}, nil
}

// ActivityStats aggregates counts grouped by verb.
func (r *Repository) ActivityStats(ctx context.Context, filter types.ActivityStatsFilter) (types.ActivityStats, error) {
	stats := types.ActivityStats{
		ByVerb: make(map[string]int),
	}
	if r.db == nil {
		return stats, errors.New("activity: stats requires bun DB")
	}
	query := r.db.NewSelect().
		Table("user_activity").
		ColumnExpr("COUNT(*) AS total").
		ColumnExpr("verb").
		Group("verb")
	query = applyActivityStatsFilter(query, filter)

	type row struct {
		Verb  string `bun:"verb"`
		Total int    `bun:"total"`
	}
	var rows []row
	if err := query.Scan(ctx, &rows); err != nil {
		return stats, err
	}
	total := 0
	for _, rec := range rows {
		stats.ByVerb[rec.Verb] = rec.Total
		total += rec.Total
	}
	stats.Total = total
	return stats, nil
}

func applyActivityFilter(q *bun.SelectQuery, filter types.ActivityFilter) *bun.SelectQuery {
	if filter.Scope.TenantID != uuid.Nil {
		q = q.Where("tenant_id = ?", filter.Scope.TenantID)
	}
	if filter.Scope.OrgID != uuid.Nil {
		q = q.Where("org_id = ?", filter.Scope.OrgID)
	}
	if filter.UserID != uuid.Nil && filter.ActorID != uuid.Nil {
		q = q.Where("(user_id = ? OR actor_id = ?)", filter.UserID, filter.ActorID)
	} else {
		if filter.UserID != uuid.Nil {
			q = q.Where("user_id = ?", filter.UserID)
		}
		if filter.ActorID != uuid.Nil {
			q = q.Where("actor_id = ?", filter.ActorID)
		}
	}
	if len(filter.Verbs) > 0 {
		q = q.Where("verb IN (?)", bun.In(filter.Verbs))
	}
	if filter.ObjectType != "" {
		q = q.Where("object_type = ?", filter.ObjectType)
	}
	if filter.ObjectID != "" {
		q = q.Where("object_id = ?", filter.ObjectID)
	}
	if len(filter.Channels) > 0 {
		q = q.Where("channel IN (?)", bun.In(filter.Channels))
	} else if filter.Channel != "" {
		q = q.Where("channel = ?", filter.Channel)
	}
	if len(filter.ChannelDenylist) > 0 {
		q = q.Where("channel NOT IN (?)", bun.In(filter.ChannelDenylist))
	}
	q = applyMachineActivityFilter(q, filter.MachineActivityEnabled, filter.MachineActorTypes, filter.MachineDataKeys)
	if filter.Since != nil && !filter.Since.IsZero() {
		q = q.Where("created_at >= ?", filter.Since)
	}
	if filter.Until != nil && !filter.Until.IsZero() {
		q = q.Where("created_at <= ?", filter.Until)
	}
	if strings.TrimSpace(filter.Keyword) != "" {
		keyword := "%" + strings.ToLower(strings.TrimSpace(filter.Keyword)) + "%"
		q = q.Where("LOWER(verb) LIKE ? OR LOWER(object_type) LIKE ? OR LOWER(object_id) LIKE ?", keyword, keyword, keyword)
	}
	return q
}

func applyActivityStatsFilter(q *bun.SelectQuery, filter types.ActivityStatsFilter) *bun.SelectQuery {
	if filter.Scope.TenantID != uuid.Nil {
		q = q.Where("tenant_id = ?", filter.Scope.TenantID)
	}
	if filter.Scope.OrgID != uuid.Nil {
		q = q.Where("org_id = ?", filter.Scope.OrgID)
	}
	if filter.UserID != uuid.Nil && filter.ActorID != uuid.Nil {
		q = q.Where("(user_id = ? OR actor_id = ?)", filter.UserID, filter.ActorID)
	} else {
		if filter.UserID != uuid.Nil {
			q = q.Where("user_id = ?", filter.UserID)
		}
		if filter.ActorID != uuid.Nil {
			q = q.Where("actor_id = ?", filter.ActorID)
		}
	}
	if filter.Since != nil && !filter.Since.IsZero() {
		q = q.Where("created_at >= ?", filter.Since)
	}
	if filter.Until != nil && !filter.Until.IsZero() {
		q = q.Where("created_at <= ?", filter.Until)
	}
	if len(filter.Verbs) > 0 {
		q = q.Where("verb IN (?)", bun.In(filter.Verbs))
	}
	q = applyMachineActivityFilter(q, filter.MachineActivityEnabled, filter.MachineActorTypes, filter.MachineDataKeys)
	return q
}

func applyMachineActivityFilter(q *bun.SelectQuery, enabled *bool, actorTypes, dataKeys []string) *bun.SelectQuery {
	if q == nil || enabled == nil || *enabled {
		return q
	}
	actorTypes = normalizeIdentifiers(actorTypes)
	dataKeys = normalizeIdentifiers(dataKeys)
	if len(actorTypes) == 0 && len(dataKeys) == 0 {
		return q
	}
	switch q.Dialect().Name() {
	case dialect.PG:
		return applyMachineActivityFilterPostgres(q, actorTypes, dataKeys)
	default:
		return applyMachineActivityFilterLike(q, actorTypes, dataKeys)
	}
}

func applyMachineActivityFilterPostgres(q *bun.SelectQuery, actorTypes, dataKeys []string) *bun.SelectQuery {
	conditions := make([]string, 0, len(dataKeys)+3)
	args := make([]any, 0, len(dataKeys)+3)
	for _, key := range dataKeys {
		if key == "" {
			continue
		}
		conditions = append(conditions, "data ->> ? = 'true'")
		args = append(args, key)
	}
	if len(actorTypes) > 0 {
		conditions = append(conditions, "data ->> 'actor_type' IN (?)")
		args = append(args, bun.In(actorTypes))
		conditions = append(conditions, "data ->> 'actorType' IN (?)")
		args = append(args, bun.In(actorTypes))
		conditions = append(conditions, "data -> 'actor' ->> 'type' IN (?)")
		args = append(args, bun.In(actorTypes))
	}
	if len(conditions) == 0 {
		return q
	}
	return q.Where("NOT ("+strings.Join(conditions, " OR ")+")", args...)
}

func applyMachineActivityFilterLike(q *bun.SelectQuery, actorTypes, dataKeys []string) *bun.SelectQuery {
	conditions := make([]string, 0, (len(dataKeys)*2)+(len(actorTypes)*3))
	args := make([]any, 0, (len(dataKeys)*2)+(len(actorTypes)*3))
	for _, key := range dataKeys {
		if key == "" {
			continue
		}
		conditions = append(conditions, "data LIKE ?")
		args = append(args, "%\""+key+"\":true%")
		conditions = append(conditions, "data LIKE ?")
		args = append(args, "%\""+key+"\":\"true\"%")
	}
	for _, actorType := range actorTypes {
		if actorType == "" {
			continue
		}
		conditions = append(conditions, "data LIKE ?")
		args = append(args, "%\"actor_type\":\""+actorType+"\"%")
		conditions = append(conditions, "data LIKE ?")
		args = append(args, "%\"actorType\":\""+actorType+"\"%")
		conditions = append(conditions, "data LIKE ?")
		args = append(args, "%\"actor\":{\"type\":\""+actorType+"\"%")
	}
	if len(conditions) == 0 {
		return q
	}
	return q.Where("NOT ("+strings.Join(conditions, " OR ")+")", args...)
}

func toLogEntry(record types.ActivityRecord) *LogEntry {
	return &LogEntry{
		ID:         record.ID,
		UserID:     record.UserID,
		ActorID:    record.ActorID,
		TenantID:   record.TenantID,
		OrgID:      record.OrgID,
		Verb:       record.Verb,
		ObjectType: record.ObjectType,
		ObjectID:   record.ObjectID,
		Channel:    record.Channel,
		IP:         record.IP,
		Data:       cloneMap(record.Data),
		CreatedAt:  record.OccurredAt,
	}
}

func toActivityRecord(entry *LogEntry) types.ActivityRecord {
	if entry == nil {
		return types.ActivityRecord{}
	}
	return types.ActivityRecord{
		ID:         entry.ID,
		UserID:     entry.UserID,
		ActorID:    entry.ActorID,
		TenantID:   entry.TenantID,
		OrgID:      entry.OrgID,
		Verb:       entry.Verb,
		ObjectType: entry.ObjectType,
		ObjectID:   entry.ObjectID,
		Channel:    entry.Channel,
		IP:         entry.IP,
		Data:       cloneMap(entry.Data),
		OccurredAt: entry.CreatedAt,
	}
}

// FromActivityRecord converts a domain activity record into the Bun model so it
// can be reused by transports without duplicating conversion logic.
func FromActivityRecord(record types.ActivityRecord) *LogEntry {
	return toLogEntry(record)
}

// ToActivityRecord converts the Bun model into the domain activity record.
func ToActivityRecord(entry *LogEntry) types.ActivityRecord {
	return toActivityRecord(entry)
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
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
