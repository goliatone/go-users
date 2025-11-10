// Package activity provides default persistence helpers for the go-users
// ActivitySink. The Repository implements both the sink (writes) and the
// ActivityRepository read-side contract so transports can log lifecycle events
// and later query them for dashboards. Host applications can swap the
// repository if they prefer a different storage engine.
package activity
