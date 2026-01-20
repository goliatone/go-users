package users

import "embed"

// GetMigrationsFS exposes the full migration tree (auth bootstrap + core).
// For dialect-aware registration, prefer GetCoreMigrationsFS and
// GetAuthBootstrapMigrationsFS so auth can be registered from the auth/ root.
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

// GetAuthExtrasMigrationsFS exposes optional auth migrations for social identities.
func GetAuthExtrasMigrationsFS() embed.FS {
	return AuthExtrasMigrationsFS
}
