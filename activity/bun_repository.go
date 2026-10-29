package activity

import (
	"context"
	"errors"
	"maps"
	"strings"
	"time"

	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
)

// MachineActivityFilter adds SQL exclusion predicates using trusted host-owned
// classification. It is called only when machine activity is disabled, before
// pagination/counting, for feeds, cursors and statistics. Preserve the existing
// query predicates; use ExcludeMachineActivity for legacy rows. Never derive
// this callback from request input. A nil callback uses the default metadata filter.
type MachineActivityFilter func(*bun.SelectQuery, []string, []string) *bun.SelectQuery

// RepositoryConfig wires the Bun-backed activity repository.
type RepositoryConfig struct {
	MachineActivityFilter MachineActivityFilter
	DB                    *bun.DB
	Repository            repository.Repository[*LogEntry]
	Clock                 types.Clock
	IDGen                 types.IDGenerator
	Enricher              ActivityEnricher
	// EnrichmentErrorHandler controls fail-fast vs best-effort behavior.
	EnrichmentErrorHandler EnrichmentErrorHandler
}

type activityStore interface {
	repository.Repository[*LogEntry]
}

// Repository persists activity logs and exposes query helpers.
type Repository struct {
	machineActivityFilter MachineActivityFilter
	activityStore
	db    *bun.DB
	clock types.Clock
	idGen types.IDGenerator

	enricher               ActivityEnricher
	enrichmentErrorHandler EnrichmentErrorHandler
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

	db := cfg.DB
	if db == nil {
		if provider, ok := repo.(interface{ DB() *bun.DB }); ok {
			db = provider.DB()
		}
	}

	return &Repository{
		machineActivityFilter:  cfg.MachineActivityFilter,
		activityStore:          repo,
		db:                     db,
		clock:                  clock,
		idGen:                  idGen,
		enricher:               cfg.Enricher,
		enrichmentErrorHandler: cfg.EnrichmentErrorHandler,
	}, nil
}

var (
	_ repository.Repository[*LogEntry]   = (*Repository)(nil)
	_ types.ActivitySink                 = (*Repository)(nil)
	_ types.ActivityRepository           = (*Repository)(nil)
	_ ActivityEnrichmentStore            = (*Repository)(nil)
	_ ActivityEnrichmentStoreWithOptions = (*Repository)(nil)
	_ ActivityEnrichmentQuery            = (*Repository)(nil)
)

// Log persists an activity record into the database.
func (r *Repository) Log(ctx context.Context, record types.ActivityRecord) error {
	entry, err := r.prepareEntry(ctx, record)
	if err != nil {
		return err
	}
	_, err = r.Create(ctx, entry)
	return err
}

// LogTx persists required audit in the caller's transaction and returns failures
// to its owner. Disclosure policy is applied after enrichment; nil policy drops
// metadata and IP. Callers must propagate this error to roll back; legacy command
// Activity sinks remain best effort and cannot make a mutation transactional.
func (r *Repository) LogTx(ctx context.Context, tx bun.Tx, record types.ActivityRecord, policy *RecordPolicy) error {
	if tx.Tx == nil {
		return errors.New("activity: a live transaction is required")
	}
	entry, err := r.prepareEntry(ctx, record)
	if err != nil {
		return err
	}
	entry = toLogEntry(policy.Sanitize(toActivityRecord(entry)))
	_, err = r.CreateTx(ctx, tx, entry)
	return err
}

func (r *Repository) prepareEntry(ctx context.Context, record types.ActivityRecord) (*LogEntry, error) {
	if _, _, err := types.CanonicalScope(types.ScopeFilter{Labels: record.Labels}); err != nil {
		return nil, err
	}
	if r.enricher != nil {
		enriched, err := applyEnricher(ctx, r.enricher, r.enrichmentErrorHandler, record)
		if err != nil {
			return nil, err
		}
		record = enriched
	}
	scope, key, err := types.CanonicalScope(types.ScopeFilter{TenantID: record.TenantID, OrgID: record.OrgID, Labels: record.Labels})
	if err != nil {
		return nil, err
	}
	record.Labels = scope.Labels
	entry := toLogEntry(record)
	entry.ScopeKey = key
	if entry.ID == uuid.Nil {
		entry.ID = r.idGen.UUID()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = r.clock.Now()
	}
	return entry, nil
}

