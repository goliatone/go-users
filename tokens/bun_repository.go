package tokens

import (
	"context"
	"errors"
	"strings"
	"time"

	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// RepositoryConfig wires the Bun-backed token repository.
type RepositoryConfig struct {
	DB         *bun.DB
	Repository repository.Repository[*Record]
	Clock      types.Clock
}

// Repository implements types.UserTokenRepository using Bun.
type Repository struct {
	store repository.Repository[*Record]
	clock types.Clock
	db    *bun.DB
}

// NewRepository constructs the default token repository.
func NewRepository(cfg RepositoryConfig) (*Repository, error) {
	if cfg.Repository == nil && cfg.DB == nil {
		return nil, errors.New("tokens: db or repository required")
	}
	repo := cfg.Repository
	if repo == nil {
		repo = repository.NewRepository(cfg.DB, repository.ModelHandlers[*Record]{
			NewRecord: func() *Record { return &Record{} },
			GetID: func(rec *Record) uuid.UUID {
				if rec == nil {
					return uuid.Nil
				}
				return rec.ID
			},
			SetID: func(rec *Record, id uuid.UUID) {
				if rec != nil {
					rec.ID = id
				}
			},
		})
	}
	clock := cfg.Clock
	if clock == nil {
		clock = types.SystemClock{}
	}
	db := cfg.DB
	if db == nil {
		if withDB, ok := repo.(interface{ DB() *bun.DB }); ok {
			db = withDB.DB()
		}
	}
	return &Repository{store: repo, clock: clock, db: db}, nil
}

var _ types.UserTokenRepository = (*Repository)(nil)

// CreateToken persists a user token record.
func (r *Repository) CreateToken(ctx context.Context, token types.UserToken) (*types.UserToken, error) {
	if token.UserID == uuid.Nil {
		return nil, types.ErrUserIDRequired
	}
	rec := fromDomain(token)
	if rec.ID == uuid.Nil {
		rec.ID = uuid.New()
	}
	now := r.clock.Now()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = now
	}
	if rec.IssuedAt == nil {
		rec.IssuedAt = timePtr(now)
	}
	if strings.TrimSpace(rec.Status) == "" {
		rec.Status = string(types.UserTokenStatusIssued)
	}
	created, err := r.store.Create(ctx, rec)
	if err != nil {
		return nil, err
	}
	return toDomain(created), nil
}

// GetTokenByJTI returns the token record matching the JTI and type.
func (r *Repository) GetTokenByJTI(ctx context.Context, tokenType types.UserTokenType, jti string) (*types.UserToken, error) {
	rec, err := r.store.Get(ctx, selectToken(tokenType, jti))
	if err != nil {
		if repository.IsRecordNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return toDomain(rec), nil
}

// UpdateTokenStatus updates the token status and usage timestamp.
func (r *Repository) UpdateTokenStatus(ctx context.Context, tokenType types.UserTokenType, jti string, status types.UserTokenStatus, usedAt time.Time) error {
	if r == nil || r.db == nil {
		return errors.New("tokens: db required for updates")
	}
	normalized := strings.TrimSpace(jti)
	if normalized == "" {
		return errors.New("tokens: jti required")
	}
	now := r.clock.Now()
	rec := &Record{
		Status:    string(status),
		UsedAt:    timePtr(usedAt),
		UpdatedAt: now,
	}
	q := r.db.NewUpdate().Model(rec).
		Column("status", "used_at", "updated_at").
		Where("jti = ?", normalized)
	if tokenType != "" {
		q = q.Where("token_type = ?", string(tokenType))
	}
	q = q.Where("status = ?", string(types.UserTokenStatusIssued)).
		Where("used_at IS NULL")
	if status == types.UserTokenStatusUsed {
		q = q.Where("expires_at IS NULL OR expires_at > ?", now)
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return repository.MapDatabaseError(err, repository.DetectDriver(r.db))
	}
	return repository.SQLExpectedCount(res, 1)
}

func selectToken(tokenType types.UserTokenType, jti string) repository.SelectCriteria {
	return func(q *bun.SelectQuery) *bun.SelectQuery {
		q = q.Where("jti = ?", strings.TrimSpace(jti))
		if tokenType != "" {
			q = q.Where("token_type = ?", string(tokenType))
		}
		return q
	}
}

func fromDomain(token types.UserToken) *Record {
	return &Record{
		ID:        token.ID,
		UserID:    token.UserID,
		TokenType: string(token.Type),
		JTI:       token.JTI,
		Status:    string(token.Status),
		IssuedAt:  timePtr(token.IssuedAt),
		ExpiresAt: timePtr(token.ExpiresAt),
		UsedAt:    timePtr(token.UsedAt),
		CreatedAt: token.CreatedAt,
		UpdatedAt: token.UpdatedAt,
	}
}

func toDomain(rec *Record) *types.UserToken {
	if rec == nil {
		return nil
	}
	return &types.UserToken{
		ID:        rec.ID,
		UserID:    rec.UserID,
		Type:      types.UserTokenType(rec.TokenType),
		JTI:       rec.JTI,
		Status:    types.UserTokenStatus(rec.Status),
		IssuedAt:  timeFromPtr(rec.IssuedAt),
		ExpiresAt: timeFromPtr(rec.ExpiresAt),
		UsedAt:    timeFromPtr(rec.UsedAt),
		CreatedAt: rec.CreatedAt,
		UpdatedAt: rec.UpdatedAt,
	}
}

func timePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value
	return &copy
}

func timeFromPtr(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}
