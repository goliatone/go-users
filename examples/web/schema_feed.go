package main

import (
	"sync"
	"time"

	"github.com/goliatone/go-users/pkg/schema"
)

type schemaFeed struct {
	mu      sync.RWMutex
	events  []schema.Snapshot
	history int
}

func newSchemaFeed(limit int) *schemaFeed {
	if limit <= 0 {
		limit = 8
	}
	return &schemaFeed{
		events:  make([]schema.Snapshot, 0, limit),
		history: limit,
	}
}

func (f *schemaFeed) Append(snapshot schema.Snapshot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if snapshot.GeneratedAt.IsZero() {
		snapshot.GeneratedAt = time.Now().UTC()
	}
	f.events = append([]schema.Snapshot{snapshot}, f.events...)
	if len(f.events) > f.history {
		f.events = f.events[:f.history]
	}
}

func (f *schemaFeed) Latest() (schema.Snapshot, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if len(f.events) == 0 {
		return schema.Snapshot{}, false
	}
	return f.events[0], true
}

func (f *schemaFeed) History() []schema.Snapshot {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]schema.Snapshot, len(f.events))
	copy(out, f.events)
	return out
}
