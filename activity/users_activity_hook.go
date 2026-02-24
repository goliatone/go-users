package activity

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

var (
	// ErrMissingActivityMapper indicates hook execution lacks a configured mapper.
	ErrMissingActivityMapper = errors.New("go-users: missing activity event mapper")
	// ErrUnsupportedActivityEvent indicates the mapper cannot interpret the event.
	ErrUnsupportedActivityEvent = errors.New("go-users: unsupported activity event")
)

// ActivityEventMapper maps arbitrary events into ActivityRecord payloads.
type ActivityEventMapper interface {
	Map(ctx context.Context, evt any) (types.ActivityRecord, error)
}

// ActivityEventMapperFunc adapts a function into ActivityEventMapper.
type ActivityEventMapperFunc func(ctx context.Context, evt any) (types.ActivityRecord, error)

// Map executes the mapper function.
func (f ActivityEventMapperFunc) Map(ctx context.Context, evt any) (types.ActivityRecord, error) {
	return f(ctx, evt)
}

// ActivityEnvelopeCarrier exposes envelope fields used by EnvelopeMapper.
type ActivityEnvelopeCarrier interface {
	ActivityChannel() string
	ActivityVerb() string
	ActivityObjectType() string
	ActivityObjectID() string
	ActivityActorID() string
	ActivityTenantID() string
	ActivityOccurredAt() time.Time
	ActivityMetadata() map[string]any
}

// ActivityEnvelope is a generic envelope shape for low-coupling integrations.
type ActivityEnvelope struct {
	Channel    string
	Verb       string
	ObjectType string
	ObjectID   string
	ActorID    string
	TenantID   string
	OccurredAt time.Time
	Metadata   map[string]any
}

func (e ActivityEnvelope) ActivityChannel() string          { return e.Channel }
func (e ActivityEnvelope) ActivityVerb() string             { return e.Verb }
func (e ActivityEnvelope) ActivityObjectType() string       { return e.ObjectType }
func (e ActivityEnvelope) ActivityObjectID() string         { return e.ObjectID }
func (e ActivityEnvelope) ActivityActorID() string          { return e.ActorID }
func (e ActivityEnvelope) ActivityTenantID() string         { return e.TenantID }
func (e ActivityEnvelope) ActivityOccurredAt() time.Time    { return e.OccurredAt }
func (e ActivityEnvelope) ActivityMetadata() map[string]any { return e.Metadata }

// EnvelopeMapper maps ActivityEnvelopeCarrier events to ActivityRecord.
type EnvelopeMapper struct {
	DefaultChannel    string
	DefaultObjectType string
}

// Map converts a generic envelope event into ActivityRecord.
func (m EnvelopeMapper) Map(_ context.Context, evt any) (types.ActivityRecord, error) {
	carrier, ok := evt.(ActivityEnvelopeCarrier)
	if !ok || carrier == nil {
		return types.ActivityRecord{}, ErrUnsupportedActivityEvent
	}

	actorID := parseUUIDOrNil(carrier.ActivityActorID())
	tenantID := parseUUIDOrNil(carrier.ActivityTenantID())
	verb := strings.TrimSpace(carrier.ActivityVerb())
	objectType := strings.TrimSpace(carrier.ActivityObjectType())
	objectID := strings.TrimSpace(carrier.ActivityObjectID())

	if objectType == "" {
		objectType = strings.TrimSpace(m.DefaultObjectType)
	}
	channel := strings.TrimSpace(carrier.ActivityChannel())
	if channel == "" {
		channel = strings.TrimSpace(m.DefaultChannel)
	}

	opts := make([]RecordOption, 0, 3)
	if channel != "" {
		opts = append(opts, WithChannel(channel))
	}
	if tenantID != uuid.Nil {
		opts = append(opts, WithTenant(tenantID))
	}
	if occurredAt := carrier.ActivityOccurredAt(); !occurredAt.IsZero() {
		opts = append(opts, WithOccurredAt(occurredAt.UTC()))
	}

	record, err := BuildRecordFromUUID(
		actorID,
		verb,
		objectType,
		objectID,
		cloneMetadata(carrier.ActivityMetadata()),
		opts...,
	)
	if err != nil {
		return types.ActivityRecord{}, err
	}
	return record, nil
}

// UsersActivityHook maps events into go-users activity records using a mapper.
type UsersActivityHook struct {
	Sink            types.ActivitySink
	Mapper          ActivityEventMapper
	SessionProvider SessionIDProvider
	Enricher        ActivityEnricher
	ErrorHandler    EnrichmentErrorHandler
}

// Notify maps and persists one event through the configured sink.
func (h *UsersActivityHook) Notify(ctx context.Context, evt any) error {
	if h == nil || h.Sink == nil {
		return types.ErrMissingActivitySink
	}
	if h.Mapper == nil {
		return ErrMissingActivityMapper
	}

	record, err := h.Mapper.Map(ctx, evt)
	if err != nil {
		return err
	}
	record = AttachSessionID(ctx, record, h.SessionProvider, "")

	return (&EnrichedSink{
		Sink:         h.Sink,
		Enricher:     h.Enricher,
		ErrorHandler: h.ErrorHandler,
	}).Log(ctx, record)
}

func parseUUIDOrNil(value string) uuid.UUID {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return uuid.Nil
	}
	return parsed
}
