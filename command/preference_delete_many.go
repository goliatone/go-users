package command

import (
	"context"
	"errors"
	"fmt"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

// PreferenceDeleteManyInput captures a bulk preference delete payload.
type PreferenceDeleteManyInput struct {
	UserID  uuid.UUID
	Scope   types.ScopeFilter
	Level   types.PreferenceLevel
	Keys    []string
	Actor   types.ActorRef
	Mode    types.PreferenceBulkMode
	Results *[]types.PreferenceBulkDeleteResult
}

// Type implements gocommand.Message.
func (PreferenceDeleteManyInput) Type() string {
	return "command.preference.delete_many"
}

// Validate implements gocommand.Message.
func (input PreferenceDeleteManyInput) Validate() error {
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
	keys := normalizePreferenceKeys(input.Keys)
	if len(keys) == 0 {
		return ErrPreferenceKeysRequired
	}
	if _, err := normalizePreferenceBulkMode(input.Mode); err != nil {
		return err
	}
	return nil
}

// PreferenceDeleteManyCommand deletes many scoped preference entries.
type PreferenceDeleteManyCommand struct {
	repo  types.PreferenceRepository
	hooks types.Hooks
	clock types.Clock
	guard scope.Guard
}

// NewPreferenceDeleteManyCommand constructs the handler.
func NewPreferenceDeleteManyCommand(cfg PreferenceCommandConfig) *PreferenceDeleteManyCommand {
	return &PreferenceDeleteManyCommand{
		repo:  cfg.Repository,
		hooks: safeHooks(cfg.Hooks),
		clock: safeClock(cfg.Clock),
		guard: safeScopeGuard(cfg.ScopeGuard),
	}
}

var _ gocommand.Commander[PreferenceDeleteManyInput] = (*PreferenceDeleteManyCommand)(nil)

// Execute validates and deletes preference entries.
func (c *PreferenceDeleteManyCommand) Execute(ctx context.Context, input PreferenceDeleteManyInput) error {
	if c.repo == nil {
		return types.ErrMissingPreferenceRepository
	}
	if err := input.Validate(); err != nil {
		return err
	}
	bulk, err := c.preparePreferenceDeleteMany(ctx, input)
	if err != nil {
		return err
	}
	var results []types.PreferenceBulkDeleteResult
	switch bulk.mode {
	case types.PreferenceBulkModeTransactional:
		results, err = c.deleteManyTransactional(ctx, input, bulk)
	case types.PreferenceBulkModeBestEffort:
		results, err = c.deleteManyBestEffort(ctx, input, bulk)
	}
	if input.Results != nil {
		*input.Results = append((*input.Results)[:0], results...)
	}
	return err
}

func (c *PreferenceDeleteManyCommand) preparePreferenceDeleteMany(ctx context.Context, input PreferenceDeleteManyInput) (preferenceBulkContext, error) {
	level, err := normalizePreferenceLevel(input.Level)
	if err != nil {
		return preferenceBulkContext{}, err
	}
	mode, err := normalizePreferenceBulkMode(input.Mode)
	if err != nil {
		return preferenceBulkContext{}, err
	}
	scope, err := c.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionPreferencesWrite, input.UserID)
	if err != nil {
		return preferenceBulkContext{}, err
	}
	return preferenceBulkContext{
		level: level,
		mode:  mode,
		scope: scope,
		keys:  normalizePreferenceKeys(input.Keys),
	}, nil
}

func (c *PreferenceDeleteManyCommand) deleteManyTransactional(ctx context.Context, input PreferenceDeleteManyInput, bulk preferenceBulkContext) ([]types.PreferenceBulkDeleteResult, error) {
	bulkRepo, ok := c.repo.(types.PreferenceBulkRepository)
	if !ok {
		return nil, types.ErrPreferenceBulkTransactionalUnsupported
	}
	if err := bulkRepo.DeleteManyPreferences(ctx, input.UserID, bulk.scope, bulk.level, bulk.keys, types.PreferenceBulkModeTransactional); err != nil {
		return nil, err
	}
	results := make([]types.PreferenceBulkDeleteResult, 0, len(bulk.keys))
	for _, key := range bulk.keys {
		results = append(results, types.PreferenceBulkDeleteResult{Key: key})
		c.emitDeleteManyHook(ctx, input, bulk.scope, key)
	}
	return results, nil
}

func (c *PreferenceDeleteManyCommand) deleteManyBestEffort(ctx context.Context, input PreferenceDeleteManyInput, bulk preferenceBulkContext) ([]types.PreferenceBulkDeleteResult, error) {
	results := make([]types.PreferenceBulkDeleteResult, 0, len(bulk.keys))
	var errs []error
	for _, key := range bulk.keys {
		delErr := c.repo.DeletePreference(ctx, input.UserID, bulk.scope, bulk.level, key)
		result := types.PreferenceBulkDeleteResult{Key: key}
		if delErr != nil {
			result.Err = delErr
			errs = append(errs, fmt.Errorf("preference %q: %w", key, delErr))
		} else {
			c.emitDeleteManyHook(ctx, input, bulk.scope, key)
		}
		results = append(results, result)
	}
	return results, errors.Join(errs...)
}

func (c *PreferenceDeleteManyCommand) emitDeleteManyHook(ctx context.Context, input PreferenceDeleteManyInput, scope types.ScopeFilter, key string) {
	emitPreferenceHook(ctx, c.hooks, types.PreferenceEvent{
		UserID:     input.UserID,
		Scope:      scope,
		Key:        key,
		Action:     "preference.delete",
		ActorID:    input.Actor.ID,
		OccurredAt: now(c.clock),
	})
}
