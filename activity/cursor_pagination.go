package activity

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// ActivityCursor defines the cursor shape for activity feeds.
type ActivityCursor struct {
	OccurredAt time.Time
	ID         uuid.UUID
}

// ApplyCursorPagination applies cursor pagination using created_at/id ordering.
// Results are ordered by created_at DESC, id DESC, and filtered to items older
// than the supplied cursor.
func ApplyCursorPagination(q *bun.SelectQuery, cursor *ActivityCursor, limit int) *bun.SelectQuery {
	if q == nil {
		return nil
	}
	q = q.OrderExpr("created_at DESC, id DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if cursor == nil || cursor.OccurredAt.IsZero() {
		return q
	}
	if cursor.ID == uuid.Nil {
		return q.Where("created_at < ?", cursor.OccurredAt)
	}
	return q.Where("(created_at < ?) OR (created_at = ? AND id < ?)", cursor.OccurredAt, cursor.OccurredAt, cursor.ID)
}