// ListActivity returns a paginated feed filtered by the supplied criteria.
func (r *Repository) ListActivity(ctx context.Context, filter types.ActivityFilter) (types.ActivityPage, error) {
	normalized, _, err := types.CanonicalScope(filter.Scope)
	if err != nil {
		return types.ActivityPage{}, err
	}
	filter.Scope = normalized
	pagination := normalizePagination(filter.Pagination, 50, 200)
	criteria := []repository.SelectCriteria{
		func(q *bun.SelectQuery) *bun.SelectQuery {
			q = q.OrderExpr("created_at DESC").
				Limit(pagination.Limit).
				Offset(pagination.Offset)
			return applyActivityFilter(q, filter, r.machineActivityFilter)
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
	normalized, _, err := types.CanonicalScope(filter.Scope)
	if err != nil {
		return types.ActivityStats{}, err
	}
	filter.Scope = normalized
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
	query = applyActivityStatsFilter(query, filter, r.machineActivityFilter)

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

func applyActivityFilter(q *bun.SelectQuery, filter types.ActivityFilter, classifiers ...MachineActivityFilter) *bun.SelectQuery {
	q = applyActivityScopeFilter(q, filter.Scope)
	q = applyActivityActorFilter(q, filter.UserID, filter.ActorID)
	q = applyActivityObjectFilter(q, filter)
	q = applyActivityChannelFilter(q, filter)
	q = applyMachineActivityFilter(q, filter.MachineActivityEnabled, filter.MachineActorTypes, filter.MachineDataKeys, classifiers...)
	q = applyActivityTimeFilter(q, filter.Since, filter.Until)
	if strings.TrimSpace(filter.Keyword) != "" {
		keyword := "%" + strings.ToLower(strings.TrimSpace(filter.Keyword)) + "%"
		q = q.Where("LOWER(verb) LIKE ? OR LOWER(object_type) LIKE ? OR LOWER(object_id) LIKE ?", keyword, keyword, keyword)
	}
	return q
}

func applyActivityScopeFilter(q *bun.SelectQuery, scope types.ScopeFilter) *bun.SelectQuery {
	normalized, key, err := types.CanonicalScope(scope)
	if err != nil {
		return q.Where("1 = 0")
	}
	q = q.Where("scope_key = ?", key)
	if key != "" {
		return q.Where("tenant_id = ? AND org_id = ?", normalized.TenantID, normalized.OrgID)
	}
	if scope.TenantID != uuid.Nil {
		q = q.Where("tenant_id = ?", scope.TenantID)
	}
	if scope.OrgID != uuid.Nil {
		q = q.Where("org_id = ?", scope.OrgID)
	}
	return q
}

func applyActivityActorFilter(q *bun.SelectQuery, userID, actorID uuid.UUID) *bun.SelectQuery {
	if userID != uuid.Nil && actorID != uuid.Nil {
		return q.Where("(user_id = ? OR actor_id = ?)", userID, actorID)
	}
	if userID != uuid.Nil {
		q = q.Where("user_id = ?", userID)
	}
	if actorID != uuid.Nil {
		q = q.Where("actor_id = ?", actorID)
	}
	return q
}

func applyActivityObjectFilter(q *bun.SelectQuery, filter types.ActivityFilter) *bun.SelectQuery {
	if len(filter.Verbs) > 0 {
		q = q.Where("verb IN (?)", bun.List(filter.Verbs))
	}
	if filter.ObjectType != "" {
		q = q.Where("object_type = ?", filter.ObjectType)
	}
	if filter.ObjectID != "" {
		q = q.Where("object_id = ?", filter.ObjectID)
	}
	return q
}

func applyActivityChannelFilter(q *bun.SelectQuery, filter types.ActivityFilter) *bun.SelectQuery {
	if len(filter.Channels) > 0 {
		q = q.Where("channel IN (?)", bun.List(filter.Channels))
	} else if filter.Channel != "" {
		q = q.Where("channel = ?", filter.Channel)
	}
	if len(filter.ChannelDenylist) > 0 {
		q = q.Where("channel NOT IN (?)", bun.List(filter.ChannelDenylist))
	}
	return q
}

func applyActivityTimeFilter(q *bun.SelectQuery, since, until *time.Time) *bun.SelectQuery {
	if since != nil && !since.IsZero() {
		q = q.Where("created_at >= ?", since)
	}
	if until != nil && !until.IsZero() {
		q = q.Where("created_at <= ?", until)
	}
	return q
}

func applyActivityStatsFilter(q *bun.SelectQuery, filter types.ActivityStatsFilter, classifiers ...MachineActivityFilter) *bun.SelectQuery {
	q = applyActivityScopeFilter(q, filter.Scope)
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
		q = q.Where("verb IN (?)", bun.List(filter.Verbs))
	}
	q = applyMachineActivityFilter(q, filter.MachineActivityEnabled, filter.MachineActorTypes, filter.MachineDataKeys, classifiers...)
	return q
}

func applyMachineActivityFilter(q *bun.SelectQuery, enabled *bool, actorTypes, dataKeys []string, classifiers ...MachineActivityFilter) *bun.SelectQuery {
	if q == nil || enabled == nil || *enabled {
		return q
	}
	actorTypes = normalizeIdentifiers(actorTypes)
	dataKeys = normalizeIdentifiers(dataKeys)
	if len(classifiers) > 0 && classifiers[0] != nil {
		return classifiers[0](q, actorTypes, dataKeys)
	}
	return ExcludeMachineActivity(q, actorTypes, dataKeys)
}

// ExcludeMachineActivity applies the default legacy metadata exclusion predicate.
// Hosts composing trusted identity filters may use it inside a grouped fallback
// branch. Values are normalized and parameterized; scope remains caller-owned.
func ExcludeMachineActivity(q *bun.SelectQuery, actorTypes, dataKeys []string) *bun.SelectQuery {
	if q == nil {
		return nil
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
		args = append(args, bun.List(actorTypes))
		conditions = append(conditions, "data ->> 'actorType' IN (?)")
		args = append(args, bun.List(actorTypes))
		conditions = append(conditions, "data -> 'actor' ->> 'type' IN (?)")
		args = append(args, bun.List(actorTypes))
	}
	if len(conditions) == 0 {
		return q
	}
	return q.Where("NOT COALESCE(("+strings.Join(conditions, " OR ")+"), FALSE)", args...)
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
		Labels:     maps.Clone(record.Labels),
		ScopeKey:   activityScopeKey(record),
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
		Labels:     maps.Clone(entry.Labels),
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
	maps.Copy(out, src)
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

func activityScopeKey(record types.ActivityRecord) string {
	_, key, _ := types.CanonicalScope(types.ScopeFilter{Labels: record.Labels})
	return key
}

// ListActivityCursor applies exact scope and cursor predicates before SQL limit.
// It is a persistence API; callers must authorize the filter scope first.
func (r *Repository) ListActivityCursor(ctx context.Context, filter types.ActivityFilter, cursor *ActivityCursor) ([]types.ActivityRecord, error) {
	if r.db == nil {
		return nil, errors.New("activity: cursor requires bun DB")
	}
	normalized, _, err := types.CanonicalScope(filter.Scope)
	if err != nil {
		return nil, err
	}
	filter.Scope = normalized
	var rows []*LogEntry
	q := applyActivityFilter(r.db.NewSelect().Model(&rows), filter, r.machineActivityFilter)
	p := normalizePagination(filter.Pagination, 50, 200)
	err = ApplyCursorPagination(q, cursor, p.Limit).Scan(ctx)
	if err != nil {
		return nil, err
	}
	records := make([]types.ActivityRecord, 0, len(rows))
	for _, row := range rows {
		records = append(records, toActivityRecord(row))
	}
	return records, nil
}
