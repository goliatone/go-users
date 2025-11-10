package command

import (
	"context"
	"errors"
	"fmt"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

// BulkUserTransitionInput applies the same lifecycle change to multiple users.
type BulkUserTransitionInput struct {
	UserIDs     []uuid.UUID
	Target      types.LifecycleState
	Actor       types.ActorRef
	Reason      string
	Metadata    map[string]any
	Scope       types.ScopeFilter
	StopOnError bool
	Results     *[]BulkUserTransitionResult
}

// BulkUserTransitionResult captures the outcome for a single user.
type BulkUserTransitionResult struct {
	UserID uuid.UUID
	Err    error
}

// BulkUserTransitionCommand iterates through the supplied user IDs, reusing the
// single-user lifecycle command to enforce policies uniformly.
type BulkUserTransitionCommand struct {
	lifecycle *UserLifecycleTransitionCommand
}

// NewBulkUserTransitionCommand constructs the bulk handler.
func NewBulkUserTransitionCommand(lifecycle *UserLifecycleTransitionCommand) *BulkUserTransitionCommand {
	return &BulkUserTransitionCommand{
		lifecycle: lifecycle,
	}
}

var _ gocommand.Commander[BulkUserTransitionInput] = (*BulkUserTransitionCommand)(nil)

// Execute transitions each user sequentially. Errors are aggregated.
func (c *BulkUserTransitionCommand) Execute(ctx context.Context, input BulkUserTransitionInput) error {
	if len(input.UserIDs) == 0 {
		return ErrUserIDsRequired
	}
	if input.Actor.ID == uuid.Nil {
		return ErrActorRequired
	}
	if input.Target == "" {
		return ErrLifecycleTargetRequired
	}

	var errs []error
	results := make([]BulkUserTransitionResult, 0, len(input.UserIDs))
	for _, id := range input.UserIDs {
		result := BulkUserTransitionResult{UserID: id}
		err := c.lifecycle.Execute(ctx, UserLifecycleTransitionInput{
			UserID:   id,
			Target:   input.Target,
			Actor:    input.Actor,
			Reason:   input.Reason,
			Metadata: input.Metadata,
			Scope:    input.Scope,
		})
		if err != nil {
			result.Err = err
			errs = append(errs, fmt.Errorf("user %s: %w", id, err))
			if input.StopOnError {
				results = append(results, result)
				break
			}
		}
		results = append(results, result)
	}

	if input.Results != nil {
		*input.Results = append((*input.Results)[:0], results...)
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
