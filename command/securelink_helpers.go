package command

import (
	"fmt"
	"strings"
	"time"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

const (
	SecureLinkActionInvite        = "invite"
	SecureLinkActionRegister      = "register"
	SecureLinkActionPasswordReset = "password_reset"
)

const (
	SecureLinkRouteInviteAccept  = "invite_accept"
	SecureLinkRouteRegister      = "register"
	SecureLinkRoutePasswordReset = "password_reset"
)

const secureLinkSourceDefault = "go-users"

func buildSecureLinkPayload(action string, user *types.AuthUser, scope types.ScopeFilter, jti string, issuedAt, expiresAt time.Time, source string) types.SecureLinkPayload {
	payload := types.SecureLinkPayload{
		"action": action,
		"jti":    strings.TrimSpace(jti),
	}
	if user != nil {
		if user.ID != uuid.Nil {
			payload["user_id"] = user.ID.String()
		}
		if email := strings.TrimSpace(user.Email); email != "" {
			payload["email"] = email
		}
	}
	if !issuedAt.IsZero() {
		payload["issued_at"] = issuedAt.Format(time.RFC3339Nano)
	}
	if !expiresAt.IsZero() {
		payload["expires_at"] = expiresAt.Format(time.RFC3339Nano)
	}
	if scope.TenantID != uuid.Nil {
		payload["tenant_id"] = scope.TenantID.String()
	}
	if scope.OrgID != uuid.Nil {
		payload["org_id"] = scope.OrgID.String()
	}
	if strings.TrimSpace(source) != "" {
		payload["source"] = strings.TrimSpace(source)
	}
	return payload
}

func tokenMetadata(jti string, issuedAt, expiresAt time.Time, actor types.ActorRef, scope types.ScopeFilter) map[string]any {
	meta := map[string]any{
		"jti":        strings.TrimSpace(jti),
		"issued_at":  issuedAt.Format(time.RFC3339Nano),
		"expires_at": expiresAt.Format(time.RFC3339Nano),
	}
	if actor.ID != uuid.Nil {
		meta["actor_id"] = actor.ID.String()
	}
	if scope.TenantID != uuid.Nil {
		meta["tenant_id"] = scope.TenantID.String()
	}
	if scope.OrgID != uuid.Nil {
		meta["org_id"] = scope.OrgID.String()
	}
	return meta
}

func attachTokenMetadata(user *types.AuthUser, key string, meta map[string]any) {
	if user == nil {
		return
	}
	if user.Metadata == nil {
		user.Metadata = map[string]any{}
	}
	user.Metadata[key] = meta
}

func payloadString(payload types.SecureLinkPayload, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func payloadUUID(payload types.SecureLinkPayload, key string) uuid.UUID {
	value := payloadString(payload, key)
	if value == "" {
		return uuid.Nil
	}
	id, _ := uuid.Parse(value)
	return id
}

func payloadTime(payload types.SecureLinkPayload, key string) time.Time {
	if payload == nil {
		return time.Time{}
	}
	value, ok := payload[key]
	if !ok {
		return time.Time{}
	}
	return parseTimeValue(value)
}

func parseTimeValue(value any) time.Time {
	switch v := value.(type) {
	case time.Time:
		return v
	case string:
		if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(v)); err == nil {
			return parsed
		}
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(v)); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func scopeFromPayload(payload types.SecureLinkPayload) types.ScopeFilter {
	return types.ScopeFilter{
		TenantID: payloadUUID(payload, "tenant_id"),
		OrgID:    payloadUUID(payload, "org_id"),
	}
}
