package extras

import (
	"io/fs"

	users "github.com/goliatone/go-users"
	"github.com/goliatone/go-users/migrations"
)

func init() {
	extrasFS, err := fs.Sub(users.GetAuthExtrasMigrationsFS(), "data/sql/migrations/auth_extras")
	if err != nil {
		return
	}
	migrations.Register(extrasFS)
}
