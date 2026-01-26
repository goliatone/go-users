package activity

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
)

// UpdateActivityData merges missing keys into the activity data payload.
func (r *Repository) UpdateActivityData(ctx context.Context, id uuid.UUID, data map[string]any) error {
	if r == nil {
		return errors.New("activity: repository required")
	}
	if id == uuid.Nil {
		return errors.New("activity: activity id required")
	}
	if len(data) == 0 {
		return nil
	}

	entry, err := r.GetByID(ctx, id.String())
	if err != nil {
		return err
	}
	if entry == nil {
		return errors.New("activity: activity not found")
	}

	missing := missingActivityData(entry.Data, data)
	if len(missing) == 0 {
		return nil
	}

	db := r.getDB()
	if db == nil {
		entry.Data = mergeActivityData(entry.Data, missing)
		_, err := r.Update(ctx, entry)
		return err
	}

	if db.Dialect().Name() == dialect.PG {
		_, err := db.NewUpdate().
			Table("user_activity").
			Set("data = data || ?::jsonb", missing).
			Where("id = ?", id).
			Exec(ctx)
		return err
	}

	merged := mergeActivityData(entry.Data, missing)
	_, err = db.NewUpdate().
		Table("user_activity").
		Set("data = ?", merged).
		Where("id = ?", id).
		Exec(ctx)
	return err
}

// ListActivityForEnrichment returns activity records missing enrichment keys or stale by cutoff.
func (r *Repository) ListActivityForEnrichment(ctx context.Context, filter ActivityEnrichmentFilter) (ActivityEnrichmentPage, error) {
	if r == nil {
		return ActivityEnrichmentPage{}, errors.New("activity: repository required")
	}
	db := r.getDB()
	if db == nil {
		return ActivityEnrichmentPage{}, errors.New("activity: enrichment query requires bun DB")
	}

	if db.Dialect().Name() == dialect.PG {
		return r.listActivityForEnrichmentQuery(ctx, db, filter)
	}

	return r.listActivityForEnrichmentFallback(ctx, db, filter)
}

func (r *Repository) listActivityForEnrichmentQuery(ctx context.Context, db *bun.DB, filter ActivityEnrichmentFilter) (ActivityEnrichmentPage, error) {
	limit := normalizeEnrichmentLimit(filter.Limit)
	rows := make([]LogEntry, 0, limit)
	query := db.NewSelect().
		Model(&rows)
	query = applyEnrichmentFilter(query, filter)
	query = ApplyCursorPagination(query, filter.Cursor, limit)
	if err := query.Scan(ctx); err != nil {
		return ActivityEnrichmentPage{}, err
	}

	return toEnrichmentPage(rows), nil
}

func (r *Repository) listActivityForEnrichmentFallback(ctx context.Context, db *bun.DB, filter ActivityEnrichmentFilter) (ActivityEnrichmentPage, error) {
	limit := normalizeEnrichmentLimit(filter.Limit)
	if limit <= 0 {
		return ActivityEnrichmentPage{}, nil
	}

	normalized := normalizeIdentifiers(filter.MissingKeys)
	cursor := filter.Cursor
	records := make([]types.ActivityRecord, 0, limit)
	var lastScanned *ActivityCursor

	for {
		rows := make([]LogEntry, 0, limit)
		query := db.NewSelect().
			Model(&rows)
		query = applyEnrichmentScope(query, filter.Scope)
		query = ApplyCursorPagination(query, cursor, limit)
		if err := query.Scan(ctx); err != nil {
			return ActivityEnrichmentPage{}, err
		}
		if len(rows) == 0 {
			break
		}

		for i := range rows {
			record := toActivityRecord(&rows[i])
			lastScanned = &ActivityCursor{OccurredAt: record.OccurredAt, ID: record.ID}
			if matchesEnrichmentFilter(record, normalized, filter.EnrichedBefore) {
				records = append(records, record)
				if len(records) >= limit {
					return ActivityEnrichmentPage{Records: records, NextCursor: lastScanned}, nil
				}
			}
		}

		if len(rows) < limit {
			break
		}
		cursor = lastScanned
	}

	return ActivityEnrichmentPage{Records: records, NextCursor: lastScanned}, nil
}

func applyEnrichmentFilter(q *bun.SelectQuery, filter ActivityEnrichmentFilter) *bun.SelectQuery {
	if q == nil {
		return nil
	}
	q = applyEnrichmentScope(q, filter.Scope)
	return applyEnrichmentMissingOrStaleFilter(q, filter)
}

func applyEnrichmentScope(q *bun.SelectQuery, scope types.ScopeFilter) *bun.SelectQuery {
	if scope.TenantID != uuid.Nil {
		q = q.Where("tenant_id = ?", scope.TenantID)
	}
	if scope.OrgID != uuid.Nil {
		q = q.Where("org_id = ?", scope.OrgID)
	}
	return q
}

