package types

import "maps"

import "time"

const (
	TemporaryPasswordMetadataKey          = "password_temporary"
	PasswordChangeRequiredMetadataKey     = "password_change_required"
	TemporaryPasswordIssuedAtMetadataKey  = "password_temporary_issued_at"
	TemporaryPasswordExpiresAtMetadataKey = "password_temporary_expires_at"
)

// MarkTemporaryPasswordMetadata returns a cloned metadata map with temporary
// password state applied.
func MarkTemporaryPasswordMetadata(metadata map[string]any, issuedAt, expiresAt time.Time) map[string]any {
	out := cloneMetadata(metadata)
	if out == nil {
		out = map[string]any{}
	}
	out[TemporaryPasswordMetadataKey] = true
	out[PasswordChangeRequiredMetadataKey] = true
	if !issuedAt.IsZero() {
		out[TemporaryPasswordIssuedAtMetadataKey] = issuedAt.UTC().Format(time.RFC3339Nano)
	}
	if !expiresAt.IsZero() {
		out[TemporaryPasswordExpiresAtMetadataKey] = expiresAt.UTC().Format(time.RFC3339Nano)
	}
	return out
}

// ClearTemporaryPasswordMetadata removes temporary password state and clears
// the password-change-required marker.
func ClearTemporaryPasswordMetadata(metadata map[string]any) map[string]any {
	out := cloneMetadata(metadata)
	if out == nil {
		return nil
	}
	delete(out, TemporaryPasswordMetadataKey)
	delete(out, PasswordChangeRequiredMetadataKey)
	delete(out, TemporaryPasswordIssuedAtMetadataKey)
	delete(out, TemporaryPasswordExpiresAtMetadataKey)
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneMetadata(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	maps.Copy(out, src)
	return out
}
