package command

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

// PreferenceUpsertManyInput captures a bulk preference upsert payload.
type PreferenceUpsertManyInput struct {
	UserID  uuid.UUID
	Scope   types.ScopeFilter
	Level   types.PreferenceLevel
	Values  map[string]any
	Actor   types.ActorRef
	Mode    types.PreferenceBulkMode
	Results *[]types.PreferenceBulkUpsertResult
}

// Type implements gocommand.Message.
func (PreferenceUpsertManyInput) Type() string {
	return "command.preference.upsert_many"
}

// Validate implements gocommand.Message.
func (input PreferenceUpsertManyInput) Validate() error {
	level, err := normalizePreferenceLevel(input.Level)
	if err != nil {
		return err
	}
	if level == types.PreferenceLevelUser && input.UserID == uuid.Nil {
		return types.ErrUserIDRequired
	}
	if input.Actor.ID == uuid.Nil {
		return ErrActorRequired
	}
	if len(input.Values) == 0 {
		return ErrPreferenceValuesRequired
	}
	if _, err := normalizePreferenceBulkMode(input.Mode); err != nil {
		return err
	}
	for key, value := range input.Values {
		if strings.TrimSpace(key) == "" {
			return ErrPreferenceKeyRequired
		}
		if value == nil {
			return ErrPreferenceValueRequired
		}
	}
	return nil
}

// PreferenceUpsertManyCommand upserts many scoped preference entries.
type PreferenceUpsertManyCommand struct {
	repo  types.PreferenceRepository
	hooks types.Hooks
	clock types.Clock
	guard scope.Guard
}

// NewPreferenceUpsertManyCommand constructs the handler.
func NewPreferenceUpsertManyCommand(cfg PreferenceCommandConfig) *PreferenceUpsertManyCommand {
	return &PreferenceUpsertManyCommand{
		repo:  cfg.Repository,
		hooks: safeHooks(cfg.Hooks),
		clock: safeClock(cfg.Clock),
		guard: safeScopeGuard(cfg.ScopeGuard),
	}
}

var _ gocommand.Commander[PreferenceUpsertManyInput] = (*PreferenceUpsertManyCommand)(nil)

// Execute validates and persists many preference entries.
func (c *PreferenceUpsertManyCommand) Execute(ctx context.Context, input PreferenceUpsertManyInput) error {
	if c.repo == nil {
		return types.ErrMissingPreferenceRepository
	}
	if err := input.Validate(); err != nil {
		return err
	}
	level, err := normalizePreferenceLevel(input.Level)
	if err != nil {
		return err
	}
	mode, err := normalizePreferenceBulkMode(input.Mode)
	if err != nil {
		return err
	}
	scope, err := c.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionPreferencesWrite, input.UserID)
	if err != nil {
		return err
	}

	values, keys, err := normalizeBulkValues(input.Values)
	if err != nil {
		return err
	}
	records := make([]types.PreferenceRecord, 0, len(keys))
	for _, key := range keys {
		payload, payloadErr := coercePreferencePayload(values[key])
		if payloadErr != nil {
			return payloadErr
		}
		records = append(records, types.PreferenceRecord{
			UserID:    input.UserID,
			Scope:     scope,
			Level:     level,
			Key:       key,
			Value:     payload,
			UpdatedBy: input.Actor.ID,
			CreatedBy: input.Actor.ID,
		})
	}

	results := make([]types.PreferenceBulkUpsertResult, 0, len(records))
	switch mode {
	case types.PreferenceBulkModeTransactional:
		results, err = c.executeTransactional(ctx, input, records)
		if err != nil {
			if input.Results != nil {
				*input.Results = append((*input.Results)[:0], results...)
			}
			return err
		}
	case types.PreferenceBulkModeBestEffort:
		var errs []error
		for _, record := range records {
			saved, upsertErr := c.repo.UpsertPreference(ctx, record)
			result := types.PreferenceBulkUpsertResult{Key: record.Key}
			if upsertErr != nil {
				result.Err = upsertErr
				errs = append(errs, fmt.Errorf("preference %q: %w", record.Key, upsertErr))
			} else {
				result.Record = saved
				emitPreferenceHook(ctx, c.hooks, types.PreferenceEvent{
					UserID:     input.UserID,
					Scope:      scope,
					Key:        record.Key,
					Action:     "preference.upsert",
					ActorID:    input.Actor.ID,
					OccurredAt: now(c.clock),
				})
			}
			results = append(results, result)
		}
		if input.Results != nil {
			*input.Results = append((*input.Results)[:0], results...)
		}
		if len(errs) > 0 {
			return errors.Join(errs...)
		}
	}

	if input.Results != nil {
		*input.Results = append((*input.Results)[:0], results...)
	}
	return nil
}

func (c *PreferenceUpsertManyCommand) executeTransactional(ctx context.Context, input PreferenceUpsertManyInput, records []types.PreferenceRecord) ([]types.PreferenceBulkUpsertResult, error) {
	bulkRepo, ok := c.repo.(types.PreferenceBulkRepository)
	if !ok {
		return nil, types.ErrPreferenceBulkTransactionalUnsupported
	}
	saved, err := bulkRepo.UpsertManyPreferences(ctx, records, types.PreferenceBulkModeTransactional)
	if err != nil {
		return nil, err
	}
	savedByKey := make(map[string]*types.PreferenceRecord, len(saved))
	for i := range saved {
		rec := saved[i]
		copy := rec
		savedByKey[rec.Key] = &copy
	}
	results := make([]types.PreferenceBulkUpsertResult, 0, len(records))
	for _, record := range records {
		res := types.PreferenceBulkUpsertResult{Key: record.Key}
		if savedRec, ok := savedByKey[record.Key]; ok {
			res.Record = savedRec
			emitPreferenceHook(ctx, c.hooks, types.PreferenceEvent{
				UserID:     input.UserID,
				Scope:      record.Scope,
				Key:        record.Key,
				Action:     "preference.upsert",
				ActorID:    input.Actor.ID,
				OccurredAt: now(c.clock),
			})
		}
		results = append(results, res)
	}
	return results, nil
}

func normalizeBulkValues(values map[string]any) (map[string]any, []string, error) {
	normalized := make(map[string]any, len(values))
	seen := make(map[string]string, len(values))
	for key, value := range values {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			return nil, nil, ErrPreferenceKeyRequired
		}
		lower := strings.ToLower(trimmed)
		if existing, ok := seen[lower]; ok {
			return nil, nil, fmt.Errorf("%w: %q conflicts with %q", ErrPreferenceDuplicateKey, trimmed, existing)
		}
		seen[lower] = trimmed
		normalized[trimmed] = value
	}
	keys := make([]string, 0, len(normalized))
	for key := range normalized {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return normalized, keys, nil
}
