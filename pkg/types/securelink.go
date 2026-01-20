package types

import (
	"context"
	"time"
)

// SecureLinkManager mirrors the external securelink manager interface.
type SecureLinkManager interface {
	Generate(route string, payloads ...SecureLinkPayload) (string, error)
	Validate(token string) (map[string]any, error)
	GetAndValidate(fn func(string) string) (SecureLinkPayload, error)
	GetExpiration() time.Duration
}

// SecureLinkPayload carries data to embed in a secure link token.
type SecureLinkPayload map[string]any

// SecureLinkConfigurator mirrors the external securelink configurator interface.
type SecureLinkConfigurator interface {
	GetSigningKey() string
	GetExpiration() time.Duration
	GetBaseURL() string
	GetQueryKey() string
	GetRoutes() map[string]string
	GetAsQuery() bool
}

// ScopeEnforcer lets hosts validate securelink scope data against the request context.
type ScopeEnforcer func(ctx context.Context, payload SecureLinkPayload, scope ScopeFilter) error
