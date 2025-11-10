package query

import "github.com/goliatone/go-users/scope"

func safeScopeGuard(g scope.Guard) scope.Guard {
	return scope.Ensure(g)
}
