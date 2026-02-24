package activity

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/goliatone/go-users/pkg/types"
)

var (
	// ErrMissingActivityMapperRegistry indicates registry-backed mapper has no registry.
	ErrMissingActivityMapperRegistry = errors.New("go-users: missing activity mapper registry")
	// ErrActivityMapperNameRequired indicates mapper registration/query lacked a name.
	ErrActivityMapperNameRequired = errors.New("go-users: activity mapper name required")
	// ErrActivityMapperExists indicates the mapper name is already registered.
	ErrActivityMapperExists = errors.New("go-users: activity mapper already registered")
	// ErrActivityMapperNotFound indicates the named mapper is not registered.
	ErrActivityMapperNotFound = errors.New("go-users: activity mapper not found")
)

// ActivityMapperRegistry stores named event mappers for runtime composition.
type ActivityMapperRegistry struct {
	mu      sync.RWMutex
	mappers map[string]ActivityEventMapper
}

// NewActivityMapperRegistry constructs an empty mapper registry.
func NewActivityMapperRegistry() *ActivityMapperRegistry {
	return &ActivityMapperRegistry{
		mappers: make(map[string]ActivityEventMapper),
	}
}

// Register adds a named mapper. Names are normalized to lower-case.
func (r *ActivityMapperRegistry) Register(name string, mapper ActivityEventMapper) error {
	if r == nil {
		return ErrMissingActivityMapperRegistry
	}
	key := strings.ToLower(strings.TrimSpace(name))
	switch {
	case key == "":
		return ErrActivityMapperNameRequired
	case mapper == nil:
		return ErrMissingActivityMapper
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.mappers[key]; exists {
		return ErrActivityMapperExists
	}
	r.mappers[key] = mapper
	return nil
}

// Lookup returns the named mapper when present.
func (r *ActivityMapperRegistry) Lookup(name string) (ActivityEventMapper, bool) {
	if r == nil {
		return nil, false
	}
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	mapper, ok := r.mappers[key]
	return mapper, ok
}

// Names returns sorted mapper names.
func (r *ActivityMapperRegistry) Names() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.mappers))
	for name := range r.mappers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// RegistryMapper resolves and delegates mapping to a named registry mapper.
type RegistryMapper struct {
	Registry *ActivityMapperRegistry
	Name     string
}

var _ ActivityEventMapper = (*RegistryMapper)(nil)

// Map resolves the named mapper and delegates event mapping.
func (m *RegistryMapper) Map(ctx context.Context, evt any) (types.ActivityRecord, error) {
	if m == nil || m.Registry == nil {
		return types.ActivityRecord{}, ErrMissingActivityMapperRegistry
	}
	name := strings.TrimSpace(m.Name)
	if name == "" {
		return types.ActivityRecord{}, ErrActivityMapperNameRequired
	}
	mapper, ok := m.Registry.Lookup(name)
	if !ok || mapper == nil {
		return types.ActivityRecord{}, ErrActivityMapperNotFound
	}
	return mapper.Map(ctx, evt)
}
