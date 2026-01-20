package passwordreset

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

// RepositoryConfig wires the Bun-backed password reset repository.
type RepositoryConfig struct {
	DB         *bun.DB
	Repository repository.Repository[*Record]
	Clock      types.Clock
}

// Repository implements types.PasswordResetRepository using Bun.
type Repository struct {
	store repository.Repository[*Record]
	clock types.Clock
	db    *bun.DB
}

// NewRepository constructs the default password reset repository.
func NewRepository(cfg RepositoryConfig) (*Repository, error) {
	if cfg.Repository == nil && cfg.DB == nil {
		return nil, errors.New("passwordreset: db or repository required")
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

var _ types.PasswordResetRepository = (*Repository)(nil)

// CreateReset persists a password reset record.
func (r *Repository) CreateReset(ctx context.Context, record types.PasswordResetRecord) (*types.PasswordResetRecord, error) {
	if record.UserID == uuid.Nil {
		return nil, types.ErrUserIDRequired
	}
	rec := fromDomain(record)
	if rec.ID == uuid.Nil {
		rec.ID = uuid.New()
	}
	now := r.clock.Now()
	if rec.CreatedAt == nil {
		rec.CreatedAt = timePtr(now)
	}
	if rec.UpdatedAt == nil {
		rec.UpdatedAt = timePtr(now)
	}
	if rec.IssuedAt == nil {
		rec.IssuedAt = timePtr(now)
	}
	if strings.TrimSpace(rec.Status) == "" {
		rec.Status = string(types.PasswordResetStatusRequested)
	}
	created, err := r.store.Create(ctx, rec)
	if err != nil {
		return nil, err
	}
	return toDomain(created), nil
}

// GetResetByJTI returns the password reset record for a JTI.
func (r *Repository) GetResetByJTI(ctx context.Context, jti string) (*types.PasswordResetRecord, error) {
	rec, err := r.store.Get(ctx, selectReset(jti))
	if err != nil {
		if repository.IsRecordNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return toDomain(rec), nil
}

// ConsumeReset marks the reset token as used, enforcing single-use semantics.
func (r *Repository) ConsumeReset(ctx context.Context, jti string, usedAt time.Time) error {
	if r == nil || r.db == nil {
		return errors.New("passwordreset: db required for updates")
	}
	normalized := strings.TrimSpace(jti)
	if normalized == "" {
		return errors.New("passwordreset: jti required")
	}
	if usedAt.IsZero() {
		usedAt = r.clock.Now()
	}
	now := r.clock.Now()
	rec := &Record{
		UsedAt:    timePtr(usedAt),
		UpdatedAt: timePtr(now),
	}
	q := r.db.NewUpdate().Model(rec).
		Column("used_at", "updated_at").
		Where("jti = ?", normalized).
		Where("status = ?", string(types.PasswordResetStatusRequested)).
		Where("used_at IS NULL").
		Where("expires_at IS NULL OR expires_at > ?", usedAt)
	res, err := q.Exec(ctx)
	if err != nil {
		return repository.MapDatabaseError(err, repository.DetectDriver(r.db))
	}
	return repository.SQLExpectedCount(res, 1)
}

// UpdateResetStatus updates reset status and usage timestamp.
func (r *Repository) UpdateResetStatus(ctx context.Context, jti string, status types.PasswordResetStatus, usedAt time.Time) error {
	rec, err := r.store.Get(ctx, selectReset(jti))
	if err != nil {
		return err
	}
	rec.Status = string(status)
	if !usedAt.IsZero() {
		rec.UsedAt = timePtr(usedAt)
		rec.ResetedAt = timePtr(usedAt)
	}
	rec.UpdatedAt = timePtr(r.clock.Now())
	_, err = r.store.Update(ctx, rec)
	return err
}

func selectReset(jti string) repository.SelectCriteria {
	return repository.SelectBy("jti", "=", strings.TrimSpace(jti))
}

func fromDomain(record types.PasswordResetRecord) *Record {
	return &Record{
		ID:            record.ID,
		UserID:        record.UserID,
		Email:         record.Email,
		Status:        string(record.Status),
		JTI:           record.JTI,
		IssuedAt:      timePtr(record.IssuedAt),
		ExpiresAt:     timePtr(record.ExpiresAt),
		UsedAt:        timePtr(record.UsedAt),
		ResetedAt:     timePtr(record.ResetAt),
		ScopeTenantID: record.Scope.TenantID,
		ScopeOrgID:    record.Scope.OrgID,
		CreatedAt:     timePtr(record.CreatedAt),
		UpdatedAt:     timePtr(record.UpdatedAt),
	}
}

func toDomain(rec *Record) *types.PasswordResetRecord {
	if rec == nil {
		return nil
	}
	return &types.PasswordResetRecord{
		ID:        rec.ID,
		UserID:    rec.UserID,
		Email:     rec.Email,
		Status:    types.PasswordResetStatus(rec.Status),
		JTI:       rec.JTI,
		IssuedAt:  timeFromPtr(rec.IssuedAt),
		ExpiresAt: timeFromPtr(rec.ExpiresAt),
		UsedAt:    timeFromPtr(rec.UsedAt),
		ResetAt:   timeFromPtr(rec.ResetedAt),
		Scope: types.ScopeFilter{
			TenantID: rec.ScopeTenantID,
			OrgID:    rec.ScopeOrgID,
		},
		CreatedAt: timeFromPtr(rec.CreatedAt),
		UpdatedAt: timeFromPtr(rec.UpdatedAt),
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
