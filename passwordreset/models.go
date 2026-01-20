package passwordreset

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// Record models the persisted password_reset row.
type Record struct {
	bun.BaseModel `bun:"table:password_reset"`

	ID            uuid.UUID  `bun:"id,pk,type:uuid"`
	UserID        uuid.UUID  `bun:"user_id,notnull,type:uuid"`
	Email         string     `bun:"email,notnull"`
	Status        string     `bun:"status,notnull"`
	JTI           string     `bun:"jti"`
	IssuedAt      *time.Time `bun:"issued_at,nullzero"`
	ExpiresAt     *time.Time `bun:"expires_at,nullzero"`
	UsedAt        *time.Time `bun:"used_at,nullzero"`
	ResetedAt     *time.Time `bun:"reseted_at,nullzero"`
	ScopeTenantID uuid.UUID  `bun:"scope_tenant_id,type:uuid,nullzero"`
	ScopeOrgID    uuid.UUID  `bun:"scope_org_id,type:uuid,nullzero"`
	CreatedAt     *time.Time `bun:"created_at,nullzero"`
	DeletedAt     *time.Time `bun:"deleted_at,soft_delete,nullzero"`
	UpdatedAt     *time.Time `bun:"updated_at,nullzero"`
}
