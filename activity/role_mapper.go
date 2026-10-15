package activity

import (
	"context"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

// RoleEventMapper preserves catalog scope for definition events and exact
// assignment scope for grant events. Attach through the existing activity hook.
type RoleEventMapper struct{}

func (RoleEventMapper) Map(_ context.Context, evt any) (types.ActivityRecord, error) {
	event, ok := evt.(types.RoleEvent)
	if !ok {
		return types.ActivityRecord{}, ErrUnsupportedActivityEvent
	}
	scope, _, err := types.CanonicalScope(event.Scope)
	if err != nil {
		return types.ActivityRecord{}, err
	}
	return types.ActivityRecord{ID: uuid.New(), UserID: event.UserID, ActorID: event.ActorID, TenantID: scope.TenantID, OrgID: scope.OrgID, Labels: scope.Labels, Verb: event.Action, ObjectType: "role", ObjectID: event.RoleID.String(), Channel: "roles", OccurredAt: event.OccurredAt}, nil
}
