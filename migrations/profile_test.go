package migrations_test

import (
	"io/fs"
	"testing"

	"github.com/goliatone/go-users/migrations"
)

func TestProfileSourcesCombinedCoreOnly(t *testing.T) {
	t.Parallel()

	sources, err := migrations.ProfileSources(migrations.ProfileCombinedWithAuth)
	if err != nil {
		t.Fatalf("profile sources: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected one source, got %d", len(sources))
	}
	if sources[0].Name != "core" {
		t.Fatalf("expected core source, got %q", sources[0].Name)
	}
	if _, err := fs.ReadFile(sources[0].Filesystem, "00003_custom_roles.up.sql"); err != nil {
		t.Fatalf("expected core migrations to include custom roles: %v", err)
	}
}

func TestProfileSourcesStandaloneDefaultIncludesAuthExtras(t *testing.T) {
	t.Parallel()

	sources, err := migrations.ProfileSources(migrations.ProfileStandalone)
	if err != nil {
		t.Fatalf("profile sources: %v", err)
	}
	if len(sources) != 3 {
		t.Fatalf("expected three sources, got %d", len(sources))
	}
	if sources[0].Name != "auth-bootstrap" {
		t.Fatalf("expected auth bootstrap first, got %q", sources[0].Name)
	}
	if sources[1].Name != "auth-extras" {
		t.Fatalf("expected auth extras second, got %q", sources[1].Name)
	}
	if sources[2].Name != "core" {
		t.Fatalf("expected core third, got %q", sources[2].Name)
	}
}

func TestProfileSourcesStandaloneDisableAuthExtras(t *testing.T) {
	t.Parallel()

	sources, err := migrations.ProfileSources(
		migrations.ProfileStandalone,
		migrations.WithProfileAuthExtras(false),
	)
	if err != nil {
		t.Fatalf("profile sources: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("expected two sources, got %d", len(sources))
	}
	if sources[0].Name != "auth-bootstrap" || sources[1].Name != "core" {
		t.Fatalf("unexpected source order: %+v", sources)
	}
}

func TestProfileSourcesCombinedRejectsAuthExtras(t *testing.T) {
	t.Parallel()

	_, err := migrations.ProfileSources(
		migrations.ProfileCombinedWithAuth,
		migrations.WithProfileAuthExtras(true),
	)
	if err == nil {
		t.Fatalf("expected error when enabling auth extras in combined profile")
	}
}

func TestProfileSourcesValidationTargetsNormalized(t *testing.T) {
	t.Parallel()

	sources, err := migrations.ProfileSources(
		migrations.ProfileStandalone,
		migrations.WithProfileValidationTargets("sqlite", "postgres", "sqlite", " "),
	)
	if err != nil {
		t.Fatalf("profile sources: %v", err)
	}
	if len(sources) == 0 {
		t.Fatalf("expected at least one source")
	}
	targets := sources[0].ValidationTargets
	if len(targets) != 2 || targets[0] != "sqlite" || targets[1] != "postgres" {
		t.Fatalf("unexpected targets: %v", targets)
	}
}