func applyEnrichmentMissingOrStaleFilter(q *bun.SelectQuery, filter ActivityEnrichmentFilter) *bun.SelectQuery {
	if q == nil {
		return nil
	}
	missingKeys := normalizeIdentifiers(filter.MissingKeys)
	var conditions []string
	args := make([]any, 0, len(missingKeys)+4)

	if len(missingKeys) > 0 {
		cond, condArgs := missingKeyCondition(q.Dialect().Name(), missingKeys)
		if cond != "" {
			conditions = append(conditions, cond)
			args = append(args, condArgs...)
		}
	}

	if filter.EnrichedBefore != nil && !filter.EnrichedBefore.IsZero() {
		cond, condArgs := enrichedBeforeCondition(q.Dialect().Name(), *filter.EnrichedBefore)
		if cond != "" {
			conditions = append(conditions, cond)
			args = append(args, condArgs...)
		}
	}

	if len(conditions) == 0 {
		return q
	}
	return q.Where("("+strings.Join(conditions, " OR ")+")", args...)
}

func missingKeyCondition(driver dialect.Name, keys []string) (string, []any) {
	if len(keys) == 0 {
		return "", nil
	}
	conditions := make([]string, 0, len(keys))
	args := make([]any, 0, len(keys))
	switch driver {
	case dialect.PG:
		for _, key := range keys {
			if key == "" {
				continue
			}
			conditions = append(conditions, "NOT (data ? ?)")
			args = append(args, key)
		}
	default:
		for _, key := range keys {
			if key == "" {
				continue
			}
			conditions = append(conditions, "data NOT LIKE ?")
			args = append(args, "%\""+key+"\":%")
		}
	}
	if len(conditions) == 0 {
		return "", nil
	}
	return "(" + strings.Join(conditions, " OR ") + ")", args
}

func enrichedBeforeCondition(driver dialect.Name, before time.Time) (string, []any) {
	if before.IsZero() {
		return "", nil
	}
	before = before.UTC()
	switch driver {
	case dialect.PG:
		return "(data ->> ? IS NULL OR data ->> ? = '' OR (data ->> ?)::timestamptz < ?)",
			[]any{DataKeyEnrichedAt, DataKeyEnrichedAt, DataKeyEnrichedAt, before}
	default:
		return "(data NOT LIKE ? OR data LIKE ?)",
			[]any{"%\"" + DataKeyEnrichedAt + "\":%", "%\"" + DataKeyEnrichedAt + "\":\"\"%"}
	}
}

func matchesEnrichmentFilter(record types.ActivityRecord, missingKeys []string, enrichedBefore *time.Time) bool {
	hasMissing := false
	if len(missingKeys) > 0 {
		for _, key := range missingKeys {
			if key == "" {
				continue
			}
			if _, ok := record.Data[key]; !ok {
				hasMissing = true
				break
			}
		}
	}

	isStale := false
	if enrichedBefore != nil && !enrichedBefore.IsZero() {
		isStale = true
		if value, ok := record.Data[DataKeyEnrichedAt]; ok {
			if parsed, ok := value.(string); ok && parsed != "" {
				if ts, err := time.Parse(time.RFC3339Nano, parsed); err == nil && !ts.Before(enrichedBefore.UTC()) {
					isStale = false
				}
			}
		}
	}

	if len(missingKeys) == 0 && (enrichedBefore == nil || enrichedBefore.IsZero()) {
		return true
	}
	return hasMissing || isStale
}

func toEnrichmentPage(rows []LogEntry) ActivityEnrichmentPage {
	records := make([]types.ActivityRecord, 0, len(rows))
	for i := range rows {
		records = append(records, toActivityRecord(&rows[i]))
	}
	if len(records) == 0 {
		return ActivityEnrichmentPage{Records: records}
	}
	last := records[len(records)-1]
	return ActivityEnrichmentPage{
		Records:    records,
		NextCursor: &ActivityCursor{OccurredAt: last.OccurredAt, ID: last.ID},
	}
}

func normalizeEnrichmentLimit(limit int) int {
	if limit <= 0 {
		return 200
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}

func missingActivityData(existing, updates map[string]any) map[string]any {
	if len(updates) == 0 {
		return nil
	}
	missing := make(map[string]any)
	for key, value := range updates {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		if existing != nil {
			if _, ok := existing[trimmed]; ok {
				continue
			}
		}
		missing[trimmed] = value
	}
	return missing
}

func mergeActivityData(existing, updates map[string]any) map[string]any {
	merged := cloneMap(existing)
	for key, value := range updates {
		merged[key] = value
	}
	return merged
}

func (r *Repository) getDB() *bun.DB {
	if r == nil {
		return nil
	}
	if r.db != nil {
		return r.db
	}
	if provider, ok := r.activityStore.(interface{ DB() *bun.DB }); ok {
		return provider.DB()
	}
	return nil
}
