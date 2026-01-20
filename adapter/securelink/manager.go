package securelink

import (
	"errors"
	"time"

	urlkit "github.com/goliatone/go-urlkit/securelink"
	"github.com/goliatone/go-users/pkg/types"
)

// Manager adapts go-urlkit securelink managers to go-users interfaces.
type Manager struct {
	inner urlkit.Manager
}

// NewManager builds a securelink adapter using the configurator interface.
func NewManager(cfg types.SecureLinkConfigurator) (*Manager, error) {
	if cfg == nil {
		return nil, errors.New("securelink configurator required")
	}
	inner, err := urlkit.NewManagerFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Manager{inner: inner}, nil
}

// WrapManager wraps an existing go-urlkit manager.
func WrapManager(inner urlkit.Manager) *Manager {
	if inner == nil {
		return nil
	}
	return &Manager{inner: inner}
}

// Generate produces a signed secure link using the configured manager.
func (m *Manager) Generate(route string, payloads ...types.SecureLinkPayload) (string, error) {
	if m == nil || m.inner == nil {
		return "", errors.New("securelink manager not configured")
	}
	return m.inner.Generate(route, toPayloads(payloads)...)
}

// Validate checks a secure link token and returns the decoded payload.
func (m *Manager) Validate(token string) (map[string]any, error) {
	if m == nil || m.inner == nil {
		return nil, errors.New("securelink manager not configured")
	}
	return m.inner.Validate(token)
}

// GetAndValidate extracts a token from the provided function and validates it.
func (m *Manager) GetAndValidate(fn func(string) string) (types.SecureLinkPayload, error) {
	if m == nil || m.inner == nil {
		return nil, errors.New("securelink manager not configured")
	}
	payload, err := m.inner.GetAndValidate(fn)
	if err != nil {
		return nil, err
	}
	return types.SecureLinkPayload(payload), nil
}

// GetExpiration exposes the manager's expiration duration.
func (m *Manager) GetExpiration() time.Duration {
	if m == nil || m.inner == nil {
		return 0
	}
	return m.inner.GetExpiration()
}

func toPayloads(payloads []types.SecureLinkPayload) []urlkit.Payload {
	if len(payloads) == 0 {
		return nil
	}
	out := make([]urlkit.Payload, 0, len(payloads))
	for _, payload := range payloads {
		out = append(out, urlkit.Payload(payload))
	}
	return out
}
