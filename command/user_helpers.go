package command

import (
	"strings"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

func normalizeAuthUser(input *types.AuthUser) *types.AuthUser {
	if input == nil {
		return nil
	}
	user := *input
	user.Email = strings.TrimSpace(user.Email)
	user.Username = strings.TrimSpace(user.Username)
	user.FirstName = strings.TrimSpace(user.FirstName)
	user.LastName = strings.TrimSpace(user.LastName)
	user.Role = strings.TrimSpace(user.Role)
	if input.Metadata == nil {
		user.Metadata = nil
	} else {
		user.Metadata = cloneMap(input.Metadata)
	}
	return &user
}

func resolveAuthUserStatus(user *types.AuthUser, override types.LifecycleState) types.LifecycleState {
	status := override
	if status == "" && user != nil {
		status = user.Status
	}
	if status == "" {
		status = types.LifecycleStateActive
	}
	return status
}

func buildUserCreatedActivityRecord(user *types.AuthUser, actor types.ActorRef, scope types.ScopeFilter, clock types.Clock, dryRun bool) types.ActivityRecord {
	userID := uuid.Nil
	objectID := uuid.Nil.String()
	email := ""
	role := ""
	status := types.LifecycleState("")
	if user != nil {
		userID = user.ID
		if userID != uuid.Nil {
			objectID = userID.String()
		}
		email = user.Email
		role = user.Role
		status = user.Status
	}
	data := map[string]any{
		"email":  email,
		"role":   role,
		"status": status,
	}
	if dryRun {
		data["dry_run"] = true
	}
	return types.ActivityRecord{
		UserID:     userID,
		ActorID:    actor.ID,
		Verb:       "user.created",
		ObjectType: "user",
		ObjectID:   objectID,
		Channel:    "users",
		TenantID:   scope.TenantID,
		OrgID:      scope.OrgID,
		Data:       data,
		OccurredAt: now(clock),
	}
}
