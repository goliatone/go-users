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

	keys := normalizePreferenceKeys(input.Keys)
	results := make([]types.PreferenceBulkDeleteResult, 0, len(keys))
	switch mode {
	case types.PreferenceBulkModeTransactional:
		bulkRepo, ok := c.repo.(types.PreferenceBulkRepository)
		if !ok {
			return types.ErrPreferenceBulkTransactionalUnsupported
		}
		if err := bulkRepo.DeleteManyPreferences(ctx, input.UserID, scope, level, keys, types.PreferenceBulkModeTransactional); err != nil {
			return err
		}
		for _, key := range keys {
			results = append(results, types.PreferenceBulkDeleteResult{Key: key})
			emitPreferenceHook(ctx, c.hooks, types.PreferenceEvent{
				UserID:     input.UserID,
				Scope:      scope,
				Key:        key,
				Action:     "preference.delete",
				ActorID:    input.Actor.ID,
				OccurredAt: now(c.clock),
			})
		}
	case types.PreferenceBulkModeBestEffort:
		var errs []error
		for _, key := range keys {
			delErr := c.repo.DeletePreference(ctx, input.UserID, scope, level, key)
			result := types.PreferenceBulkDeleteResult{Key: key}
			if delErr != nil {
				result.Err = delErr
				errs = append(errs, fmt.Errorf("preference %q: %w", key, delErr))
			} else {
				emitPreferenceHook(ctx, c.hooks, types.PreferenceEvent{
					UserID:     input.UserID,
					Scope:      scope,
					Key:        key,
					Action:     "preference.delete",
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
