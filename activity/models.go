package activity

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// LogEntry models the persisted row in user_activity.
type LogEntry struct {
	bun.BaseModel `bun:"table:user_activity"`

	ID         uuid.UUID      `bun:",pk,type:uuid"`
	UserID     uuid.UUID      `bun:"user_id,type:uuid"`
	ActorID    uuid.UUID      `bun:"actor_id,type:uuid"`
	TenantID   uuid.UUID      `bun:"tenant_id,type:uuid"`
	OrgID      uuid.UUID      `bun:"org_id,type:uuid"`
	Verb       string         `bun:"verb"`
	ObjectType string         `bun:"object_type"`
	ObjectID   string         `bun:"object_id"`
	Channel    string         `bun:"channel"`
	IP         string         `bun:"ip"`
	Data       map[string]any `bun:"data,type:jsonb"`
	CreatedAt  time.Time      `bun:"created_at"`
}
