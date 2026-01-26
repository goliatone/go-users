package activity

import (
	"context"
	"testing"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/stretchr/testify/require"
)

func TestAttachSessionIDValueDefaultsKey(t *testing.T) {
	t.Helper()
	record := types.ActivityRecord{
		Data: map[string]any{"existing": "keep"},
	}
	out := AttachSessionIDValue(record, "session-1", "")
	require.Equal(t, "session-1", out.Data[DataKeySessionID])
	require.Equal(t, "keep", out.Data["existing"])
}

func TestAttachSessionIDValueDoesNotOverwrite(t *testing.T) {
	t.Helper()
	record := types.ActivityRecord{
		Data: map[string]any{DataKeySessionID: "original"},
	}
	out := AttachSessionIDValue(record, "new", DataKeySessionID)
	require.Equal(t, "original", out.Data[DataKeySessionID])
}

func TestAttachSessionIDUsesProvider(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	record := types.ActivityRecord{}
	provider := SessionIDProviderFunc(func(_ context.Context) (string, bool) {
		return "session-2", true
	})

	out := AttachSessionID(ctx, record, provider, "custom_key")
	require.Equal(t, "session-2", out.Data["custom_key"])
}

func TestAttachSessionIDSkipsWhenUnavailable(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	record := types.ActivityRecord{
		Data: map[string]any{"existing": "keep"},
	}
	provider := SessionIDProviderFunc(func(_ context.Context) (string, bool) {
		return "", false
	})

	out := AttachSessionID(ctx, record, provider, "")
	require.Equal(t, "keep", out.Data["existing"])
	require.Nil(t, out.Data[DataKeySessionID])
}
