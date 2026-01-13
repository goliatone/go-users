package command

import (
	"context"
	"errors"

	gocommand "github.com/goliatone/go-command"
	goerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

// BulkUserImportInput applies user creation in bulk without file parsing.
type BulkUserImportInput struct {
	Users           []*types.AuthUser
	Actor           types.ActorRef
	Scope           types.ScopeFilter
	DefaultStatus   types.LifecycleState
	ContinueOnError bool
	DryRun          bool
	Results         *[]BulkUserImportResult
}

// Type implements gocommand.Message.
func (BulkUserImportInput) Type() string {
	return "command.user.import.bulk"
}

// Validate implements gocommand.Message.
func (input BulkUserImportInput) Validate() error {
	switch {
	case len(input.Users) == 0:
		return ErrUsersRequired
	case input.Actor.ID == uuid.Nil:
		return ErrActorRequired
	default:
		return nil
	}
}

// BulkUserImportResult captures the outcome for a single record.
type BulkUserImportResult struct {
	Index  int
	UserID uuid.UUID
	Email  string
	Status types.LifecycleState
	Err    error
}

// BulkUserImportCommand imports users in bulk, reusing the create command.
type BulkUserImportCommand struct {
	create *UserCreateCommand
}

// NewBulkUserImportCommand constructs the bulk import handler.
func NewBulkUserImportCommand(create *UserCreateCommand) *BulkUserImportCommand {
	return &BulkUserImportCommand{
		create: create,
	}
}

var _ gocommand.Commander[BulkUserImportInput] = (*BulkUserImportCommand)(nil)

// Execute imports each user sequentially, recording per-record results.
func (c *BulkUserImportCommand) Execute(ctx context.Context, input BulkUserImportInput) error {
	if c == nil || c.create == nil {
		return goerrors.New("go-users: bulk user import requires user create command", goerrors.CategoryInternal).
			WithCode(goerrors.CodeInternal)
	}
	if err := input.Validate(); err != nil {
		return err
	}

	results := make([]BulkUserImportResult, 0, len(input.Users))
	var errs []error

	for idx, user := range input.Users {
		result := BulkUserImportResult{Index: idx}
		normalized := normalizeAuthUser(user)
		if normalized != nil {
			result.Email = normalized.Email
		}

		statusOverride := types.LifecycleState("")
		if normalized == nil {
			err := bulkImportError(ErrUserRequired, bulkImportMetadata(idx, result.Email, uuid.Nil))
			result.Err = err
			results = append(results, result)
			errs = append(errs, err)
			if !input.ContinueOnError {
				break
			}
			continue
		}
		if normalized.Status == "" && input.DefaultStatus != "" {
			statusOverride = input.DefaultStatus
		}

		if input.DryRun {
			err := c.executeDryRun(ctx, input, normalized, statusOverride, &result)
			if err != nil {
				errs = append(errs, err)
				if !input.ContinueOnError {
					results = append(results, result)
					break
				}
			}
			results = append(results, result)
			continue
		}

		created := &types.AuthUser{}
		err := c.create.Execute(ctx, UserCreateInput{
			User:   normalized,
			Status: statusOverride,
			Actor:  input.Actor,
			Scope:  input.Scope,
			Result: created,
		})
		if err != nil {
			err = bulkImportError(err, bulkImportMetadata(idx, result.Email, uuid.Nil))
			result.Err = err
			results = append(results, result)
			errs = append(errs, err)
			if !input.ContinueOnError {
				break
			}
			continue
		}

		if created != nil {
			result.UserID = created.ID
			result.Email = created.Email
			result.Status = created.Status
		} else {
			result.Status = resolveAuthUserStatus(normalized, statusOverride)
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

func (c *BulkUserImportCommand) executeDryRun(ctx context.Context, input BulkUserImportInput, user *types.AuthUser, statusOverride types.LifecycleState, result *BulkUserImportResult) error {
	createInput := UserCreateInput{
		User:   user,
		Status: statusOverride,
		Actor:  input.Actor,
		Scope:  input.Scope,
	}
	if err := createInput.Validate(); err != nil {
		err = bulkImportError(err, bulkImportMetadata(result.Index, result.Email, uuid.Nil))
		result.Err = err
		return err
	}

	scopeFilter, err := c.create.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionUsersWrite, uuid.Nil)
	if err != nil {
		err = bulkImportError(err, bulkImportMetadata(result.Index, result.Email, uuid.Nil))
		result.Err = err
		return err
	}

	status := resolveAuthUserStatus(user, statusOverride)
	if user != nil {
		user.Status = status
	}

	record := buildUserCreatedActivityRecord(user, input.Actor, scopeFilter, c.create.clock, true)
	logActivity(ctx, c.create.sink, record)
	emitActivityHook(ctx, c.create.hooks, record)

	result.Status = status
	result.UserID = uuid.Nil
	return nil
}

func bulkImportMetadata(index int, email string, userID uuid.UUID) map[string]any {
	metadata := map[string]any{
		"index": index,
	}
	if email != "" {
		metadata["email"] = email
	}
	if userID != uuid.Nil {
		metadata["user_id"] = userID.String()
	}
	return metadata
}

func bulkImportError(err error, metadata map[string]any) error {
	if err == nil {
		return nil
	}
	var richErr *goerrors.Error
	if goerrors.As(err, &richErr) {
		return richErr.WithMetadata(metadata)
	}

	category := goerrors.CategoryInternal
	code := goerrors.CodeInternal
	switch {
	case errors.Is(err, ErrUserRequired),
		errors.Is(err, ErrUserEmailRequired),
		errors.Is(err, ErrUsersRequired),
		errors.Is(err, ErrActorRequired):
		category = goerrors.CategoryValidation
		code = goerrors.CodeBadRequest
	case errors.Is(err, types.ErrUnauthorizedScope):
		category = goerrors.CategoryAuthz
		code = goerrors.CodeForbidden
	}

	return goerrors.Wrap(err, category, "go-users: bulk user import failed").
		WithCode(code).
		WithMetadata(metadata)
}
