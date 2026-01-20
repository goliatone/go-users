package bootstrap

import (
	users "github.com/goliatone/go-users"
	"github.com/goliatone/go-users/migrations"
)

func init() {
	migrations.Register(users.GetAuthBootstrapMigrationsFS())
}
