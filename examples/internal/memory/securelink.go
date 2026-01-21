package memory

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

// SecureLinkManager is an in-memory secure link stub for examples.
type SecureLinkManager struct {
	mu         sync.RWMutex
	expiration time.Duration
	payloads   map[string]types.SecureLinkPayload
}

// NewSecureLinkManager provisions a secure link stub.
func NewSecureLinkManager() *SecureLinkManager {
	return &SecureLinkManager{
		payloads: make(map[string]types.SecureLinkPayload),
	}
}

var _ types.SecureLinkManager = (*SecureLinkManager)(nil)

// Generate returns a unique token and stores the supplied payloads.
func (s *SecureLinkManager) Generate(route string, payloads ...types.SecureLinkPayload) (string, error) {
	if s == nil {
		return "", errors.New("securelink manager not configured")
	}
	tokenPrefix := strings.TrimSpace(route)
	if tokenPrefix == "" {
		tokenPrefix = "link"
	}
	token := tokenPrefix + ":" + uuid.New().String()
	merged := mergeSecureLinkPayloads(payloads)
	s.mu.Lock()
	if s.payloads == nil {
		s.payloads = make(map[string]types.SecureLinkPayload)
	}
	s.payloads[token] = merged
	s.mu.Unlock()
	return token, nil
}

// Validate returns the payload associated with the supplied token.
func (s *SecureLinkManager) Validate(token string) (map[string]any, error) {
	if s == nil {
		return nil, errors.New("securelink manager not configured")
	}
	s.mu.RLock()
	payload, ok := s.payloads[token]
	s.mu.RUnlock()
	if !ok {
		return map[string]any{}, nil
	}
	return map[string]any(cloneSecureLinkPayload(payload)), nil
}

// GetAndValidate extracts a token with the supplied getter and validates it.
func (s *SecureLinkManager) GetAndValidate(fn func(string) string) (types.SecureLinkPayload, error) {
	if fn == nil {
		return types.SecureLinkPayload{}, nil
	}
	token := fn("")
	if token == "" {
		return types.SecureLinkPayload{}, nil
	}
	payload, err := s.Validate(token)
	if err != nil {
		return nil, err
	}
	return types.SecureLinkPayload(payload), nil
}

// GetExpiration returns the configured expiration duration.
func (s *SecureLinkManager) GetExpiration() time.Duration {
	if s == nil {
		return 0
	}
	return s.expiration
}

func mergeSecureLinkPayloads(payloads []types.SecureLinkPayload) types.SecureLinkPayload {
	if len(payloads) == 0 {
		return types.SecureLinkPayload{}
	}
	merged := make(types.SecureLinkPayload)
	for _, payload := range payloads {
		for key, value := range payload {
			merged[key] = value
		}
	}
	return merged
}

func cloneSecureLinkPayload(payload types.SecureLinkPayload) types.SecureLinkPayload {
	if len(payload) == 0 {
		return types.SecureLinkPayload{}
	}
	copy := make(types.SecureLinkPayload, len(payload))
	for key, value := range payload {
		copy[key] = value
	}
	return copy
}
