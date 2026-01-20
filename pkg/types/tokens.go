package types

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// UserTokenType identifies onboarding token types stored in user_tokens.
type UserTokenType string

const (
	UserTokenInvite        UserTokenType = "invite"
	UserTokenRegistration  UserTokenType = "register"
	UserTokenPasswordReset UserTokenType = "password_reset"
)

// UserTokenStatus tracks lifecycle state for user_tokens records.
type UserTokenStatus string

const (
	UserTokenStatusIssued  UserTokenStatus = "issued"
	UserTokenStatusUsed    UserTokenStatus = "used"
	UserTokenStatusExpired UserTokenStatus = "expired"
)

// UserToken captures persisted onboarding token metadata.
type UserToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Type      UserTokenType
	JTI       string
	Status    UserTokenStatus
	IssuedAt  time.Time
	ExpiresAt time.Time
	UsedAt    time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// UserTokenRepository persists invite/registration tokens.
type UserTokenRepository interface {
	CreateToken(ctx context.Context, token UserToken) (*UserToken, error)
	GetTokenByJTI(ctx context.Context, tokenType UserTokenType, jti string) (*UserToken, error)
	UpdateTokenStatus(ctx context.Context, tokenType UserTokenType, jti string, status UserTokenStatus, usedAt time.Time) error
}

// PasswordResetStatus tracks password_reset lifecycle values.
type PasswordResetStatus string

const (
	PasswordResetStatusUnknown   PasswordResetStatus = "unknown"
	PasswordResetStatusRequested PasswordResetStatus = "requested"
	PasswordResetStatusExpired   PasswordResetStatus = "expired"
	PasswordResetStatusChanged   PasswordResetStatus = "changed"
)

// PasswordResetRecord captures password reset lifecycle metadata.
type PasswordResetRecord struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Email     string
	Status    PasswordResetStatus
	JTI       string
	IssuedAt  time.Time
	ExpiresAt time.Time
	UsedAt    time.Time
	ResetAt   time.Time
	Scope     ScopeFilter
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PasswordResetRepository persists password reset lifecycle records.
type PasswordResetRepository interface {
	CreateReset(ctx context.Context, record PasswordResetRecord) (*PasswordResetRecord, error)
	GetResetByJTI(ctx context.Context, jti string) (*PasswordResetRecord, error)
	ConsumeReset(ctx context.Context, jti string, usedAt time.Time) error
	UpdateResetStatus(ctx context.Context, jti string, status PasswordResetStatus, usedAt time.Time) error
}
