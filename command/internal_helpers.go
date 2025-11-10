package command

import (
	"context"
	"time"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
)

func safeClock(clock types.Clock) types.Clock {
	if clock != nil {
		return clock
	}
	return types.SystemClock{}
}

func safeLogger(logger types.Logger) types.Logger {
	if logger != nil {
		return logger
	}
	return types.NopLogger{}
}

func safeHooks(hooks types.Hooks) types.Hooks {
	return hooks
}

func safeActivitySink(sink types.ActivitySink) types.ActivitySink {
	return sink
}

func safeScopeGuard(g scope.Guard) scope.Guard {
	return scope.Ensure(g)
}

func now(clock types.Clock) time.Time {
	if clock == nil {
		return time.Now().UTC()
	}
	return clock.Now()
}

func emitLifecycleHook(ctx context.Context, hooks types.Hooks, event types.LifecycleEvent) {
	if hooks.AfterLifecycle == nil {
		return
	}
	hooks.AfterLifecycle(ctx, event)
}

func logActivity(ctx context.Context, sink types.ActivitySink, record types.ActivityRecord) {
	if sink == nil {
		return
	}
	_ = sink.Log(ctx, record)
}

func emitActivityHook(ctx context.Context, hooks types.Hooks, record types.ActivityRecord) {
	if hooks.AfterActivity == nil {
		return
	}
	hooks.AfterActivity(ctx, record)
}

func emitPreferenceHook(ctx context.Context, hooks types.Hooks, event types.PreferenceEvent) {
	if hooks.AfterPreferenceChange == nil {
		return
	}
	hooks.AfterPreferenceChange(ctx, event)
}

func emitProfileHook(ctx context.Context, hooks types.Hooks, event types.ProfileEvent) {
	if hooks.AfterProfileChange == nil {
		return
	}
	hooks.AfterProfileChange(ctx, event)
}
