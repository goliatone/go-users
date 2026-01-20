package tokens

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// Record models the persisted user_tokens row.
type Record struct {
	bun.BaseModel `bun:"table:user_tokens"`

	ID        uuid.UUID  `bun:"id,pk,type:uuid"`
	UserID    uuid.UUID  `bun:"user_id,notnull,type:uuid"`
	TokenType string     `bun:"token_type,notnull"`
	JTI       string     `bun:"jti,notnull"`
	Status    string     `bun:"status,notnull"`
	IssuedAt  *time.Time `bun:"issued_at,nullzero"`
	ExpiresAt *time.Time `bun:"expires_at,nullzero"`
	UsedAt    *time.Time `bun:"used_at,nullzero"`
	CreatedAt time.Time  `bun:"created_at"`
	UpdatedAt time.Time  `bun:"updated_at"`
}
