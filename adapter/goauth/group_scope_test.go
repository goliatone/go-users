package goauth

import (
	"context"
	auth "github.com/goliatone/go-auth"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"reflect"
	"testing"
)

// Reflection lets this same local reproduction run on the pre-projector HEAD.
func TestG5GroupOnlyNativeAuthSink(t *testing.T) {
	ctx := context.Background()
	destination := &auditCaptureSink{}
	group := types.ScopeFilter{}.WithLabel("group", uuid.New())
	cfg := ActivitySinkConfig{Sink: destination, Scope: group, AnonymousActorID: uuid.New()}
	field := reflect.ValueOf(&cfg).Elem().FieldByName("ScopeProjector")
	if field.IsValid() {
		field.Set(reflect.ValueOf(func(context.Context, auth.ActivityEvent) (types.ScopeFilter, error) { return group, nil }))
	}
	sink, err := NewActivitySink(cfg)
	require.NoError(t, err, "validated Group-only projection must be supported")
	require.NoError(t, sink.Record(ctx, auth.ActivityEvent{EventType: auth.ActivityEventLoginSuccess}))
	require.Len(t, destination.records, 1)
}

func TestGroupProjectionIsDetachedAndValidated(t *testing.T) {
	destination := &auditCaptureSink{}
	group := types.ScopeFilter{}.WithLabel("group", uuid.New())
	deny := true
	cfg := ActivitySinkConfig{Sink: destination, AnonymousActorID: uuid.New(), ScopeProjector: func(context.Context, auth.ActivityEvent) (types.ScopeFilter, error) {
		if deny {
			return types.ScopeFilter{}, types.ErrUnauthorizedScope
		}
		return group, nil
	}}
	sink, err := NewActivitySink(cfg)
	require.NoError(t, err)
	require.ErrorIs(t, sink.Record(context.Background(), auth.ActivityEvent{EventType: auth.ActivityEventLoginSuccess}), types.ErrUnauthorizedScope)
	require.Empty(t, destination.records)
	deny = false
	require.NoError(t, sink.Record(context.Background(), auth.ActivityEvent{EventType: auth.ActivityEventLoginSuccess, Metadata: map[string]any{"group": uuid.New().String(), "password": "opaque"}}))
	got := destination.records[0]
	require.Equal(t, group.Labels, got.Labels)
	require.NotContains(t, got.Data, "group")
	require.NotContains(t, got.Data, "password")
	got.Labels["group"] = uuid.New()
	require.NotEqual(t, got.Labels, group.Labels)
}
