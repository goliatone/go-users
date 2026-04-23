package command

import (
	"context"
	"strings"
	"time"

	goerrors "github.com/goliatone/go-errors"
	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

const DefaultTemporaryPasswordTTL = 24 * time.Hour

// UserBootstrapPasswordInput creates or refreshes a bootstrap user with a temporary password.
type UserBootstrapPasswordInput struct {
	User         *types.AuthUser
	Identifier   string
	PasswordHash string
	TTL          time.Duration
	Actor        types.ActorRef
	Scope        types.ScopeFilter
	Result       *UserBootstrapPasswordResult
}

// Type implements gocommand.Message.
func (UserBootstrapPasswordInput) Type() string {
	return "command.user.bootstrap_password"
}

// Validate implements gocommand.Message.
func (input UserBootstrapPasswordInput) Validate() error {
	switch {
	case input.User == nil:
		return ErrUserRequired
	case strings.TrimSpace(input.User.Email) == "":
		return ErrUserEmailRequired
	case input.Actor.ID == uuid.Nil:
		return ErrActorRequired
	case strings.TrimSpace(input.PasswordHash) == "":
		return ErrPasswordHashRequired
	default:
		return nil
	}
}

// UserBootstrapPasswordResult returns the affected user and temporary password expiry.
type UserBootstrapPasswordResult struct {
	User      *types.AuthUser
	Created   bool
	ExpiresAt time.Time
}

// UserBootstrapPasswordCommand composes create/reset commands for instance bootstrap users.
type UserBootstrapPasswordCommand struct {
	repo   types.AuthRepository
	create *UserCreateCommand
	reset  *UserPasswordResetCommand
	clock  types.Clock
}

// BootstrapPasswordCommandConfig wires the bootstrap password command.
type BootstrapPasswordCommandConfig struct {
	Repository types.AuthRepository
	Create     *UserCreateCommand
	Reset      *UserPasswordResetCommand
	Clock      types.Clock
}

// NewUserBootstrapPasswordCommand constructs the bootstrap handler.
func NewUserBootstrapPasswordCommand(cfg BootstrapPasswordCommandConfig) *UserBootstrapPasswordCommand {
	clock := cfg.Clock
	if clock == nil {
		clock = types.SystemClock{}
	}
	create := cfg.Create
	if create == nil {
		create = NewUserCreateCommand(UserCreateCommandConfig{
			Repository: cfg.Repository,
			Clock:      clock,
		})
	}
	reset := cfg.Reset
	if reset == nil {
		reset = NewUserPasswordResetCommand(PasswordResetCommandConfig{
			Repository: cfg.Repository,
			Clock:      clock,
		})
	}
	return &UserBootstrapPasswordCommand{
		repo:   cfg.Repository,
		create: create,
		reset:  reset,
		clock:  clock,
	}
}

// Execute creates the user when missing or refreshes its temporary password when present.
func (c *UserBootstrapPasswordCommand) Execute(ctx context.Context, input UserBootstrapPasswordInput) error {
	if c == nil || c.repo == nil {
		return types.ErrMissingAuthRepository
	}
	if err := input.Validate(); err != nil {
		return err
	}
	ttl := input.TTL
	if ttl <= 0 {
		ttl = DefaultTemporaryPasswordTTL
	}
	issuedAt := c.clock.Now()
	expiresAt := issuedAt.Add(ttl)
	identifier := strings.TrimSpace(input.Identifier)
	if identifier == "" {
		identifier = strings.TrimSpace(input.User.Email)
	}

	existing, err := c.repo.GetByIdentifier(ctx, identifier)
	if err != nil && !isBootstrapUserNotFound(err) {
		return err
	}
	if existing == nil {
		created, err := c.createBootstrapUser(ctx, input, issuedAt, expiresAt)
		if err != nil {
			return err
		}
		setBootstrapResult(input.Result, created, true, expiresAt)
		return nil
	}

	if err := c.reset.Execute(ctx, UserPasswordResetInput{
		UserID:          existing.ID,
		NewPasswordHash: strings.TrimSpace(input.PasswordHash),
		Actor:           input.Actor,
		Scope:           input.Scope,
	}); err != nil {
		return err
	}
	if tempRepo, ok := c.repo.(types.TemporaryPasswordRepository); ok {
		if err := tempRepo.MarkTemporaryPassword(ctx, existing.ID, issuedAt, expiresAt); err != nil {
			return err
		}
		if refreshed, err := c.repo.GetByID(ctx, existing.ID); err == nil && refreshed != nil {
			existing = refreshed
		}
	} else {
		existing.PasswordHash = strings.TrimSpace(input.PasswordHash)
		existing.Metadata = types.MarkTemporaryPasswordMetadata(existing.Metadata, issuedAt, expiresAt)
		if updated, err := c.repo.Update(ctx, existing); err != nil {
			return err
		} else if updated != nil {
			existing = updated
		}
	}
	setBootstrapResult(input.Result, existing, false, expiresAt)
	return nil
}

func (c *UserBootstrapPasswordCommand) createBootstrapUser(ctx context.Context, input UserBootstrapPasswordInput, issuedAt, expiresAt time.Time) (*types.AuthUser, error) {
	user := *input.User
	user.PasswordHash = strings.TrimSpace(input.PasswordHash)
	user.Metadata = types.MarkTemporaryPasswordMetadata(input.User.Metadata, issuedAt, expiresAt)
	result := &types.AuthUser{}
	if err := c.create.Execute(ctx, UserCreateInput{
		User:   &user,
		Status: user.Status,
		Actor:  input.Actor,
		Scope:  input.Scope,
		Result: result,
	}); err != nil {
		return nil, err
	}
	return result, nil
}

func setBootstrapResult(result *UserBootstrapPasswordResult, user *types.AuthUser, created bool, expiresAt time.Time) {
	if result == nil {
		return
	}
	result.User = user
	result.Created = created
	result.ExpiresAt = expiresAt
}

func isBootstrapUserNotFound(err error) bool {
	if err == nil {
		return false
	}
	return repository.IsRecordNotFound(err) ||
		goerrors.IsNotFound(err) ||
		strings.Contains(strings.ToLower(err.Error()), "not found")
}
