// Package activityenrichment provides a cron-friendly command for activity backfills.
package activityenrichment

import (
	"context"
	"errors"
	"strings"
	"time"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/activity"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

const (
	// DefaultSchedule is the fallback cron expression when none is configured.
	DefaultSchedule = "0 * * * *"
	// DefaultBatchSize matches the repository default for enrichment selection.
	DefaultBatchSize = 200
	// MaxBatchSize caps the batch size to protect the resolver pipeline.
	MaxBatchSize = 1000
)

const activityEnrichmentMessageType = "command.activity.enrichment"

var (
	// ErrMissingEnrichmentQuery indicates the enrichment query dependency is missing.
	ErrMissingEnrichmentQuery = errors.New("go-users: missing activity enrichment query")
	// ErrMissingEnrichmentStore indicates the enrichment store dependency is missing.
	ErrMissingEnrichmentStore = errors.New("go-users: missing activity enrichment store")
	// ErrMissingCommand indicates the command instance was not provided.
	ErrMissingCommand = errors.New("go-users: activity enrichment command required")
)

// Config wires the activity enrichment command dependencies and defaults.
type Config struct {
	Schedule         string
	BatchSize        int
	EnrichedAtCutoff time.Duration
	// Scope defaults scheduled runs; zero scope means global backfill.
	Scope              types.ScopeFilter
	ActivityRepository types.ActivityRepository
	EnrichmentQuery    activity.ActivityEnrichmentQuery
	EnrichmentStore    activity.ActivityEnrichmentStore
	Enricher           activity.ActivityEnricher
	ActorResolver      activity.ActorResolver
	ObjectResolver     activity.ObjectResolver
	Clock              types.Clock
	Logger             types.Logger
}

// Input describes a single enrichment run.
type Input struct {
	// Scope overrides the configured scope for this run.
	Scope          types.ScopeFilter
	BatchSize      int
	EnrichedBefore *time.Time
}

type enrichmentStats struct {
	Processed int
	Enriched  int
	Failed    int
	Skipped   int
}

func (s *enrichmentStats) add(other enrichmentStats) {
	s.Processed += other.Processed
	s.Enriched += other.Enriched
	s.Failed += other.Failed
	s.Skipped += other.Skipped
}

// Type implements gocommand.Message.
func (Input) Type() string {
	return activityEnrichmentMessageType
}

// Validate implements gocommand.Message.
func (Input) Validate() error {
	return nil
}

// Command schedules and executes activity enrichment backfills.
type Command struct {
	schedule         string
	batchSize        int
	enrichedAtCutoff time.Duration
	scope            types.ScopeFilter
	repo             types.ActivityRepository
	query            activity.ActivityEnrichmentQuery
	store            activity.ActivityEnrichmentStore
	enricher         activity.ActivityEnricher
	actorResolver    activity.ActorResolver
	objectResolver   activity.ObjectResolver
	clock            types.Clock
	logger           types.Logger
}

// New constructs an activity enrichment command with the supplied configuration.
func New(cfg Config) *Command {
	schedule := normalizeSchedule(cfg.Schedule)
	batchSize := normalizeBatchSize(cfg.BatchSize)
	cutoff := cfg.EnrichedAtCutoff
	if cutoff < 0 {
		cutoff = 0
	}

	clock := cfg.Clock
	if clock == nil {
		clock = types.SystemClock{}
	}
	logger := cfg.Logger
	if logger == nil {
		logger = types.NopLogger{}
	}

	store := cfg.EnrichmentStore
	query := cfg.EnrichmentQuery
	if cfg.ActivityRepository != nil {
		if store == nil {
			if cast, ok := cfg.ActivityRepository.(activity.ActivityEnrichmentStore); ok {
				store = cast
			}
		}
		if query == nil {
			if cast, ok := cfg.ActivityRepository.(activity.ActivityEnrichmentQuery); ok {
				query = cast
			}
		}
	}

	return &Command{
		schedule:         schedule,
		batchSize:        batchSize,
		enrichedAtCutoff: cutoff,
		scope:            cfg.Scope.Clone(),
		repo:             cfg.ActivityRepository,
		query:            query,
		store:            store,
		enricher:         cfg.Enricher,
		actorResolver:    cfg.ActorResolver,
		objectResolver:   cfg.ObjectResolver,
		clock:            clock,
		logger:           logger,
	}
}

var _ gocommand.Commander[Input] = (*Command)(nil)
var _ gocommand.CronCommand = (*Command)(nil)

// Execute validates dependencies and prepares the backfill run.
func (c *Command) Execute(ctx context.Context, input Input) error {
	if c == nil {
		return ErrMissingCommand
	}
	if c.logger == nil {
		c.logger = types.NopLogger{}
	}
	if c.query == nil {
		return ErrMissingEnrichmentQuery
	}
	if c.store == nil {
		return ErrMissingEnrichmentStore
	}
	if err := input.Validate(); err != nil {
		return err
	}
	filter := c.buildFilter(input)
	return c.run(ctx, filter)
}

// CronHandler implements gocommand.CronCommand.
func (c *Command) CronHandler() func() error {
	return func() error {
		if c == nil {
			return ErrMissingCommand
		}
		input := Input{
			Scope:          c.scope.Clone(),
			BatchSize:      c.batchSize,
			EnrichedBefore: c.enrichedBefore(),
		}
		return c.Execute(context.Background(), input)
	}
}

// CronOptions implements gocommand.CronCommand.
func (c *Command) CronOptions() gocommand.HandlerConfig {
	schedule := DefaultSchedule
	if c != nil {
		schedule = normalizeSchedule(c.schedule)
	}
	return gocommand.HandlerConfig{Expression: schedule}
}

func (c *Command) run(ctx context.Context, filter activity.ActivityEnrichmentFilter) error {
	if ctx == nil {
		return errors.New("activity enrichment requires context")
	}
	filter = c.normalizeFilter(filter)
	cursor := filter.Cursor
	summary := enrichmentStats{}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		filter.Cursor = cursor
		page, err := c.query.ListActivityForEnrichment(ctx, filter)
		if err != nil {
			return err
		}
		if len(page.Records) == 0 {
			c.logSummary(summary)
			return nil
		}
		batchStats, err := c.enrichBatch(ctx, page.Records, filter.EnrichedBefore)
		if err != nil {
			return err
		}
		summary.add(batchStats)
		if page.NextCursor == nil || len(page.Records) < filter.Limit {
			c.logSummary(summary)
			return nil
		}
		cursor = page.NextCursor
	}
}

