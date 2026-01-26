package activity

import (
	"context"

	"github.com/goliatone/go-users/pkg/types"
)

// EnrichedSink enriches activity records before logging them to a sink.
type EnrichedSink struct {
	Sink         types.ActivitySink
	Enricher     ActivityEnricher
	ErrorHandler EnrichmentErrorHandler
}

var _ types.ActivitySink = (*EnrichedSink)(nil)

// Log enriches the record (if configured) and forwards it to the sink.
func (s *EnrichedSink) Log(ctx context.Context, record types.ActivityRecord) error {
	if s == nil || s.Sink == nil {
		return types.ErrMissingActivitySink
	}
	if s.Enricher == nil {
		return s.Sink.Log(ctx, record)
	}

	enriched, err := applyEnricher(ctx, s.Enricher, s.ErrorHandler, record)
	if err != nil {
		return err
	}
	return s.Sink.Log(ctx, enriched)
}
