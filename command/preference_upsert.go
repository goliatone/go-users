package command

import (
	"context"
	"strings"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

// PreferenceCommandConfig wires dependencies for preference commands.
type PreferenceCommandConfig struct {
	Repository types.PreferenceRepository
	Hooks      types.Hooks
	Clock      types.Clock
	ScopeGuard scope.Guard
}

// PreferenceUpsertInput captures a preference mutation payload.
type PreferenceUpsertInput struct {
	UserID uuid.UUID
	Scope  types.ScopeFilter
	Level  types.PreferenceLevel
	Key    string
	Value  map[string]any
	Actor  types.ActorRef
	Result *types.PreferenceRecord
}

// PreferenceUpsertCommand upserts a scoped preference record.
type PreferenceUpsertCommand struct {
	repo  types.PreferenceRepository
	hooks types.Hooks
	clock types.Clock
	guard scope.Guard
}

// NewPreferenceUpsertCommand constructs the handler.
func NewPreferenceUpsertCommand(cfg PreferenceCommandConfig) *PreferenceUpsertCommand {
	return &PreferenceUpsertCommand{
		repo:  cfg.Repository,
		hooks: safeHooks(cfg.Hooks),
		clock: safeClock(cfg.Clock),
		guard: safeScopeGuard(cfg.ScopeGuard),
	}
}

var _ gocommand.Commander[PreferenceUpsertInput] = (*PreferenceUpsertCommand)(nil)

// Execute validates and persists the preference payload.
func (c *PreferenceUpsertCommand) Execute(ctx context.Context, input PreferenceUpsertInput) error {
	if c.repo == nil {
		return types.ErrMissingPreferenceRepository
	}
	if strings.TrimSpace(input.Key) == "" {
		return ErrPreferenceKeyRequired
	}
	if input.Value == nil {
		return ErrPreferenceValueRequired
	}
	if needsUser(input.Level) && input.UserID == uuid.Nil {
		return types.ErrUserIDRequired
	}
	if input.Actor.ID == uuid.Nil {
		return ErrActorRequired
	}

	scope, err := c.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionPreferencesWrite, input.UserID)
	if err != nil {
		return err
	}

	level := input.Level
	if level == "" {
		level = types.PreferenceLevelUser
	}

	record := types.PreferenceRecord{
		UserID:    input.UserID,
		Scope:     scope,
		Level:     level,
		Key:       strings.TrimSpace(input.Key),
		Value:     cloneMap(input.Value),
		UpdatedBy: input.Actor.ID,
		CreatedBy: input.Actor.ID,
	}
	saved, err := c.repo.UpsertPreference(ctx, record)
	if err != nil {
		return err
	}
	if input.Result != nil && saved != nil {
		*input.Result = *saved
	}
	emitPreferenceHook(ctx, c.hooks, types.PreferenceEvent{
		UserID:     input.UserID,
		Scope:      scope,
		Key:        record.Key,
		Action:     "preference.upsert",
		ActorID:    input.Actor.ID,
		OccurredAt: now(c.clock),
	})
	return nil
}

func needsUser(level types.PreferenceLevel) bool {
	return level == "" || level == types.PreferenceLevelUser
}
