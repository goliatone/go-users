package preferences

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// Record models the user_preferences row.
type Record struct {
	bun.BaseModel `bun:"table:user_preferences"`

	ID         uuid.UUID      `bun:"id,pk,type:uuid"`
	UserID     uuid.UUID      `bun:"user_id,type:uuid"`
	TenantID   uuid.UUID      `bun:"tenant_id,type:uuid"`
	OrgID      uuid.UUID      `bun:"org_id,type:uuid"`
	ScopeLevel string         `bun:"scope_level"`
	Key        string         `bun:"key"`
	Value      map[string]any `bun:"value,type:jsonb"`
	Version    int            `bun:"version"`
	CreatedAt  time.Time      `bun:"created_at"`
	CreatedBy  uuid.UUID      `bun:"created_by,type:uuid"`
	UpdatedAt  time.Time      `bun:"updated_at"`
	UpdatedBy  uuid.UUID      `bun:"updated_by,type:uuid"`
}
