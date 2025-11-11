package schema

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// Notifier provides a simple observer pattern so validation listeners can
// trigger schema refreshes in admin consumers.
type Notifier struct {
	mu        sync.RWMutex
	listeners []func(context.Context, uuid.UUID, map[string]any)
}

// NewNotifier constructs a schema notifier.
func NewNotifier() *Notifier {
	return &Notifier{}
}

// Register adds a listener that receives actor notifications. Nil listeners are ignored.
func (n *Notifier) Register(listener func(context.Context, uuid.UUID, map[string]any)) {
	if listener == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.listeners = append(n.listeners, listener)
}

// Notify emits a schema refresh event to all registered listeners.
func (n *Notifier) Notify(ctx context.Context, actorID uuid.UUID, metadata map[string]any) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	for _, listener := range n.listeners {
		listener(ctx, actorID, metadata)
	}
}
