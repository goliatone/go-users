package command

import (
	"context"
	"strings"
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

type secureLinkRuntimeConfig struct {
	SecureLinks  types.SecureLinkManager
	Clock        types.Clock
	IDGen        types.IDGenerator
	Activity     types.ActivitySink
	Hooks        types.Hooks
	Logger       types.Logger
	TokenTTL     time.Duration
	DefaultTTL   time.Duration
	ScopeGuard   scope.Guard
	Route        string
	DefaultRoute string
}

type secureLinkRuntime struct {
	manager  types.SecureLinkManager
	clock    types.Clock
	idGen    types.IDGenerator
	sink     types.ActivitySink
	hooks    types.Hooks
	logger   types.Logger
	tokenTTL time.Duration
	guard    scope.Guard
	route    string
}

func newSecureLinkRuntime(cfg secureLinkRuntimeConfig) secureLinkRuntime {
	ttl := cfg.TokenTTL
	if ttl == 0 && cfg.SecureLinks != nil {
		ttl = cfg.SecureLinks.GetExpiration()
	}
	if ttl == 0 {
		ttl = cfg.DefaultTTL
	}
	idGen := cfg.IDGen
	if idGen == nil {
		idGen = types.UUIDGenerator{}
	}
	route := strings.TrimSpace(cfg.Route)
	if route == "" {
		route = cfg.DefaultRoute
	}
	return secureLinkRuntime{
		manager:  cfg.SecureLinks,
		clock:    safeClock(cfg.Clock),
		idGen:    idGen,
		sink:     safeActivitySink(cfg.Activity),
		hooks:    safeHooks(cfg.Hooks),
		logger:   safeLogger(cfg.Logger),
		tokenTTL: ttl,
		guard:    safeScopeGuard(cfg.ScopeGuard),
		route:    route,
	}
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
