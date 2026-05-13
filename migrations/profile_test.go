package migrations_test

import (
	"io/fs"
	"slices"
	"testing"

	persistence "github.com/goliatone/go-persistence-bun"
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
	assertProfileSourceMetadata(t, sources[0], "go-users", 30, nil)
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
	assertProfileSourceMetadata(t, sources[0], "go-users-auth", 10, nil)
	assertProfileSourceMetadata(t, sources[1], "go-users-auth-extras", 20, []string{"go-users-auth"})
	assertProfileSourceMetadata(t, sources[2], "go-users", 30, []string{"go-users-auth-extras"})
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
	assertProfileSourceMetadata(t, sources[0], "go-users-auth", 10, nil)
	assertProfileSourceMetadata(t, sources[1], "go-users", 30, []string{"go-users-auth"})
}

func TestProfileSourcesCombinedCoreDependencies(t *testing.T) {
	t.Parallel()

	sources, err := migrations.ProfileSources(
		migrations.ProfileCombinedWithAuth,
		migrations.WithProfileCoreDependencies("go-auth-core", " go-auth-extras ", "go-auth-core"),
	)
	if err != nil {
		t.Fatalf("profile sources: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected one source, got %d", len(sources))
	}
	assertProfileSourceMetadata(t, sources[0], "go-users", 30, []string{"go-auth-core", "go-auth-extras"})
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

func TestProfileSourcesLabelsDoNotChangeStableMetadata(t *testing.T) {
	t.Parallel()

	sources, err := migrations.ProfileSources(
		migrations.ProfileStandalone,
		migrations.WithProfileSourceLabels("app-users", "app-auth", "app-auth-extras"),
	)
	if err != nil {
		t.Fatalf("profile sources: %v", err)
	}
	if len(sources) != 3 {
		t.Fatalf("expected three sources, got %d", len(sources))
	}
	if sources[0].SourceLabel != "app-auth" || sources[1].SourceLabel != "app-auth-extras" || sources[2].SourceLabel != "app-users" {
		t.Fatalf("unexpected source labels: %+v", sources)
	}
	assertProfileSourceMetadata(t, sources[0], "go-users-auth", 10, nil)
	assertProfileSourceMetadata(t, sources[1], "go-users-auth-extras", 20, []string{"go-users-auth"})
	assertProfileSourceMetadata(t, sources[2], "go-users", 30, []string{"go-users-auth-extras"})
}

func TestStableOrderedProfileSourcesUseSourceStableMetadata(t *testing.T) {
	t.Parallel()

	sources, err := migrations.StableOrderedProfileSources(
		migrations.ProfileCombinedWithAuth,
		migrations.WithProfileCoreDependencies("go-auth-core"),
	)
	if err != nil {
		t.Fatalf("stable ordered profile sources: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected one source, got %d", len(sources))
	}
	source := sources[0]
	if source.IdentityMode != persistence.OrderedMigrationIdentitySourceStable {
		t.Fatalf("identity mode = %s, want source-stable", source.IdentityMode.String())
	}
	if source.Name != "core" || source.SourceKey != "go-users" || source.Order != 30 {
		t.Fatalf("unexpected stable source metadata: %+v", source)
	}
	if !slices.Equal(source.DependsOn, []string{"go-auth-core"}) {
		t.Fatalf("dependencies = %v, want [go-auth-core]", source.DependsOn)
	}
	if len(source.Options) == 0 {
		t.Fatalf("expected dialect options on stable source")
	}
}

func assertProfileSourceMetadata(t *testing.T, source migrations.ProfileSource, sourceKey string, order int, dependsOn []string) {
	t.Helper()

	if source.SourceKey != sourceKey {
		t.Fatalf("source %q key = %q, want %q", source.Name, source.SourceKey, sourceKey)
	}
	if source.Order != order {
		t.Fatalf("source %q order = %d, want %d", source.Name, source.Order, order)
	}
	if !slices.Equal(source.DependsOn, dependsOn) {
		t.Fatalf("source %q dependencies = %v, want %v", source.Name, source.DependsOn, dependsOn)
	}
}