func (c *Command) buildFilter(input Input) activity.ActivityEnrichmentFilter {
	scope := resolveScope(input.Scope, c.scope)
	batchSize := resolveBatchSize(input.BatchSize, c.batchSize)
	enrichedBefore := input.EnrichedBefore
	if enrichedBefore == nil {
		enrichedBefore = c.enrichedBefore()
	}
	return activity.ActivityEnrichmentFilter{
		Scope:          scope,
		EnrichedBefore: enrichedBefore,
		Limit:          batchSize,
	}
}

func (c *Command) enrichedBefore() *time.Time {
	if c == nil || c.enrichedAtCutoff <= 0 {
		return nil
	}
	now := time.Now().UTC()
	if c.clock != nil {
		now = c.clock.Now().UTC()
	}
	cutoff := now.Add(-c.enrichedAtCutoff)
	return &cutoff
}

func resolveScope(input, fallback types.ScopeFilter) types.ScopeFilter {
	if !isScopeEmpty(input) {
		return input.Clone()
	}
	return fallback.Clone()
}

func isScopeEmpty(scope types.ScopeFilter) bool {
	return scope.TenantID == uuid.Nil && scope.OrgID == uuid.Nil && len(scope.Labels) == 0
}

func resolveBatchSize(input, fallback int) int {
	if input > 0 {
		return normalizeBatchSize(input)
	}
	if fallback > 0 {
		return normalizeBatchSize(fallback)
	}
	return DefaultBatchSize
}

func normalizeBatchSize(batchSize int) int {
	if batchSize <= 0 {
		return DefaultBatchSize
	}
	if batchSize > MaxBatchSize {
		return MaxBatchSize
	}
	return batchSize
}

func normalizeSchedule(schedule string) string {
	schedule = strings.TrimSpace(schedule)
	if schedule == "" {
		return DefaultSchedule
	}
	return schedule
}

func (c *Command) normalizeFilter(filter activity.ActivityEnrichmentFilter) activity.ActivityEnrichmentFilter {
	filter.Limit = resolveBatchSize(filter.Limit, c.batchSize)
	if len(filter.MissingKeys) == 0 {
		filter.MissingKeys = defaultMissingKeys()
	}
	return filter
}

