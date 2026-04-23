package command

import (
	"context"
	"strings"
	"time"

	goerrors "github.com/goliatone/go-errors"
	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
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
	Activity   types.ActivitySink
	Hooks      types.Hooks
	Logger     types.Logger
	ScopeGuard scope.Guard
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
			Activity:   cfg.Activity,
			Hooks:      cfg.Hooks,
			Logger:     cfg.Logger,
			ScopeGuard: cfg.ScopeGuard,
		})
	}
	reset := cfg.Reset
	if reset == nil {
		reset = NewUserPasswordResetCommand(PasswordResetCommandConfig{
			Repository: cfg.Repository,
			Clock:      clock,
			Activity:   cfg.Activity,
			Hooks:      cfg.Hooks,
			Logger:     cfg.Logger,
			ScopeGuard: cfg.ScopeGuard,
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
		created, createErr := c.createBootstrapUser(ctx, input, issuedAt, expiresAt)
		if createErr != nil {
			return createErr
		}
		setBootstrapResult(input.Result, created, true, expiresAt)
		return nil
	}

	if resetErr := c.reset.Execute(ctx, UserPasswordResetInput{
		UserID:          existing.ID,
		NewPasswordHash: strings.TrimSpace(input.PasswordHash),
		Actor:           input.Actor,
		Scope:           input.Scope,
	}); resetErr != nil {
		return resetErr
	}
	refreshed, err := c.markTemporary(ctx, existing.ID, issuedAt, expiresAt)
	if err != nil {
		return err
	}
	if refreshed != nil {
		existing = refreshed
	}
	setBootstrapResult(input.Result, existing, false, expiresAt)
	return nil
}

func (c *UserBootstrapPasswordCommand) createBootstrapUser(ctx context.Context, input UserBootstrapPasswordInput, issuedAt, expiresAt time.Time) (*types.AuthUser, error) {
	user := *input.User
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
	if err := c.reset.Execute(ctx, UserPasswordResetInput{
		UserID:          result.ID,
		NewPasswordHash: strings.TrimSpace(input.PasswordHash),
		Actor:           input.Actor,
		Scope:           input.Scope,
	}); err != nil {
		return nil, err
	}
	refreshed, err := c.markTemporary(ctx, result.ID, issuedAt, expiresAt)
	if err != nil {
		return nil, err
	}
	if refreshed != nil {
		return refreshed, nil
	}
	return result, nil
}

func (c *UserBootstrapPasswordCommand) markTemporary(ctx context.Context, id uuid.UUID, issuedAt, expiresAt time.Time) (*types.AuthUser, error) {
	if tempRepo, ok := c.repo.(types.TemporaryPasswordRepository); ok {
		if err := tempRepo.MarkTemporaryPassword(ctx, id, issuedAt, expiresAt); err != nil {
			return nil, err
		}
		return c.repo.GetByID(ctx, id)
	}

	user, err := c.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	user.Metadata = types.MarkTemporaryPasswordMetadata(user.Metadata, issuedAt, expiresAt)
	return c.repo.Update(ctx, user)
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
		goerrors.IsNotFound(err)
}
