// Package activity provides default persistence helpers for the go-users
// ActivitySink. The Repository implements both the sink (writes) and the
// ActivityRepository read-side contract so transports can log lifecycle events
// and later query them for dashboards. The ActivitySink interface lives in
// pkg/types and is intentionally minimal (`Log(ctx, ActivityRecord) error`) so
// hosts can swap sinks without breaking changes.
package activity