func (c *Command) logSummary(summary enrichmentStats) {
	if c == nil || c.logger == nil {
		return
	}
	c.logger.Info(
		"activity enrichment summary",
		"processed", summary.Processed,
		"enriched", summary.Enriched,
		"failed", summary.Failed,
		"skipped", summary.Skipped,
	)
}

func defaultMissingKeys() []string {
	return []string{activity.DataKeyActorDisplay, activity.DataKeyObjectDisplay}
}

func (c *Command) enrichBatch(ctx context.Context, records []types.ActivityRecord, enrichedBefore *time.Time) (enrichmentStats, error) {
	stats := enrichmentStats{}
	if err := ctx.Err(); err != nil {
		return stats, err
	}
	groups := groupRecordsByTenant(records)
	for tenantID, batch := range groups {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		actorInfo, err := c.resolveActors(ctx, tenantID, batch)
		if err != nil {
			c.logger.Error("activity enrichment actor resolver failed", err, "tenant_id", tenantID, "records", len(batch))
			actorInfo = nil
		}
		objectInfo, err := c.resolveObjects(ctx, tenantID, batch)
		if err != nil {
			c.logger.Error("activity enrichment object resolver failed", err, "tenant_id", tenantID, "records", len(batch))
			objectInfo = nil
		}
		for _, record := range batch {
			if err := ctx.Err(); err != nil {
				return stats, err
			}
			stats.Processed++
			info := actorInfo[record.ActorID]
			objectType := strings.TrimSpace(record.ObjectType)
			objectID := strings.TrimSpace(record.ObjectID)
			objects := objectInfo[objectType]
			obj := objects[objectID]
			updated, err := c.enrichRecord(ctx, record, info, obj, enrichedBefore)
			if err != nil {
				stats.Failed++
				c.logger.Error(
					"activity enrichment record failed",
					err,
					"record_id", record.ID,
					"tenant_id", record.TenantID,
					"actor_id", record.ActorID,
					"object_type", record.ObjectType,
					"object_id", record.ObjectID,
				)
				continue
			}
			if updated {
				stats.Enriched++
			} else {
				stats.Skipped++
			}
		}
	}
	return stats, nil
}

func groupRecordsByTenant(records []types.ActivityRecord) map[uuid.UUID][]types.ActivityRecord {
	groups := make(map[uuid.UUID][]types.ActivityRecord)
	for _, record := range records {
		tenantID := record.TenantID
		groups[tenantID] = append(groups[tenantID], record)
	}
	return groups
}

func (c *Command) resolveActors(ctx context.Context, tenantID uuid.UUID, records []types.ActivityRecord) (map[uuid.UUID]activity.ActorInfo, error) {
	if c.actorResolver == nil {
		return nil, nil
	}
	ids := uniqueActorIDs(records)
	if len(ids) == 0 {
		return nil, nil
	}
	meta := activity.ResolveContext{TenantID: tenantID}
	return c.actorResolver.ResolveActors(ctx, ids, meta)
}

func (c *Command) resolveObjects(ctx context.Context, tenantID uuid.UUID, records []types.ActivityRecord) (map[string]map[string]activity.ObjectInfo, error) {
	if c.objectResolver == nil {
		return nil, nil
	}
	byType := make(map[string][]string)
	for _, record := range records {
		objectType := strings.TrimSpace(record.ObjectType)
		if objectType == "" {
			continue
		}
		objectID := strings.TrimSpace(record.ObjectID)
		if objectID == "" {
			continue
		}
		byType[objectType] = append(byType[objectType], objectID)
	}
	meta := activity.ResolveContext{TenantID: tenantID}
	resolved := make(map[string]map[string]activity.ObjectInfo, len(byType))
	for objectType, ids := range byType {
		unique := uniqueStrings(ids)
		if len(unique) == 0 {
			continue
		}
		objects, err := c.objectResolver.ResolveObjects(ctx, objectType, unique, meta)
		if err != nil {
			return nil, err
		}
		if len(objects) > 0 {
			resolved[objectType] = objects
		}
	}
	return resolved, nil
}

