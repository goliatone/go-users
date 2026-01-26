package activity

import (
	"context"
	"errors"
	"testing"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/stretchr/testify/require"
)

func TestEnricherChainOrder(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	record := types.ActivityRecord{
		Data: map[string]any{"orig": "1"},
	}

	chain := EnricherChain{
		EnricherFunc(func(_ context.Context, rec types.ActivityRecord) (types.ActivityRecord, error) {
			rec.Data = cloneMetadata(rec.Data)
			rec.Data["first"] = "yes"
			return rec, nil
		}),
		EnricherFunc(func(_ context.Context, rec types.ActivityRecord) (types.ActivityRecord, error) {
			rec.Data = cloneMetadata(rec.Data)
			rec.Data["second"] = "yes"
			return rec, nil
		}),
	}

	out, err := chain.Enrich(ctx, record)
	require.NoError(t, err)
	require.Equal(t, "1", out.Data["orig"])
	require.Equal(t, "yes", out.Data["first"])
	require.Equal(t, "yes", out.Data["second"])
}

func TestEnricherChainFailFast(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	record := types.ActivityRecord{
		Data: map[string]any{"orig": "1"},
	}

	chain := EnricherChain{
		EnricherFunc(func(_ context.Context, rec types.ActivityRecord) (types.ActivityRecord, error) {
			rec.Data = cloneMetadata(rec.Data)
			rec.Data["first"] = "yes"
			return rec, nil
		}),
		EnricherFunc(func(_ context.Context, _ types.ActivityRecord) (types.ActivityRecord, error) {
			return types.ActivityRecord{}, errors.New("boom")
		}),
	}

	out, err := chain.Enrich(ctx, record)
	require.Error(t, err)
	require.Equal(t, record.Data, out.Data)
	require.Empty(t, out.Data["first"])
}

func TestEnricherChainBestEffort(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	record := types.ActivityRecord{
		Data: map[string]any{"orig": "1"},
	}

	chain := EnricherChain{
		EnricherFunc(func(_ context.Context, rec types.ActivityRecord) (types.ActivityRecord, error) {
			rec.Data = cloneMetadata(rec.Data)
			rec.Data["first"] = "yes"
			return rec, nil
		}),
		EnricherFunc(func(_ context.Context, _ types.ActivityRecord) (types.ActivityRecord, error) {
			return types.ActivityRecord{}, errors.New("boom")
		}),
	}

	out, err := chain.EnrichWithHandler(ctx, record, DefaultEnrichmentErrorHandler(EnrichmentBestEffort))
	require.NoError(t, err)
	require.Equal(t, "1", out.Data["orig"])
	require.Equal(t, "yes", out.Data["first"])
}
