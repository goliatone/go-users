package schema

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/goliatone/go-router"
	"github.com/google/uuid"
)

// ChangePublisher adapts schema change notifications to external systems
// (e.g., websockets, message buses, webhook fan-out).
type ChangePublisher interface {
	Notify(ctx context.Context, actorID uuid.UUID, metadata map[string]any)
}

// Listener receives registry snapshots whenever schemas change.
type Listener func(context.Context, Snapshot)

// Snapshot captures a moment-in-time export of the registered schemas.
type Snapshot struct {
	GeneratedAt   time.Time
	ResourceNames []string
	Document      map[string]any
}

// Registry tracks controller metadata so go-users can expose an aggregated
// schema endpoint and publish change events to downstream consumers.
type Registry struct {
	mu sync.RWMutex

	providers map[string]router.MetadataProvider
	listeners []Listener
	publisher ChangePublisher

	info             router.OpenAPIInfo
	tags             []string
	relationProvider router.RelationMetadataProvider
	uiOptions        router.UISchemaOptions
	hasUIOptions     bool
}

// Option customizes registry behaviour.
type Option func(*Registry)

// NewRegistry constructs a registry with optional configuration.
func NewRegistry(opts ...Option) *Registry {
	reg := &Registry{
		providers: make(map[string]router.MetadataProvider),
		info: router.OpenAPIInfo{
			Title:   "Admin Schemas",
			Version: "1.0.0",
		},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(reg)
		}
	}
	return reg
}

// WithInfo overrides the default OpenAPI info metadata.
func WithInfo(info router.OpenAPIInfo) Option {
	return func(r *Registry) {
		if info.Title != "" {
			r.info = info
		}
		if info.Version != "" {
			r.info.Version = info.Version
		}
		if info.Description != "" {
			r.info.Description = info.Description
		}
	}
}

// WithTags sets global tags applied to every generated OpenAPI document.
func WithTags(tags ...string) Option {
	return func(r *Registry) {
		if len(tags) == 0 {
			return
		}
		copied := append([]string(nil), tags...)
		r.tags = copied
	}
}

// WithRelationProvider configures a custom relation metadata provider for the
// generated documents.
func WithRelationProvider(provider router.RelationMetadataProvider) Option {
	return func(r *Registry) {
		if provider != nil {
			r.relationProvider = provider
		}
	}
}

// WithUISchemaOptions applies UI-specific schema enrichment callbacks.
func WithUISchemaOptions(opts router.UISchemaOptions) Option {
	return func(r *Registry) {
		r.uiOptions = opts
		r.hasUIOptions = true
	}
}

// WithPublisher wires a publisher used to notify listeners outside the process
// (e.g., websocket hubs) whenever schemas change.
func WithPublisher(publisher ChangePublisher) Option {
	return func(r *Registry) {
		r.publisher = publisher
	}
}

// Register adds a metadata provider to the registry. Subsequent registrations
// with the same resource name replace the previous snapshot.
func (r *Registry) Register(provider router.MetadataProvider) {
	if provider == nil {
		return
	}
	metadata := provider.GetMetadata()
	if metadata.Name == "" {
		return
	}
	snap, listeners, publisher := r.storeProvider(staticMetadataProvider{metadata: metadata})
	r.dispatch(context.Background(), snap, listeners, publisher)
}

// RegisterAll is a convenience helper that registers multiple providers.
func (r *Registry) RegisterAll(providers ...router.MetadataProvider) {
	for _, provider := range providers {
		r.Register(provider)
	}
}

// Subscribe attaches a listener invoked each time the registry snapshot is
// refreshed (typically whenever a new controller registers).
func (r *Registry) Subscribe(listener Listener) {
	if listener == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listeners = append(r.listeners, listener)
}

// Resources returns the registered resource names sorted for determinism.
func (r *Registry) Resources() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Document compiles the currently registered providers into a single OpenAPI
// document. Nil is returned when no providers are registered yet.
func (r *Registry) Document() map[string]any {
	r.mu.RLock()
	snap := r.buildSnapshotLocked()
	r.mu.RUnlock()
	return r.compile(snap)
}

// Handler returns a go-router handler that serves the aggregated schema
// document from the `/admin/schemas` endpoint (or any route the caller chooses).
func (r *Registry) Handler() router.HandlerFunc {
	return func(ctx router.Context) error {
		doc := r.Document()
		if len(doc) == 0 {
			return ctx.NoContent(http.StatusNoContent)
		}
		return ctx.JSON(http.StatusOK, doc)
	}
}

// storeProvider snapshots the registry for downstream dispatch logic. Callers
// must not hold locks when invoking dispatch with the returned snapshot.
func (r *Registry) storeProvider(provider router.MetadataProvider) (snapshotData, []Listener, ChangePublisher) {
	r.mu.Lock()
	defer r.mu.Unlock()
	meta := provider.GetMetadata()
	r.providers[meta.Name] = provider
	return r.buildSnapshotLocked(), append([]Listener(nil), r.listeners...), r.publisher
}

// dispatch notifies local listeners and optional publishers about schema changes.
func (r *Registry) dispatch(ctx context.Context, snap snapshotData, listeners []Listener, publisher ChangePublisher) {
	if len(listeners) == 0 && publisher == nil {
		return
	}
	doc := r.compile(snap)
	if len(doc) == 0 {
		return
	}
	event := Snapshot{
		GeneratedAt:   time.Now().UTC(),
		ResourceNames: append([]string(nil), snap.resourceNames...),
		Document:      doc,
	}
	for _, listener := range listeners {
		listener(ctx, event)
	}
	if publisher != nil {
		publisher.Notify(ctx, uuid.Nil, map[string]any{
			"event":     "schemas.registry.updated",
			"version":   snap.info.Version,
			"resources": event.ResourceNames,
		})
	}
}

// snapshotData contains the inputs needed to render an OpenAPI document.
type snapshotData struct {
	providers        []router.MetadataProvider
	resourceNames    []string
	info             router.OpenAPIInfo
	tags             []string
	relationProvider router.RelationMetadataProvider
	uiOptions        *router.UISchemaOptions
}

func (r *Registry) buildSnapshotLocked() snapshotData {
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	providers := make([]router.MetadataProvider, 0, len(names))
	for _, name := range names {
		providers = append(providers, r.providers[name])
	}
	var uiOpts *router.UISchemaOptions
	if r.hasUIOptions {
		opts := r.uiOptions
		uiOpts = &opts
	}
	return snapshotData{
		providers:        providers,
		resourceNames:    names,
		info:             r.info,
		tags:             append([]string(nil), r.tags...),
		relationProvider: r.relationProvider,
		uiOptions:        uiOpts,
	}
}

func (r *Registry) compile(snap snapshotData) map[string]any {
	if len(snap.providers) == 0 {
		return nil
	}
	aggregator := router.NewMetadataAggregator()
	if snap.relationProvider != nil {
		aggregator.WithRelationProvider(snap.relationProvider)
	}
	if snap.uiOptions != nil {
		aggregator.WithUISchemaOptions(*snap.uiOptions)
	}
	if len(snap.tags) > 0 {
		aggregator.SetTags(snap.tags)
	}
	if snap.info.Title != "" {
		aggregator.SetInfo(snap.info)
	}
	aggregator.AddProviders(snap.providers...)
	aggregator.Compile()
	return aggregator.GenerateOpenAPI()
}

type staticMetadataProvider struct {
	metadata router.ResourceMetadata
}

func (s staticMetadataProvider) GetMetadata() router.ResourceMetadata {
	return s.metadata
}
