package registry

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// CustomRole represents the schema stored in custom_roles.
type CustomRole struct {
	bun.BaseModel `bun:"table:custom_roles"`

	ID          uuid.UUID      `bun:",pk,type:uuid"`
	Name        string         `bun:"name,notnull"`
	Order       int            `bun:"order,notnull,default:0"`
	Description string         `bun:"description"`
	RoleKey     string         `bun:"role_key"`
	Permissions []string       `bun:"permissions,type:jsonb"`
	Metadata    map[string]any `bun:"metadata,type:jsonb"`
	IsSystem    bool           `bun:"is_system,notnull"`
	TenantID    uuid.UUID      `bun:"tenant_id,type:uuid,notnull,default:'00000000-0000-0000-0000-000000000000'"`
	OrgID       uuid.UUID      `bun:"org_id,type:uuid,notnull,default:'00000000-0000-0000-0000-000000000000'"`
	CreatedAt   time.Time      `bun:"created_at,notnull"`
	UpdatedAt   time.Time      `bun:"updated_at,notnull"`
	CreatedBy   uuid.UUID      `bun:"created_by,type:uuid,notnull"`
	UpdatedBy   uuid.UUID      `bun:"updated_by,type:uuid,notnull"`
}

// RoleAssignment represents rows from user_custom_roles.
type RoleAssignment struct {
	bun.BaseModel `bun:"table:user_custom_roles"`

	UserID     uuid.UUID `bun:"user_id,type:uuid,pk"`
	RoleID     uuid.UUID `bun:"role_id,type:uuid,pk"`
	TenantID   uuid.UUID `bun:"tenant_id,type:uuid,pk"`
	OrgID      uuid.UUID `bun:"org_id,type:uuid,pk"`
	AssignedAt time.Time `bun:"assigned_at,notnull"`
	AssignedBy uuid.UUID `bun:"assigned_by,type:uuid,notnull"`
}
