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

	enriched, err := s.enrich(ctx, record)
	if err != nil {
		return err
	}
	return s.Sink.Log(ctx, enriched)
}

func (s *EnrichedSink) enrich(ctx context.Context, record types.ActivityRecord) (types.ActivityRecord, error) {
	if s.ErrorHandler == nil {
		return s.Enricher.Enrich(ctx, record)
	}
	if handlerChain, ok := s.Enricher.(EnricherWithHandler); ok {
		return handlerChain.EnrichWithHandler(ctx, record, s.ErrorHandler)
	}

	enriched, err := s.Enricher.Enrich(ctx, record)
	if err != nil {
		handled, hErr := s.ErrorHandler(ctx, err, s.Enricher, enriched, record)
		if hErr != nil {
			return record, hErr
		}
		return handled, nil
	}
	return enriched, nil
}
