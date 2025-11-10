package profile

import (
	"context"
	"errors"

	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// RepositoryConfig wires the Bun-backed profile repository.
type RepositoryConfig struct {
	DB         *bun.DB
	Repository repository.Repository[*Record]
	Clock      types.Clock
}

type profileStore interface {
	repository.Repository[*Record]
}

// Repository implements types.ProfileRepository using Bun.
type Repository struct {
	profileStore
	clock types.Clock
}

// NewRepository constructs the default profile repository.
func NewRepository(cfg RepositoryConfig) (*Repository, error) {
	if cfg.Repository == nil && cfg.DB == nil {
		return nil, errors.New("profile: db or repository required")
	}
	repo := cfg.Repository
	if repo == nil {
		repo = repository.NewRepository(cfg.DB, repository.ModelHandlers[*Record]{
			NewRecord: func() *Record { return &Record{} },
			GetID: func(rec *Record) uuid.UUID {
				if rec == nil {
					return uuid.Nil
				}
				return rec.UserID
			},
			SetID: func(rec *Record, id uuid.UUID) {
				if rec != nil {
					rec.UserID = id
				}
			},
		})
	}

	clock := cfg.Clock
	if clock == nil {
		clock = types.SystemClock{}
	}

	return &Repository{
		profileStore: repo,
		clock:        clock,
	}, nil
}

var (
	_ repository.Repository[*Record] = (*Repository)(nil)
	_ types.ProfileRepository        = (*Repository)(nil)
)

// GetProfile returns the profile for the supplied user within the provided scope.
func (r *Repository) GetProfile(ctx context.Context, userID uuid.UUID, scope types.ScopeFilter) (*types.UserProfile, error) {
	if userID == uuid.Nil {
		return nil, types.ErrUserIDRequired
	}
	rec, err := r.Get(ctx, selectUserID(userID), scopeCriteria(scope))
	if err != nil {
		if repository.IsRecordNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return toDomain(rec), nil
}

// UpsertProfile inserts or updates the user profile based on whether it already exists.
func (r *Repository) UpsertProfile(ctx context.Context, profile types.UserProfile) (*types.UserProfile, error) {
	if profile.UserID == uuid.Nil {
		return nil, types.ErrUserIDRequired
	}
	now := r.clock.Now()
	rec := fromDomain(profile)
	rec.UpdatedAt = now
	if rec.UpdatedBy == uuid.Nil {
		rec.UpdatedBy = profile.UpdatedBy
		if rec.UpdatedBy == uuid.Nil {
			rec.UpdatedBy = profile.CreatedBy
		}
	}
	rec.Contact = cloneMap(rec.Contact)
	rec.Metadata = cloneMap(rec.Metadata)

	existing, err := r.Get(ctx, selectUserID(profile.UserID), scopeCriteria(profile.Scope))
	switch {
	case err == nil:
		rec.CreatedAt = existing.CreatedAt
		if rec.CreatedAt.IsZero() {
			rec.CreatedAt = now
		}
		if rec.CreatedBy == uuid.Nil {
			rec.CreatedBy = existing.CreatedBy
			if rec.CreatedBy == uuid.Nil {
				rec.CreatedBy = rec.UpdatedBy
			}
		}
		rec.TenantID = scopeUUID(profile.Scope.TenantID)
		rec.OrgID = scopeUUID(profile.Scope.OrgID)
		updated, err := r.Update(ctx, rec)
		if err != nil {
			return nil, err
		}
		return toDomain(updated), nil
	case repository.IsRecordNotFound(err):
		if rec.CreatedAt.IsZero() {
			rec.CreatedAt = now
		}
		if rec.CreatedBy == uuid.Nil {
			rec.CreatedBy = rec.UpdatedBy
		}
		rec.TenantID = scopeUUID(profile.Scope.TenantID)
		rec.OrgID = scopeUUID(profile.Scope.OrgID)
		created, err := r.Create(ctx, rec)
		if err != nil {
			return nil, err
		}
		return toDomain(created), nil
	default:
		return nil, err
	}
}

func scopeCriteria(scope types.ScopeFilter) repository.SelectCriteria {
	return func(q *bun.SelectQuery) *bun.SelectQuery {
		if scope.TenantID != uuid.Nil {
			q = q.Where("tenant_id = ?", scope.TenantID)
		}
		if scope.OrgID != uuid.Nil {
			q = q.Where("org_id = ?", scope.OrgID)
		}
		return q
	}
}

func scopeUUID(id uuid.UUID) uuid.UUID {
	if id == uuid.Nil {
		return uuid.Nil
	}
	return id
}

func selectUserID(userID uuid.UUID) repository.SelectCriteria {
	return repository.SelectBy("user_id", "=", userID.String())
}

func fromDomain(profile types.UserProfile) *Record {
	return &Record{
		UserID:      profile.UserID,
		DisplayName: profile.DisplayName,
		AvatarURL:   profile.AvatarURL,
		Locale:      profile.Locale,
		Timezone:    profile.Timezone,
		Bio:         profile.Bio,
		Contact:     cloneMap(profile.Contact),
		Metadata:    cloneMap(profile.Metadata),
		TenantID:    scopeUUID(profile.Scope.TenantID),
		OrgID:       scopeUUID(profile.Scope.OrgID),
		CreatedAt:   profile.CreatedAt,
		CreatedBy:   profile.CreatedBy,
		UpdatedAt:   profile.UpdatedAt,
		UpdatedBy:   profile.UpdatedBy,
	}
}

func toDomain(rec *Record) *types.UserProfile {
	if rec == nil {
		return nil
	}
	return &types.UserProfile{
		UserID:      rec.UserID,
		DisplayName: rec.DisplayName,
		AvatarURL:   rec.AvatarURL,
		Locale:      rec.Locale,
		Timezone:    rec.Timezone,
		Bio:         rec.Bio,
		Contact:     cloneMap(rec.Contact),
		Metadata:    cloneMap(rec.Metadata),
		Scope: types.ScopeFilter{
			TenantID: rec.TenantID,
			OrgID:    rec.OrgID,
		},
		CreatedAt: rec.CreatedAt,
		UpdatedAt: rec.UpdatedAt,
		CreatedBy: rec.CreatedBy,
		UpdatedBy: rec.UpdatedBy,
	}
}

func cloneMap(origin map[string]any) map[string]any {
	if len(origin) == 0 {
		return nil
	}
	out := make(map[string]any, len(origin))
	for k, v := range origin {
		out[k] = v
	}
	return out
}
