package activity

import (
	"context"

	"github.com/goliatone/go-users/pkg/types"
)

// ActivityEnricher mutates or returns an enriched ActivityRecord.
type ActivityEnricher interface {
	Enrich(ctx context.Context, record types.ActivityRecord) (types.ActivityRecord, error)
}

// EnricherFunc adapts a function into an ActivityEnricher.
type EnricherFunc func(ctx context.Context, record types.ActivityRecord) (types.ActivityRecord, error)

// Enrich executes the function and satisfies ActivityEnricher.
func (f EnricherFunc) Enrich(ctx context.Context, record types.ActivityRecord) (types.ActivityRecord, error) {
	return f(ctx, record)
}

// EnricherChain composes multiple enrichers in order.
type EnricherChain []ActivityEnricher

// EnricherWithHandler allows enrichment with an explicit error handler.
type EnricherWithHandler interface {
	EnrichWithHandler(ctx context.Context, record types.ActivityRecord, handler EnrichmentErrorHandler) (types.ActivityRecord, error)
}

// EnrichmentErrorStrategy chooses how enrichment errors are handled.
type EnrichmentErrorStrategy int

const (
	// EnrichmentFailFast stops on the first error and returns the original record.
	EnrichmentFailFast EnrichmentErrorStrategy = iota
	// EnrichmentBestEffort keeps the last successful record and continues the chain.
	EnrichmentBestEffort
)

// EnrichmentErrorHandler decides how to handle errors during enrichment.
// Return a non-nil error to stop the chain. Return nil to continue using the
// returned record. Best-effort handlers should return the last successful
// record to allow partial enrichment.
type EnrichmentErrorHandler func(ctx context.Context, err error, enricher ActivityEnricher, current types.ActivityRecord, original types.ActivityRecord) (types.ActivityRecord, error)

// DefaultEnrichmentErrorHandler returns a handler for the chosen strategy.
func DefaultEnrichmentErrorHandler(strategy EnrichmentErrorStrategy) EnrichmentErrorHandler {
	switch strategy {
	case EnrichmentBestEffort:
		return func(_ context.Context, _ error, _ ActivityEnricher, current types.ActivityRecord, _ types.ActivityRecord) (types.ActivityRecord, error) {
			return current, nil
		}
	default:
		return func(_ context.Context, err error, _ ActivityEnricher, _ types.ActivityRecord, original types.ActivityRecord) (types.ActivityRecord, error) {
			return original, err
		}
	}
}

func applyEnricher(ctx context.Context, enricher ActivityEnricher, handler EnrichmentErrorHandler, record types.ActivityRecord) (types.ActivityRecord, error) {
	if enricher == nil {
		return record, nil
	}
	if handler == nil {
		return enricher.Enrich(ctx, record)
	}
	if handlerChain, ok := enricher.(EnricherWithHandler); ok {
		return handlerChain.EnrichWithHandler(ctx, record, handler)
	}

	enriched, err := enricher.Enrich(ctx, record)
	if err != nil {
		handled, hErr := handler(ctx, err, enricher, enriched, record)
		if hErr != nil {
			return record, hErr
		}
		return handled, nil
	}
	return enriched, nil
}

// Enrich applies the chain sequentially and stops on the first error.
func (c EnricherChain) Enrich(ctx context.Context, record types.ActivityRecord) (types.ActivityRecord, error) {
	return c.EnrichWithHandler(ctx, record, nil)
}

// EnrichWithHandler applies the chain and delegates error handling when provided.
func (c EnricherChain) EnrichWithHandler(ctx context.Context, record types.ActivityRecord, handler EnrichmentErrorHandler) (types.ActivityRecord, error) {
	original := record
	current := record

	for _, enricher := range c {
		if enricher == nil {
			continue
		}
		next, err := enricher.Enrich(ctx, current)
		if err != nil {
			if handler == nil {
				return original, err
			}
			handled, hErr := handler(ctx, err, enricher, current, original)
			if hErr != nil {
				return original, hErr
			}
			current = handled
			continue
		}
		current = next
	}

	return current, nil
}
