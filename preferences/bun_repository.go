package preferences

import (
	"context"
	"errors"
	"strings"

	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// RepositoryConfig wires dependencies for the Bun-backed preference store.
type RepositoryConfig struct {
	DB         *bun.DB
	Repository repository.Repository[*Record]
	Clock      types.Clock
	IDGen      types.IDGenerator
}

type preferenceStore interface {
	repository.Repository[*Record]
}

// Repository implements types.PreferenceRepository.
type Repository struct {
	preferenceStore
	clock types.Clock
	idGen types.IDGenerator
}

// NewRepository constructs the default preference repository.
func NewRepository(cfg RepositoryConfig) (*Repository, error) {
	if cfg.Repository == nil && cfg.DB == nil {
		return nil, errors.New("preferences: db or repository required")
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
	idGen := cfg.IDGen
	if idGen == nil {
		idGen = types.UUIDGenerator{}
	}

	return &Repository{
		preferenceStore: repo,
		clock:           clock,
		idGen:           idGen,
	}, nil
}

var (
	_ repository.Repository[*Record] = (*Repository)(nil)
	_ types.PreferenceRepository     = (*Repository)(nil)
)

// ListPreferences fetches preference records for the requested scope level.
func (r *Repository) ListPreferences(ctx context.Context, filter types.PreferenceFilter) ([]types.PreferenceRecord, error) {
	level := coalesceLevel(filter.Level)
	ids, err := scopeIDs(level, filter.UserID, filter.Scope)
	if err != nil {
		return nil, err
	}
	criteria := []repository.SelectCriteria{
		func(q *bun.SelectQuery) *bun.SelectQuery {
			q = q.Where("scope_level = ?", string(level)).
				Where("user_id = ?", ids.user).
				Where("tenant_id = ?", ids.tenant).
				Where("org_id = ?", ids.org).
				OrderExpr("key ASC")
			if len(filter.Keys) > 0 {
				keys := make([]string, len(filter.Keys))
				for i, key := range filter.Keys {
					keys[i] = strings.ToLower(strings.TrimSpace(key))
				}
				q = q.Where("lower(key) IN (?)", bun.In(keys))
			}
			return q
		},
	}

	rows, _, err := r.List(ctx, criteria...)
	if err != nil {
		return nil, err
	}
	result := make([]types.PreferenceRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, toDomain(row))
	}
	return result, nil
}

// UpsertPreference inserts or updates a scoped preference entry.
func (r *Repository) UpsertPreference(ctx context.Context, record types.PreferenceRecord) (*types.PreferenceRecord, error) {
	level := coalesceLevel(record.Level)
	ids, err := scopeIDs(level, record.UserID, record.Scope)
	if err != nil {
		return nil, err
	}
	now := r.clock.Now()
	payload := fromDomain(record)
	payload.ScopeLevel = string(level)
	payload.UserID = ids.user
	payload.TenantID = ids.tenant
	payload.OrgID = ids.org
	payload.Value = cloneMap(payload.Value)

	existing, err := r.findExisting(ctx, level, ids, record.Key)
	switch {
	case err == nil && existing != nil:
		payload.ID = existing.ID
		payload.CreatedAt = existing.CreatedAt
		payload.CreatedBy = existing.CreatedBy
		payload.Version = existing.Version + 1
		payload.UpdatedAt = now
		updated, err := r.Update(ctx, payload)
		if err != nil {
			return nil, err
		}
		return toDomainPtr(updated), nil
	case repository.IsRecordNotFound(err):
		payload.ID = r.idGen.UUID()
		payload.Version = max(record.Version, 1)
		payload.CreatedAt = now
		payload.UpdatedAt = now
		if payload.CreatedBy == uuid.Nil {
			payload.CreatedBy = payload.UpdatedBy
		}
		created, err := r.Create(ctx, payload)
		if err != nil {
			return nil, err
		}
		return toDomainPtr(created), nil
	default:
		return nil, err
	}
}

// DeletePreference removes a scoped preference entry.
func (r *Repository) DeletePreference(ctx context.Context, userID uuid.UUID, scope types.ScopeFilter, level types.PreferenceLevel, key string) error {
	level = coalesceLevel(level)
	ids, err := scopeIDs(level, userID, scope)
	if err != nil {
		return err
	}
	existing, err := r.findExisting(ctx, level, ids, key)
	if err != nil {
		return err
	}
	return r.Delete(ctx, existing)
}

func (r *Repository) findExisting(ctx context.Context, level types.PreferenceLevel, ids scopeValues, key string) (*Record, error) {
	lowerKey := strings.ToLower(strings.TrimSpace(key))
	if lowerKey == "" {
		return nil, errors.New("preferences: key required")
	}
	criteria := []repository.SelectCriteria{
		func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.
				Where("scope_level = ?", string(level)).
				Where("user_id = ?", ids.user).
				Where("tenant_id = ?", ids.tenant).
				Where("org_id = ?", ids.org).
				Where("lower(key) = ?", lowerKey).
				Limit(1)
		},
	}
	rows, _, err := r.List(ctx, criteria...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, repository.NewRecordNotFound()
	}
	return rows[0], nil
}

