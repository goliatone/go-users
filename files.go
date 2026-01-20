package users

import "embed"

// GetMigrationsFS exposes the full migration set (auth bootstrap + core).
// Prefer GetCoreMigrationsFS and GetAuthBootstrapMigrationsFS for split usage.
func GetMigrationsFS() embed.FS {
	return MigrationsFS
}

// GetCoreMigrationsFS exposes go-users core migrations (no users/password_reset).
func GetCoreMigrationsFS() embed.FS {
	return CoreMigrationsFS
}

// GetAuthBootstrapMigrationsFS exposes the auth bootstrap migrations.
func GetAuthBootstrapMigrationsFS() embed.FS {
	return AuthBootstrapMigrationsFS
}
