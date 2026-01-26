package activity

import (
	"context"
	"strings"

	"github.com/goliatone/go-users/pkg/types"
)

// SessionIDProvider extracts a session identifier from the request context.
type SessionIDProvider interface {
	SessionID(ctx context.Context) (string, bool)
}

// SessionIDProviderFunc adapts a function into a SessionIDProvider.
type SessionIDProviderFunc func(ctx context.Context) (string, bool)

// SessionID returns the session identifier and satisfies SessionIDProvider.
func (f SessionIDProviderFunc) SessionID(ctx context.Context) (string, bool) {
	if f == nil {
		return "", false
	}
	return f(ctx)
}

// AttachSessionID adds a session identifier to the record data if available.
func AttachSessionID(ctx context.Context, record types.ActivityRecord, provider SessionIDProvider, key string) types.ActivityRecord {
	if provider == nil {
		return record
	}
	sessionID, ok := provider.SessionID(ctx)
	if !ok {
		return record
	}
	return AttachSessionIDValue(record, sessionID, key)
}

// AttachSessionIDValue adds a session identifier to the record data if missing.
func AttachSessionIDValue(record types.ActivityRecord, sessionID, key string) types.ActivityRecord {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return record
	}
	if strings.TrimSpace(key) == "" {
		key = DataKeySessionID
	}
	out := record
	out.Data = cloneMetadata(record.Data)
	if _, exists := out.Data[key]; exists {
		return out
	}
	out.Data[key] = sessionID
	return out
}
