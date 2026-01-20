package bootstrap

import (
	"io/fs"

	users "github.com/goliatone/go-users"
	"github.com/goliatone/go-users/migrations"
)

func init() {
	authFS, err := fs.Sub(users.GetAuthBootstrapMigrationsFS(), "data/sql/migrations/auth")
	if err != nil {
		return
	}
	migrations.Register(authFS)
}
