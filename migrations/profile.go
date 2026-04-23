package migrations

import (
	"fmt"
	"io/fs"
	"strings"

	users "github.com/goliatone/go-users"
)

const (
	defaultCoreSourceLabel          = "go-users"
	defaultAuthBootstrapSourceLabel = "go-users-auth"
	defaultAuthExtrasSourceLabel    = "go-users-auth-extras"
)

// MigrationProfile defines which go-users migration tracks should be registered.
type MigrationProfile string

const (
	// ProfileCombinedWithAuth registers core-only migrations and is intended for
	// installs where go-auth migrations are already registered.
	ProfileCombinedWithAuth MigrationProfile = "combined-with-auth"
	// ProfileStandalone registers auth bootstrap + optional auth extras + core.
	ProfileStandalone MigrationProfile = "standalone"
)

// ProfileSource describes a migration source to register through go-persistence-bun.
type ProfileSource struct {
	Name              string
	SourceLabel       string
	Subdir            string
	Filesystem        fs.FS
	ValidationTargets []string
}

// ProfileOption customizes ProfileSources.
type ProfileOption func(*profileOptions)

type profileOptions struct {
	includeAuthExtras *bool
	validationTargets []string
	coreLabel         string
	authLabel         string
	authExtrasLabel   string
}

// WithProfileAuthExtras toggles auth_extras for standalone profiles.
// Combined-with-auth profiles reject this option when set to true.
func WithProfileAuthExtras(enabled bool) ProfileOption {
	return func(opts *profileOptions) {
		if opts == nil {
			return
		}
		opts.includeAuthExtras = &enabled
	}
}

// WithProfileValidationTargets overrides dialect validation targets.
func WithProfileValidationTargets(targets ...string) ProfileOption {
	return func(opts *profileOptions) {
		if opts == nil {
			return
		}
		opts.validationTargets = normalizeTargets(targets)
	}
}

// WithProfileSourceLabels overrides source labels used for registration.
func WithProfileSourceLabels(coreLabel, authLabel, authExtrasLabel string) ProfileOption {
	return func(opts *profileOptions) {
		if opts == nil {
			return
		}
		if trimmed := strings.TrimSpace(coreLabel); trimmed != "" {
			opts.coreLabel = trimmed
		}
		if trimmed := strings.TrimSpace(authLabel); trimmed != "" {
			opts.authLabel = trimmed
		}
		if trimmed := strings.TrimSpace(authExtrasLabel); trimmed != "" {
			opts.authExtrasLabel = trimmed
		}
	}
}

// ProfileSources returns ordered migration sources for the selected profile.
//
// Supported profiles:
//   - ProfileCombinedWithAuth: core migrations only
//   - ProfileStandalone: auth bootstrap + optional auth extras + core
func ProfileSources(profile MigrationProfile, opts ...ProfileOption) ([]ProfileSource, error) {
	cfg := profileOptions{
		validationTargets: []string{"postgres", "sqlite"},
		coreLabel:         defaultCoreSourceLabel,
		authLabel:         defaultAuthBootstrapSourceLabel,
		authExtrasLabel:   defaultAuthExtrasSourceLabel,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	cfg.validationTargets = normalizeTargets(cfg.validationTargets)

	resolved, err := normalizeProfile(profile)
	if err != nil {
		return nil, err
	}

	includeAuthExtras := resolved == ProfileStandalone
	if cfg.includeAuthExtras != nil {
		includeAuthExtras = *cfg.includeAuthExtras
	}
	if resolved == ProfileCombinedWithAuth && includeAuthExtras {
		return nil, fmt.Errorf("migrations: profile %q cannot include auth extras", resolved)
	}

	sources := make([]ProfileSource, 0, 3)
	if resolved == ProfileStandalone {
		authFS, authErr := fs.Sub(users.GetAuthBootstrapMigrationsFS(), "data/sql/migrations/auth")
		if authErr != nil {
			return nil, fmt.Errorf("migrations: load auth bootstrap migrations: %w", authErr)
		}
		sources = append(sources, ProfileSource{
			Name:              "auth-bootstrap",
			SourceLabel:       cfg.authLabel,
			Subdir:            "data/sql/migrations/auth",
			Filesystem:        authFS,
			ValidationTargets: append([]string{}, cfg.validationTargets...),
		})

		if includeAuthExtras {
			extrasFS, extrasErr := fs.Sub(users.GetAuthExtrasMigrationsFS(), "data/sql/migrations/auth_extras")
			if extrasErr != nil {
				return nil, fmt.Errorf("migrations: load auth extras migrations: %w", extrasErr)
			}
			sources = append(sources, ProfileSource{
				Name:              "auth-extras",
				SourceLabel:       cfg.authExtrasLabel,
				Subdir:            "data/sql/migrations/auth_extras",
				Filesystem:        extrasFS,
				ValidationTargets: append([]string{}, cfg.validationTargets...),
			})
		}
	}

	coreFS, err := fs.Sub(users.GetCoreMigrationsFS(), "data/sql/migrations")
	if err != nil {
		return nil, fmt.Errorf("migrations: load core migrations: %w", err)
	}
	sources = append(sources, ProfileSource{
		Name:              "core",
		SourceLabel:       cfg.coreLabel,
		Subdir:            "data/sql/migrations",
		Filesystem:        coreFS,
		ValidationTargets: append([]string{}, cfg.validationTargets...),
	})

	return sources, nil
}

// ProfileFilesystems returns only the filesystems for a profile in registration order.
func ProfileFilesystems(profile MigrationProfile, opts ...ProfileOption) ([]fs.FS, error) {
	sources, err := ProfileSources(profile, opts...)
	if err != nil {
		return nil, err
	}
	filesystems := make([]fs.FS, 0, len(sources))
	for _, source := range sources {
		filesystems = append(filesystems, source.Filesystem)
	}
	return filesystems, nil
}

// RegisterProfile resolves a profile and records its filesystems in registry order.
func RegisterProfile(profile MigrationProfile, opts ...ProfileOption) error {
	filesystems, err := ProfileFilesystems(profile, opts...)
	if err != nil {
		return err
	}
	for _, filesystem := range filesystems {
		Register(filesystem)
	}
	return nil
}

func normalizeProfile(profile MigrationProfile) (MigrationProfile, error) {
	value := strings.ToLower(strings.TrimSpace(string(profile)))
	switch value {
	case string(ProfileStandalone), "standalone-with-auth-bootstrap":
		return ProfileStandalone, nil
	case string(ProfileCombinedWithAuth), "combined", "core-only", "core_only", "with-go-auth":
		return ProfileCombinedWithAuth, nil
	default:
		return "", fmt.Errorf("migrations: unsupported profile %q", profile)
	}
}

func normalizeTargets(targets []string) []string {
	if len(targets) == 0 {
		return []string{"postgres", "sqlite"}
	}
	seen := make(map[string]struct{}, len(targets))
	out := make([]string, 0, len(targets))
	for _, target := range targets {
		trimmed := strings.ToLower(strings.TrimSpace(target))
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return []string{"postgres", "sqlite"}
	}
	return out
}
