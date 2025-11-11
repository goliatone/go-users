package command

import (
	"context"
	"strings"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

// PreferenceDeleteInput captures the delete payload.
type PreferenceDeleteInput struct {
	UserID uuid.UUID
	Scope  types.ScopeFilter
	Level  types.PreferenceLevel
	Key    string
	Actor  types.ActorRef
}

// Type implements gocommand.Message.
func (PreferenceDeleteInput) Type() string {
	return "command.preference.delete"
}

// Validate implements gocommand.Message.
func (input PreferenceDeleteInput) Validate() error {
	if strings.TrimSpace(input.Key) == "" {
		return ErrPreferenceKeyRequired
	}
	if needsUser(input.Level) && input.UserID == uuid.Nil {
		return types.ErrUserIDRequired
	}
	if input.Actor.ID == uuid.Nil {
		return ErrActorRequired
	}
	return nil
}

// PreferenceDeleteCommand removes a scoped preference entry.
type PreferenceDeleteCommand struct {
	repo  types.PreferenceRepository
	hooks types.Hooks
	clock types.Clock
	guard scope.Guard
}

// NewPreferenceDeleteCommand constructs the delete handler.
func NewPreferenceDeleteCommand(cfg PreferenceCommandConfig) *PreferenceDeleteCommand {
	return &PreferenceDeleteCommand{
		repo:  cfg.Repository,
		hooks: safeHooks(cfg.Hooks),
		clock: safeClock(cfg.Clock),
		guard: safeScopeGuard(cfg.ScopeGuard),
	}
}

var _ gocommand.Commander[PreferenceDeleteInput] = (*PreferenceDeleteCommand)(nil)

// Execute removes the preference entry for the supplied key.
func (c *PreferenceDeleteCommand) Execute(ctx context.Context, input PreferenceDeleteInput) error {
	if c.repo == nil {
		return types.ErrMissingPreferenceRepository
	}
	if err := input.Validate(); err != nil {
		return err
	}
	level := input.Level
	if level == "" {
		level = types.PreferenceLevelUser
	}
	scope, err := c.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionPreferencesWrite, input.UserID)
	if err != nil {
		return err
	}

	if err := c.repo.DeletePreference(ctx, input.UserID, scope, level, strings.TrimSpace(input.Key)); err != nil {
		return err
	}
	emitPreferenceHook(ctx, c.hooks, types.PreferenceEvent{
		UserID:     input.UserID,
		Scope:      scope,
		Key:        strings.TrimSpace(input.Key),
		Action:     "preference.delete",
		ActorID:    input.Actor.ID,
		OccurredAt: now(c.clock),
	})
	return nil
}
