package profile

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// Record models the user_profiles row.
type Record struct {
	bun.BaseModel `bun:"table:user_profiles"`

	UserID      uuid.UUID      `bun:"user_id,pk,type:uuid"`
	DisplayName string         `bun:"display_name"`
	AvatarURL   string         `bun:"avatar_url"`
	Locale      string         `bun:"locale"`
	Timezone    string         `bun:"timezone"`
	Bio         string         `bun:"bio"`
	Contact     map[string]any `bun:"contact,type:jsonb"`
	Metadata    map[string]any `bun:"metadata,type:jsonb"`
	TenantID    uuid.UUID      `bun:"tenant_id,type:uuid"`
	OrgID       uuid.UUID      `bun:"org_id,type:uuid"`
	CreatedAt   time.Time      `bun:"created_at"`
	CreatedBy   uuid.UUID      `bun:"created_by,type:uuid"`
	UpdatedAt   time.Time      `bun:"updated_at"`
	UpdatedBy   uuid.UUID      `bun:"updated_by,type:uuid"`
}
