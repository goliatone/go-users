package types

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AuthUser is the storage-agnostic representation of an upstream auth user.
// Fields mirror the values go-users needs to orchestrate lifecycle, profile,
// and preference flows without binding to go-auth structs.
type AuthUser struct {
	ID        uuid.UUID
	Role      string
	Status    LifecycleState
	Email     string
	Username  string
	FirstName string
	LastName  string
	Metadata  map[string]any
	CreatedAt *time.Time
	UpdatedAt *time.Time
	Raw       any
}

// LifecycleTransition describes an allowed move between states.
type LifecycleTransition struct {
	From LifecycleState
	To   LifecycleState
}

// ActorRef identifies who or what is initiating a lifecycle change.
type ActorRef struct {
	ID   uuid.UUID
	Type string
}

// TransitionConfig captures metadata supplied to lifecycle changes.
type TransitionConfig struct {
	Reason   string
	Metadata map[string]any
	Force    bool
}

// TransitionOption customizes lifecycle transitions triggered through the
// AuthRepository.
type TransitionOption func(*TransitionConfig)

// WithTransitionReason sets the human readable reason recorded for a transition.
func WithTransitionReason(reason string) TransitionOption {
	return func(cfg *TransitionConfig) {
		cfg.Reason = reason
	}
}

// WithTransitionMetadata merges metadata into the transition audit payload.
func WithTransitionMetadata(metadata map[string]any) TransitionOption {
	return func(cfg *TransitionConfig) {
		if len(metadata) == 0 {
			return
		}
		if cfg.Metadata == nil {
			cfg.Metadata = make(map[string]any, len(metadata))
		}
		for k, v := range metadata {
			cfg.Metadata[k] = v
		}
	}
}

// WithForceTransition bypasses policy checks (use sparingly).
func WithForceTransition() TransitionOption {
	return func(cfg *TransitionConfig) {
		cfg.Force = true
	}
}

// AuthRepository abstracts whichever upstream user repository go-users sits on.
// Implementations typically wrap go-auth's Users repository, but any Bun-backed
// store that honors these semantics can be injected.
type AuthRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*AuthUser, error)
	GetByIdentifier(ctx context.Context, identifier string) (*AuthUser, error)
	Create(ctx context.Context, input *AuthUser) (*AuthUser, error)
	Update(ctx context.Context, input *AuthUser) (*AuthUser, error)
	UpdateStatus(ctx context.Context, actor ActorRef, id uuid.UUID, next LifecycleState, opts ...TransitionOption) (*AuthUser, error)
	AllowedTransitions(ctx context.Context, id uuid.UUID) ([]LifecycleTransition, error)
	ResetPassword(ctx context.Context, id uuid.UUID, passwordHash string) error
}
