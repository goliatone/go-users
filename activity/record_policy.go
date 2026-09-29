package activity

import (
	"context"
	"errors"
	"maps"
	"strings"
	"unicode"

	"github.com/goliatone/go-users/pkg/types"
)

const (
	// DataKeyAttemptedIdentifier contains unverified submitted login input, never actor identity.
	DataKeyAttemptedIdentifier  = "attempted_identifier"
	AuthenticationChannel       = "authentication"
	LoginFailureVerb            = "auth.login.failure"
	AttemptedIdentifierMaxRunes = 254
)

// RecordPolicyConfig defines host-owned disclosure policy. Zero configuration
// drops all metadata and IP. It does not change the package's global masker.
type RecordPolicyConfig struct {
	// DataSanitizer owns general metadata redaction. It receives a detached copy
	// of JSON-decoded maps/slices and must return only fields safe to disclose.
	DataSanitizer func(types.ActivityRecord) map[string]any
	// RetainFailedLoginIdentifier permits the bounded, event-specific exception.
	RetainFailedLoginIdentifier bool
	PreserveIP                  bool
}

// RecordPolicy applies the same disclosure rules to sinks, hooks, reads and
// caller-owned transactions. It never changes canonical identity or scope and
// does not authorize access. Configuration is immutable; callbacks must be safe
// for concurrent use when the policy is shared.
type RecordPolicy struct{ config RecordPolicyConfig }

func NewRecordPolicy(config RecordPolicyConfig) *RecordPolicy {
	return &RecordPolicy{config: config}
}

// SanitizeAttemptedIdentifier bounds unverified input without normalizing case
// or changing the identifier used by authentication. Markup remains plain text;
// renderers must escape it for their output context.
func SanitizeAttemptedIdentifier(value any) string {
	input, ok := value.(string)
	if !ok {
		return ""
	}
	runes := make([]rune, 0, AttemptedIdentifierMaxRunes)
	for _, r := range strings.TrimSpace(input) {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == unicode.ReplacementChar {
			continue
		}
		runes = append(runes, r)
		if len(runes) == AttemptedIdentifierMaxRunes {
			break
		}
	}
	return strings.TrimSpace(string(runes))
}

// Sanitize returns a detached record. A nil policy is fail-closed like the zero
// configuration. The attempted-identifier exception is enforced even if a host
// callback returns that field for an unrelated event.
func (p *RecordPolicy) Sanitize(record types.ActivityRecord) types.ActivityRecord {
	var config RecordPolicyConfig
	if p != nil {
		config = p.config
	}
	identifier := ""
	if config.RetainFailedLoginIdentifier && record.Verb == LoginFailureVerb && record.Channel == AuthenticationChannel {
		identifier = SanitizeAttemptedIdentifier(record.Data[DataKeyAttemptedIdentifier])
	}
	input := record
	record.Data = nil
	if config.DataSanitizer != nil {
		input.Data = copyPolicyData(input.Data)
		record.Data = copyPolicyData(config.DataSanitizer(input))
	}
	delete(record.Data, DataKeyAttemptedIdentifier)
	if identifier != "" {
		if record.Data == nil {
			record.Data = map[string]any{}
		}
		record.Data[DataKeyAttemptedIdentifier] = identifier
	}
	if !config.PreserveIP {
		record.IP = ""
	}
	return record
}

// SanitizeHooks decorates only AfterActivity. Other hooks retain host policy.
func (p *RecordPolicy) SanitizeHooks(hooks types.Hooks) types.Hooks {
	next := hooks.AfterActivity
	if next != nil {
		hooks.AfterActivity = func(ctx context.Context, record types.ActivityRecord) { next(ctx, p.Sanitize(record)) }
	}
	return hooks
}

// SanitizingSink applies an explicit policy before forwarding an audit record.
// Missing destinations return an error instead of silently dropping audit data.
type SanitizingSink struct {
	Next   types.ActivitySink
	Policy *RecordPolicy
}

func (s SanitizingSink) Log(ctx context.Context, record types.ActivityRecord) error {
	if s.Next == nil {
		return errors.New("activity: sanitizing sink requires a destination")
	}
	return s.Next.Log(ctx, s.Policy.Sanitize(record))
}

// Activity metadata uses JSON-decoded maps/slices. Detach them so neither
// callbacks nor downstream consumers can mutate the producer's metadata.
func copyPolicyData(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	out := make(map[string]any, len(data))
	for key, value := range data {
		out[key] = copyPolicyValue(value)
	}
	return out
}

func copyPolicyValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return copyPolicyData(value)
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = copyPolicyValue(item)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(value))
		maps.Copy(out, value)
		return out
	case []string:
		return append([]string(nil), value...)
	default:
		return value
	}
}
