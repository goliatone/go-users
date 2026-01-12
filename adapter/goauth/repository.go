package goauth

import (
	"context"

	auth "github.com/goliatone/go-auth"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

// UsersAdapter wraps go-auth Users repositories so they satisfy the
// go-users AuthRepository interface.
type UsersAdapter struct {
	repo   auth.Users
	sm     auth.UserStateMachine
	policy types.TransitionPolicy
}

// NewUsersAdapter builds a UsersAdapter. Callers can override the transition
// policy (defaults to the upstream state machine rules) with WithPolicy.
func NewUsersAdapter(repo auth.Users, opts ...UsersAdapterOption) *UsersAdapter {
	adapter := &UsersAdapter{
		repo:   repo,
		sm:     auth.NewUserStateMachine(repo),
		policy: types.DefaultTransitionPolicy(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(adapter)
		}
	}
	return adapter
}

// UsersAdapterOption customizes adapter construction.
type UsersAdapterOption func(*UsersAdapter)

// WithPolicy overrides the default transition policy.
func WithPolicy(policy types.TransitionPolicy) UsersAdapterOption {
	return func(adapter *UsersAdapter) {
		if policy != nil {
			adapter.policy = policy
		}
	}
}

var _ types.AuthRepository = (*UsersAdapter)(nil)

// GetByID loads a user by UUID.
func (a *UsersAdapter) GetByID(ctx context.Context, id uuid.UUID) (*types.AuthUser, error) {
	record, err := a.repo.GetByID(ctx, id.String())
	if err != nil {
		return nil, err
	}
	return toAuthUser(record), nil
}

// GetByIdentifier loads a user using email/username/UUID.
func (a *UsersAdapter) GetByIdentifier(ctx context.Context, identifier string) (*types.AuthUser, error) {
	record, err := a.repo.GetByIdentifier(ctx, identifier)
	if err != nil {
		return nil, err
	}
	return toAuthUser(record), nil
}

// Create delegates to go-auth's repository.
func (a *UsersAdapter) Create(ctx context.Context, input *types.AuthUser) (*types.AuthUser, error) {
	record := fromAuthUser(input)
	created, err := a.repo.Create(ctx, record)
	if err != nil {
		return nil, err
	}
	return toAuthUser(created), nil
}

// Update delegates to go-auth's repository.
func (a *UsersAdapter) Update(ctx context.Context, input *types.AuthUser) (*types.AuthUser, error) {
	record := fromAuthUser(input)
	updated, err := a.repo.Update(ctx, record)
	if err != nil {
		return nil, err
	}
	return toAuthUser(updated), nil
}

// UpdateStatus transitions the user to the next lifecycle state.
func (a *UsersAdapter) UpdateStatus(ctx context.Context, actor types.ActorRef, id uuid.UUID, next types.LifecycleState, opts ...types.TransitionOption) (*types.AuthUser, error) {
	record, err := a.repo.GetByID(ctx, id.String())
	if err != nil {
		return nil, err
	}

	current := types.LifecycleState(record.Status)
	config := configFromOptions(opts...)
	if a.policy != nil && !config.Force {
		if err := a.policy.Validate(current, next); err != nil {
			return nil, err
		}
	}

	goAuthOpts := buildGoAuthOptions(config)
	goActor := auth.ActorRef{
		ID:   actor.ID.String(),
		Type: actor.Type,
	}
	updated, err := a.sm.Transition(ctx, goActor, record, auth.UserStatus(next), goAuthOpts...)
	if err != nil {
		return nil, err
	}
	return toAuthUser(updated), nil
}

// AllowedTransitions reports valid target states using the configured policy.
func (a *UsersAdapter) AllowedTransitions(ctx context.Context, id uuid.UUID) ([]types.LifecycleTransition, error) {
	record, err := a.repo.GetByID(ctx, id.String())
	if err != nil {
		return nil, err
	}
	current := types.LifecycleState(record.Status)
	if a.policy == nil {
		return nil, nil
	}
	targets := a.policy.AllowedTargets(current)
	transitions := make([]types.LifecycleTransition, 0, len(targets))
	for _, target := range targets {
		transitions = append(transitions, types.LifecycleTransition{
			From: current,
			To:   target,
		})
	}
	return transitions, nil
}

// ResetPassword delegates to the upstream repository implementation.
func (a *UsersAdapter) ResetPassword(ctx context.Context, id uuid.UUID, passwordHash string) error {
	return a.repo.ResetPassword(ctx, id, passwordHash)
}

func toAuthUser(user *auth.User) *types.AuthUser {
	if user == nil {
		return nil
	}
	return &types.AuthUser{
		ID:        user.ID,
		Role:      string(user.Role),
		Status:    types.LifecycleState(user.Status),
		Email:     user.Email,
		Username:  user.Username,
		FirstName: user.FirstName,
		LastName:  user.LastName,
		Metadata:  copyMetadata(user.Metadata),
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		Raw:       user,
	}
}

func fromAuthUser(user *types.AuthUser) *auth.User {
	if user == nil {
		return nil
	}
	if raw, ok := user.Raw.(*auth.User); ok && raw != nil {
		clone := *raw
		clone.ID = user.ID
		clone.Role = auth.UserRole(user.Role)
		clone.Status = auth.UserStatus(user.Status)
		clone.Email = user.Email
		clone.Username = user.Username
		clone.FirstName = user.FirstName
		clone.LastName = user.LastName
		clone.Metadata = copyMetadata(user.Metadata)
		return &clone
	}
	return &auth.User{
		ID:        user.ID,
		Role:      auth.UserRole(user.Role),
		Status:    auth.UserStatus(user.Status),
		Email:     user.Email,
		Username:  user.Username,
		FirstName: user.FirstName,
		LastName:  user.LastName,
		Metadata:  copyMetadata(user.Metadata),
	}
}

func buildGoAuthOptions(cfg types.TransitionConfig) []auth.TransitionOption {
	opts := make([]auth.TransitionOption, 0, 3)
	if cfg.Reason != "" {
		opts = append(opts, auth.WithTransitionReason(cfg.Reason))
	}
	if len(cfg.Metadata) > 0 {
		opts = append(opts, auth.WithTransitionMetadata(cfg.Metadata))
	}
	if cfg.Force {
		opts = append(opts, auth.WithForceTransition())
	}
	return opts
}

func configFromOptions(opts ...types.TransitionOption) types.TransitionConfig {
	cfg := types.TransitionConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}

func copyMetadata(origin map[string]any) map[string]any {
	if len(origin) == 0 {
		return nil
	}
	out := make(map[string]any, len(origin))
	for k, v := range origin {
		out[k] = v
	}
	return out
}
