package command

import (
	"context"
	"strings"
	"time"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

const defaultInviteTTL = 72 * time.Hour

// UserInviteInput carries the data required to invite a new user.
type UserInviteInput struct {
	Email     string
	FirstName string
	LastName  string
	Role      string
	Metadata  map[string]any
	Actor     types.ActorRef
	Scope     types.ScopeFilter
	Result    *UserInviteResult
}

// UserInviteResult exposes the creation output and invite token details.
type UserInviteResult struct {
	User      *types.AuthUser
	Token     string
	ExpiresAt time.Time
}

// UserInviteCommand creates pending users and records invite metadata.
type UserInviteCommand struct {
	repo     types.AuthRepository
	clock    types.Clock
	idGen    types.IDGenerator
	sink     types.ActivitySink
	hooks    types.Hooks
	logger   types.Logger
	tokenTTL time.Duration
	guard    scope.Guard
}

// InviteCommandConfig holds dependencies for the invite flow.
type InviteCommandConfig struct {
	Repository types.AuthRepository
	Clock      types.Clock
	IDGen      types.IDGenerator
	Activity   types.ActivitySink
	Hooks      types.Hooks
	Logger     types.Logger
	TokenTTL   time.Duration
	ScopeGuard scope.Guard
}

// NewUserInviteCommand constructs the invite handler.
func NewUserInviteCommand(cfg InviteCommandConfig) *UserInviteCommand {
	ttl := cfg.TokenTTL
	if ttl == 0 {
		ttl = defaultInviteTTL
	}
	idGen := cfg.IDGen
	if idGen == nil {
		idGen = types.UUIDGenerator{}
	}
	return &UserInviteCommand{
		repo:     cfg.Repository,
		clock:    safeClock(cfg.Clock),
		idGen:    idGen,
		sink:     safeActivitySink(cfg.Activity),
		hooks:    safeHooks(cfg.Hooks),
		logger:   safeLogger(cfg.Logger),
		tokenTTL: ttl,
		guard:    safeScopeGuard(cfg.ScopeGuard),
	}
}

var _ gocommand.Commander[UserInviteInput] = (*UserInviteCommand)(nil)

// Execute creates the pending user record and registers invite metadata.
func (c *UserInviteCommand) Execute(ctx context.Context, input UserInviteInput) error {
	if err := c.validate(input); err != nil {
		return err
	}
	scope, err := c.guard.Enforce(ctx, input.Actor, input.Scope, types.PolicyActionUsersWrite, uuid.Nil)
	if err != nil {
		return err
	}

	token := c.idGen.UUID().String()
	expiresAt := now(c.clock).Add(c.tokenTTL)
	metadata := cloneMap(input.Metadata)
	metadata["invite"] = map[string]any{
		"token":      token,
		"expires_at": expiresAt.Format(time.RFC3339Nano),
		"actor_id":   input.Actor.ID.String(),
		"tenant_id":  scope.TenantID.String(),
		"org_id":     scope.OrgID.String(),
	}

	user := &types.AuthUser{
		Email:     strings.TrimSpace(input.Email),
		FirstName: strings.TrimSpace(input.FirstName),
		LastName:  strings.TrimSpace(input.LastName),
		Role:      input.Role,
		Status:    types.LifecycleStatePending,
		Metadata:  metadata,
	}
	created, err := c.repo.Create(ctx, user)
	if err != nil {
		return err
	}

	record := types.ActivityRecord{
		UserID:     created.ID,
		ActorID:    input.Actor.ID,
		Verb:       "user.invite",
		ObjectType: "user",
		ObjectID:   created.ID.String(),
		Channel:    "invites",
		TenantID:   scope.TenantID,
		OrgID:      scope.OrgID,
		Data: map[string]any{
			"email":      created.Email,
			"role":       created.Role,
			"token":      token,
			"expires_at": expiresAt,
		},
		OccurredAt: now(c.clock),
	}
	logActivity(ctx, c.sink, record)
	emitActivityHook(ctx, c.hooks, record)

	if input.Result != nil {
		*input.Result = UserInviteResult{
			User:      created,
			Token:     token,
			ExpiresAt: expiresAt,
		}
	}

	return nil
}

func (c *UserInviteCommand) validate(input UserInviteInput) error {
	switch {
	case strings.TrimSpace(input.Email) == "":
		return ErrInviteEmailRequired
	case input.Actor.ID == uuid.Nil:
		return ErrActorRequired
	default:
		return nil
	}
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