type scopeValues struct {
	user   uuid.UUID
	tenant uuid.UUID
	org    uuid.UUID
}

func scopeIDs(level types.PreferenceLevel, userID uuid.UUID, scope types.ScopeFilter) (scopeValues, error) {
	switch level {
	case types.PreferenceLevelUser:
		if userID == uuid.Nil {
			return scopeValues{}, types.ErrUserIDRequired
		}
		return scopeValues{
			user:   userID,
			tenant: scopeUUID(scope.TenantID),
			org:    scopeUUID(scope.OrgID),
		}, nil
	case types.PreferenceLevelOrg:
		if scope.OrgID == uuid.Nil {
			return scopeValues{}, errors.New("preferences: org scope required")
		}
		return scopeValues{
			tenant: scopeUUID(scope.TenantID),
			org:    scopeUUID(scope.OrgID),
		}, nil
	case types.PreferenceLevelTenant:
		if scope.TenantID == uuid.Nil {
			return scopeValues{}, errors.New("preferences: tenant scope required")
		}
		return scopeValues{
			tenant: scopeUUID(scope.TenantID),
		}, nil
	case types.PreferenceLevelSystem:
		return scopeValues{}, nil
	default:
		return scopeValues{}, errors.New("preferences: unknown level")
	}
}

func coalesceLevel(level types.PreferenceLevel) types.PreferenceLevel {
	if level == "" {
		return types.PreferenceLevelUser
	}
	return level
}

func fromDomain(record types.PreferenceRecord) *Record {
	return &Record{
		ID:         record.ID,
		UserID:     record.UserID,
		TenantID:   scopeUUID(record.Scope.TenantID),
		OrgID:      scopeUUID(record.Scope.OrgID),
		ScopeLevel: string(record.Level),
		Key:        strings.TrimSpace(record.Key),
		Value:      cloneMap(record.Value),
		Version:    record.Version,
		CreatedAt:  record.CreatedAt,
		CreatedBy:  record.CreatedBy,
		UpdatedAt:  record.UpdatedAt,
		UpdatedBy:  record.UpdatedBy,
	}
}

func toDomain(record *Record) types.PreferenceRecord {
	if record == nil {
		return types.PreferenceRecord{}
	}
	return types.PreferenceRecord{
		ID:     record.ID,
		UserID: record.UserID,
		Scope: types.ScopeFilter{
			TenantID: record.TenantID,
			OrgID:    record.OrgID,
		},
		Level:     types.PreferenceLevel(record.ScopeLevel),
		Key:       record.Key,
		Value:     cloneMap(record.Value),
		Version:   record.Version,
		CreatedAt: record.CreatedAt,
		CreatedBy: record.CreatedBy,
		UpdatedAt: record.UpdatedAt,
		UpdatedBy: record.UpdatedBy,
	}
}

func toDomainPtr(record *Record) *types.PreferenceRecord {
	rec := toDomain(record)
	return &rec
}

// FromPreferenceRecord converts a domain preference record into the Bun model.
func FromPreferenceRecord(record types.PreferenceRecord) *Record {
	return fromDomain(record)
}

// ToPreferenceRecord converts the Bun model into the domain preference record.
func ToPreferenceRecord(record *Record) types.PreferenceRecord {
	return toDomain(record)
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func scopeUUID(id uuid.UUID) uuid.UUID {
	if id == uuid.Nil {
		return uuid.Nil
	}
	return id
}
