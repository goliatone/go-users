// Package goauth provides adapters that wrap github.com/goliatone/go-auth
// repositories so they satisfy go-users interfaces, and maps authentication
// events into scoped Activity records with explicit host disclosure policy.
// The optional ScopedPermissionsResolver binds current host-authorized scope
// and version before go-auth permission caching.
package goauth