func (c *Command) enrichRecord(ctx context.Context, record types.ActivityRecord, actorInfo activity.ActorInfo, objectInfo activity.ObjectInfo, enrichedBefore *time.Time) (bool, error) {
	enriched := applyResolvedInfo(record, actorInfo, objectInfo)
	if c.enricher != nil {
		var err error
		enriched, err = c.enricher.Enrich(ctx, enriched)
		if err != nil {
			return false, err
		}
	}

	missing := missingData(record.Data, enriched.Data)
	isStale := recordIsStale(record, enrichedBefore)
	if len(missing) == 0 && !isStale {
		return false, nil
	}

	if len(missing) > 0 || isStale {
		enriched = activity.StampEnrichment(enriched, clockNow(c.clock), "")
	}

	forceKeys := []string{activity.DataKeyEnrichedAt, activity.DataKeyEnricherVersion}
	if err := c.updateActivityData(ctx, record.ID, enriched.Data, forceKeys); err != nil {
		return false, err
	}
	return true, nil
}

func (c *Command) updateActivityData(ctx context.Context, id uuid.UUID, data map[string]any, forceKeys []string) error {
	if store, ok := c.store.(activity.ActivityEnrichmentStoreWithOptions); ok {
		return store.UpdateActivityDataWithOptions(ctx, id, data, activity.ActivityEnrichmentUpdateOptions{ForceKeys: forceKeys})
	}
	return c.store.UpdateActivityData(ctx, id, data)
}

func applyResolvedInfo(record types.ActivityRecord, actorInfo activity.ActorInfo, objectInfo activity.ObjectInfo) types.ActivityRecord {
	out := record
	out.Data = cloneMap(record.Data)

	if record.ActorID != uuid.Nil {
		setIfMissing(out.Data, activity.DataKeyActorID, record.ActorID.String())
	} else if actorInfo.ID != uuid.Nil {
		setIfMissing(out.Data, activity.DataKeyActorID, actorInfo.ID.String())
	}
	if actorInfo.Display != "" {
		setIfMissing(out.Data, activity.DataKeyActorDisplay, actorInfo.Display)
	}
	if actorInfo.Email != "" {
		setIfMissing(out.Data, activity.DataKeyActorEmail, actorInfo.Email)
	}
	if actorInfo.Type != "" {
		setIfMissing(out.Data, activity.DataKeyActorType, actorInfo.Type)
	}

	if record.ObjectID != "" {
		setIfMissing(out.Data, activity.DataKeyObjectID, record.ObjectID)
	} else if objectInfo.ID != "" {
		setIfMissing(out.Data, activity.DataKeyObjectID, objectInfo.ID)
	}
	hasObjectInfo := objectInfo.ID != "" || objectInfo.Type != "" || objectInfo.Display != "" || objectInfo.Deleted
	if objectInfo.Display != "" {
		setIfMissing(out.Data, activity.DataKeyObjectDisplay, objectInfo.Display)
	}
	if objectInfo.Type != "" {
		setIfMissing(out.Data, activity.DataKeyObjectType, objectInfo.Type)
	} else if record.ObjectType != "" {
		setIfMissing(out.Data, activity.DataKeyObjectType, record.ObjectType)
	}
	if hasObjectInfo {
		setIfMissing(out.Data, activity.DataKeyObjectDeleted, objectInfo.Deleted)
	}
	return out
}

func missingData(existing, updates map[string]any) map[string]any {
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

func recordIsStale(record types.ActivityRecord, enrichedBefore *time.Time) bool {
	if enrichedBefore == nil || enrichedBefore.IsZero() {
		return false
	}
	threshold := enrichedBefore.UTC()
	value, ok := record.Data[activity.DataKeyEnrichedAt]
	if !ok {
		return true
	}
	parsed, ok := value.(string)
	if !ok || strings.TrimSpace(parsed) == "" {
		return true
	}
	ts, err := time.Parse(time.RFC3339Nano, parsed)
	if err != nil {
		return true
	}
	return ts.Before(threshold)
}

func uniqueActorIDs(records []types.ActivityRecord) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(records))
	for _, record := range records {
		if record.ActorID == uuid.Nil {
			continue
		}
		seen[record.ActorID] = struct{}{}
	}
	out := make([]uuid.UUID, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	return out
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
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

func clockNow(clock types.Clock) time.Time {
	if clock == nil {
		return time.Now().UTC()
	}
	return clock.Now().UTC()
}

func setIfMissing(data map[string]any, key string, value any) {
	if data == nil {
		return
	}
	if key == "" || value == nil {
		return
	}
	if _, ok := data[key]; ok {
		return
	}
	data[key] = value
}
