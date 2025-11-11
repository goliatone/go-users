package goauth

import (
	auth "github.com/goliatone/go-auth"
	"github.com/goliatone/go-users/pkg/types"
)

// UserFromDomain converts a go-users AuthUser into the upstream go-auth model.
func UserFromDomain(user *types.AuthUser) *auth.User {
	return fromAuthUser(user)
}

// UserToDomain converts the go-auth user model into the go-users AuthUser.
func UserToDomain(user *auth.User) *types.AuthUser {
	return toAuthUser(user)
}
