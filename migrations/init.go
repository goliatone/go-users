package migrations

import (
	"io/fs"

	users "github.com/goliatone/go-users"
)

func init() {
	coreFS, err := fs.Sub(users.GetCoreMigrationsFS(), "data/sql/migrations")
	if err != nil {
		return
	}
	Register(coreFS)
}
