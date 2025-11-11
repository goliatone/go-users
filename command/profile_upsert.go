package command

import (
	"context"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

// ProfileCommandConfig wires dependencies for profile commands.
type ProfileCommandConfig struct {
	Repository types.ProfileRepository
	Hooks      types.Hooks
	Clock      types.Clock
	ScopeGuard scope.Guard
}

// ProfileUpsertInput captures a profile patch request.
type ProfileUpsertInput struct {
	UserID uuid.UUID
	Patch  types.ProfilePatch
	Scope  types.ScopeFilter
	Actor  types.ActorRef
	Result *types.UserProfile
}

// Type implements gocommand.Message.
func (ProfileUpsertInput) Type() string {
	return "command.profile.upsert"
}

// Validate implements gocommand.Message.
func (input ProfileUpsertInput) Validate() error {
	if input.UserID == uuid.Nil {
		return types.ErrUserIDRequired
	}
	if input.Actor.ID == uuid.Nil {
		return ErrActorRequired
	}
	return nil
}

// ProfileUpsertCommand applies profile patches for a user.
type ProfileUpsertCommand struct {
	repo  types.ProfileRepository
	hooks types.Hooks
	clock types.Clock
	guard scope.Guard
}

// NewProfileUpsertCommand constructs the profile command handler.
func NewProfileUpsertCommand(cfg ProfileCommandConfig) *ProfileUpsertCommand {
	return &ProfileUpsertCommand{
		repo:  cfg.Repository,
		hooks: safeHooks(cfg.Hooks),
		clock: safeClock(cfg.Clock),
		guard: safeScopeGuard(cfg.ScopeGuard),
	}
}

var _ gocommand.Commander[ProfileUpsertInput] = (*ProfileUpsertCommand)(nil)

// Execute applies the supplied patch creating the profile when necessary.
func (c *ProfileUpsertCommand) Execute(ctx context.Context, input ProfileUpsertInput) error {
	if c.repo == nil {
		return types.ErrMissingProfileRepository
	}
	if err := input.Validate(); err != nil {
		return err
	}

	scope, err := c.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionProfilesWrite, input.UserID)
	if err != nil {
		return err
	}

	existing, err := c.repo.GetProfile(ctx, input.UserID, scope)
	if err != nil {
		return err
	}
	profile := &types.UserProfile{
		UserID: input.UserID,
		Scope:  scope,
	}
	if existing != nil {
		*profile = *existing
	}
	if profile.CreatedBy == uuid.Nil {
		profile.CreatedBy = input.Actor.ID
	}
	profile.UpdatedBy = input.Actor.ID
	applyProfilePatch(profile, input.Patch)

	updated, err := c.repo.UpsertProfile(ctx, *profile)
	if err != nil {
		return err
	}
	var eventProfile types.UserProfile
	if updated != nil {
		eventProfile = *updated
		if input.Result != nil {
			*input.Result = *updated
		}
	} else {
		eventProfile = *profile
		if input.Result != nil {
			*input.Result = *profile
		}
	}
	emitProfileHook(ctx, c.hooks, types.ProfileEvent{
		UserID:     input.UserID,
		Scope:      scope,
		ActorID:    input.Actor.ID,
		OccurredAt: now(c.clock),
		Profile:    eventProfile,
	})
	return nil
}

func applyProfilePatch(profile *types.UserProfile, patch types.ProfilePatch) {
	if profile == nil {
		return
	}
	if patch.DisplayName != nil {
		profile.DisplayName = *patch.DisplayName
	}
	if patch.AvatarURL != nil {
		profile.AvatarURL = *patch.AvatarURL
	}
	if patch.Locale != nil {
		profile.Locale = *patch.Locale
	}
	if patch.Timezone != nil {
		profile.Timezone = *patch.Timezone
	}
	if patch.Bio != nil {
		profile.Bio = *patch.Bio
	}
	if patch.Contact != nil {
		profile.Contact = cloneMap(patch.Contact)
	}
	if patch.Metadata != nil {
		profile.Metadata = cloneMap(patch.Metadata)
	}
}
