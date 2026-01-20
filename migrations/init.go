package migrations

import (
	users "github.com/goliatone/go-users"
)

func init() {
	Register(users.GetCoreMigrationsFS())
}
