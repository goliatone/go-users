package memory

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

// UserTokenRepository stores token metadata in memory for examples.
type UserTokenRepository struct {
	mu     sync.RWMutex
	tokens map[string]*types.UserToken
}

// NewUserTokenRepository provisions a token repository stub.
func NewUserTokenRepository() *UserTokenRepository {
	return &UserTokenRepository{
		tokens: make(map[string]*types.UserToken),
	}
}

var _ types.UserTokenRepository = (*UserTokenRepository)(nil)

// CreateToken persists an onboarding token record.
func (r *UserTokenRepository) CreateToken(_ context.Context, token types.UserToken) (*types.UserToken, error) {
	if r == nil {
		return nil, errors.New("token repository not configured")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tokens == nil {
		r.tokens = make(map[string]*types.UserToken)
	}
	copy := token
	if copy.ID == uuid.Nil {
		copy.ID = uuid.New()
	}
	now := time.Now().UTC()
	if copy.CreatedAt.IsZero() {
		copy.CreatedAt = now
	}
	copy.UpdatedAt = now
	stored := cloneUserToken(&copy)
	r.tokens[tokenKey(copy.Type, copy.JTI)] = stored
	return cloneUserToken(stored), nil
}

// GetTokenByJTI returns a stored token record by type and JTI.
func (r *UserTokenRepository) GetTokenByJTI(_ context.Context, tokenType types.UserTokenType, jti string) (*types.UserToken, error) {
	if r == nil {
		return nil, errors.New("token repository not configured")
	}
	r.mu.RLock()
	token, ok := r.tokens[tokenKey(tokenType, jti)]
	r.mu.RUnlock()
	if !ok {
		return nil, errors.New("not found")
	}
	return cloneUserToken(token), nil
}

// UpdateTokenStatus updates the stored token state.
func (r *UserTokenRepository) UpdateTokenStatus(_ context.Context, tokenType types.UserTokenType, jti string, status types.UserTokenStatus, usedAt time.Time) error {
	if r == nil {
		return errors.New("token repository not configured")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	token, ok := r.tokens[tokenKey(tokenType, jti)]
	if !ok {
		return errors.New("not found")
	}
	token.Status = status
	if !usedAt.IsZero() {
		token.UsedAt = usedAt
	}
	token.UpdatedAt = time.Now().UTC()
	return nil
}

func tokenKey(tokenType types.UserTokenType, jti string) string {
	return string(tokenType) + ":" + strings.TrimSpace(jti)
}

func cloneUserToken(token *types.UserToken) *types.UserToken {
	if token == nil {
		return nil
	}
	copy := *token
	return &copy
}
